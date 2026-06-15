package parser

import (
	"testing"

	"github.com/waywardgeek/lyre/pkg/ast"
)

// Helper to parse a function body from a .fg-style forge block.
func parseFuncBody(t *testing.T, source string) *ast.FuncDecl {
	t.Helper()
	// Wrap in a forge block with a function
	wrapped := "lyric test {\n" + source + "\n}"
	file, err := ParseString(wrapped)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(file.Blocks))
	}
	if len(file.Blocks[0].Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(file.Blocks[0].Functions))
	}
	return &file.Blocks[0].Functions[0]
}

func TestFuncWithBody(t *testing.T) {
	fn := parseFuncBody(t, `func add(x: i32, y: i32) -> i32 {
		return x + y
	}`)
	if fn.Name != "add" {
		t.Errorf("expected name 'add', got %q", fn.Name)
	}
	if fn.Body == nil {
		t.Fatal("expected function body, got nil")
	}
	if len(fn.Body.Stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(fn.Body.Stmts))
	}
	if fn.Body.Stmts[0].Kind != ast.StmtReturn {
		t.Errorf("expected return statement, got %v", fn.Body.Stmts[0].Kind)
	}
}

func TestFuncWithoutBody(t *testing.T) {
	fn := parseFuncBody(t, `func add(x: i32, y: i32) -> i32`)
	if fn.Body != nil {
		t.Error("expected nil body for declaration-only func")
	}
}

func TestLetDecl(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		let x: i32 = 42
		let mut y = 10
	}`)
	if len(fn.Body.Stmts) != 2 {
		t.Fatalf("expected 2 stmts, got %d", len(fn.Body.Stmts))
	}
	s0 := fn.Body.Stmts[0].Data.(*ast.VarDeclStmt)
	if s0.Name != "x" || s0.IsMut || s0.Type == nil {
		t.Errorf("let x: got name=%q isMut=%v type=%v", s0.Name, s0.IsMut, s0.Type)
	}
	s1 := fn.Body.Stmts[1].Data.(*ast.VarDeclStmt)
	if s1.Name != "y" || !s1.IsMut {
		t.Errorf("let mut y: got name=%q isMut=%v", s1.Name, s1.IsMut)
	}
}

func TestIfElse(t *testing.T) {
	fn := parseFuncBody(t, `func f(x: i32) -> i32 {
		if x > 0 {
			return x
		} else if x == 0 {
			return 0
		} else {
			return -x
		}
	}`)
	if len(fn.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(fn.Body.Stmts))
	}
	ifStmt := fn.Body.Stmts[0].Data.(*ast.IfStmt)
	if len(ifStmt.ElseIfs) != 1 {
		t.Errorf("expected 1 else-if, got %d", len(ifStmt.ElseIfs))
	}
	if ifStmt.Else == nil {
		t.Error("expected else block")
	}
}

func TestForLoop(t *testing.T) {
	fn := parseFuncBody(t, `func f(items: [i32]) {
		for item in items {
			print(item)
		}
	}`)
	if len(fn.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(fn.Body.Stmts))
	}
	forStmt := fn.Body.Stmts[0].Data.(*ast.ForStmt)
	if forStmt.Var != "item" {
		t.Errorf("expected var 'item', got %q", forStmt.Var)
	}
}

func TestWhileLoop(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		while x > 0 {
			x = x - 1
		}
	}`)
	if len(fn.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(fn.Body.Stmts))
	}
	if fn.Body.Stmts[0].Kind != ast.StmtWhile {
		t.Errorf("expected while, got %v", fn.Body.Stmts[0].Kind)
	}
}

func TestAssignment(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		x = 42
		y += 1
	}`)
	if len(fn.Body.Stmts) != 2 {
		t.Fatalf("expected 2 stmts, got %d", len(fn.Body.Stmts))
	}
	if fn.Body.Stmts[0].Kind != ast.StmtAssign {
		t.Errorf("expected assign, got %v", fn.Body.Stmts[0].Kind)
	}
	// y += 1 desugars to y = y + 1
	s1 := fn.Body.Stmts[1].Data.(*ast.AssignStmt)
	binExpr := s1.Value.Data.(*ast.BinaryExpr)
	if binExpr.Op != ast.OpAdd {
		t.Errorf("expected OpAdd in desugared +=, got %v", binExpr.Op)
	}
}

func TestMatchStmt(t *testing.T) {
	fn := parseFuncBody(t, `func f(opt: Option<i32>) {
		match opt {
			Some(x) => {
				print(x)
			}
			None => {
				print(0)
			}
		}
	}`)
	if len(fn.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(fn.Body.Stmts))
	}
	matchStmt := fn.Body.Stmts[0].Data.(*ast.MatchStmt)
	if len(matchStmt.Arms) != 2 {
		t.Errorf("expected 2 arms, got %d", len(matchStmt.Arms))
	}
}

func TestCascade(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		cascade {
			close(file)
		}
	}`)
	if len(fn.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(fn.Body.Stmts))
	}
	if fn.Body.Stmts[0].Kind != ast.StmtCascade {
		t.Errorf("expected cascade, got %v", fn.Body.Stmts[0].Kind)
	}
}

func TestBreakContinue(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		while true {
			if done {
				break
			}
			continue
		}
	}`)
	whileStmt := fn.Body.Stmts[0].Data.(*ast.WhileStmt)
	if len(whileStmt.Body.Stmts) != 2 {
		t.Fatalf("expected 2 stmts in while, got %d", len(whileStmt.Body.Stmts))
	}
	ifStmt := whileStmt.Body.Stmts[0].Data.(*ast.IfStmt)
	if ifStmt.Then.Stmts[0].Kind != ast.StmtBreak {
		t.Errorf("expected break")
	}
	if whileStmt.Body.Stmts[1].Kind != ast.StmtContinue {
		t.Errorf("expected continue")
	}
}

func TestBinaryPrecedence(t *testing.T) {
	// a + b * c should parse as a + (b * c)
	fn := parseFuncBody(t, `func f() {
		return a + b * c
	}`)
	ret := fn.Body.Stmts[0].Data.(*ast.ReturnStmt)
	bin := ret.Value.Data.(*ast.BinaryExpr)
	if bin.Op != ast.OpAdd {
		t.Errorf("top-level op should be Add, got %v", bin.Op)
	}
	rhs := bin.Right.Data.(*ast.BinaryExpr)
	if rhs.Op != ast.OpMul {
		t.Errorf("rhs op should be Mul, got %v", rhs.Op)
	}
}

func TestUnaryExpr(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		return -x
	}`)
	ret := fn.Body.Stmts[0].Data.(*ast.ReturnStmt)
	unary := ret.Value.Data.(*ast.UnaryExpr)
	if unary.Op != ast.OpNeg {
		t.Errorf("expected OpNeg, got %v", unary.Op)
	}
}

func TestMethodCall(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		obj.method(1, 2)
	}`)
	exprStmt := fn.Body.Stmts[0].Data.(*ast.ExprStmt)
	mc := exprStmt.Expr.Data.(*ast.MethodCallExpr)
	if mc.Method != "method" {
		t.Errorf("expected method 'method', got %q", mc.Method)
	}
	if len(mc.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(mc.Args))
	}
}

func TestFieldAccess(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		return obj.field
	}`)
	ret := fn.Body.Stmts[0].Data.(*ast.ReturnStmt)
	fa := ret.Value.Data.(*ast.FieldAccessExpr)
	if fa.Field != "field" {
		t.Errorf("expected 'field', got %q", fa.Field)
	}
}

func TestIndexExpr(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		return xs[0]
	}`)
	ret := fn.Body.Stmts[0].Data.(*ast.ReturnStmt)
	idx := ret.Value.Data.(*ast.IndexExpr)
	lit := idx.Index.Data.(*ast.IntLitExpr)
	if lit.Value != "0" {
		t.Errorf("expected index 0, got %q", lit.Value)
	}
}

func TestListLiteral(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		let xs = [1, 2, 3]
	}`)
	decl := fn.Body.Stmts[0].Data.(*ast.VarDeclStmt)
	list := decl.Value.Data.(*ast.ListLitExpr)
	if len(list.Elems) != 3 {
		t.Errorf("expected 3 elems, got %d", len(list.Elems))
	}
}

func TestBooleanLogic(t *testing.T) {
	// a && b || c should parse as (a && b) || c
	fn := parseFuncBody(t, `func f() {
		return a && b || c
	}`)
	ret := fn.Body.Stmts[0].Data.(*ast.ReturnStmt)
	bin := ret.Value.Data.(*ast.BinaryExpr)
	if bin.Op != ast.OpOr {
		t.Errorf("top-level op should be Or, got %v", bin.Op)
	}
	lhs := bin.Left.Data.(*ast.BinaryExpr)
	if lhs.Op != ast.OpAnd {
		t.Errorf("lhs op should be And, got %v", lhs.Op)
	}
}

func TestNilLiteral(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		return nil
	}`)
	ret := fn.Body.Stmts[0].Data.(*ast.ReturnStmt)
	if ret.Value.Kind != ast.ExprNil {
		t.Errorf("expected ExprNil, got %v", ret.Value.Kind)
	}
}

func TestNestedBlocks(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		if true {
			while x > 0 {
				for i in items {
					x = x - 1
				}
			}
		}
	}`)
	if fn.Body == nil {
		t.Fatal("expected body")
	}
	ifStmt := fn.Body.Stmts[0].Data.(*ast.IfStmt)
	whileStmt := ifStmt.Then.Stmts[0].Data.(*ast.WhileStmt)
	forStmt := whileStmt.Body.Stmts[0].Data.(*ast.ForStmt)
	if forStmt.Var != "i" {
		t.Errorf("expected 'i', got %q", forStmt.Var)
	}
}

func TestFuncCallChain(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		foo(bar(x), baz(y, z))
	}`)
	exprStmt := fn.Body.Stmts[0].Data.(*ast.ExprStmt)
	call := exprStmt.Expr.Data.(*ast.CallExpr)
	if len(call.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(call.Args))
	}
}

func TestComparisonChain(t *testing.T) {
	// x == y != z should parse as (x == y) != z
	fn := parseFuncBody(t, `func f() {
		return x == y != z
	}`)
	ret := fn.Body.Stmts[0].Data.(*ast.ReturnStmt)
	bin := ret.Value.Data.(*ast.BinaryExpr)
	// Both == and != have same precedence, left-assoc → top is !=
	if bin.Op != ast.OpNeq {
		t.Errorf("top op should be Neq, got %v", bin.Op)
	}
}

func TestStringLiteral(t *testing.T) {
	fn := parseFuncBody(t, `func f() {
		return "hello world"
	}`)
	ret := fn.Body.Stmts[0].Data.(*ast.ReturnStmt)
	s := ret.Value.Data.(*ast.StringLitExpr)
	if s.Value != "hello world" {
		t.Errorf("expected 'hello world', got %q", s.Value)
	}
}

func TestWildcardPattern(t *testing.T) {
	fn := parseFuncBody(t, `func f(x: i32) {
		match x {
			0 => {
				return 0
			}
			_ => {
				return 1
			}
		}
	}`)
	matchStmt := fn.Body.Stmts[0].Data.(*ast.MatchStmt)
	if matchStmt.Arms[1].Pattern.Kind != ast.PatWildcard {
		t.Errorf("expected wildcard pattern, got %v", matchStmt.Arms[1].Pattern.Kind)
	}
}

// Verify that existing .lyric parsing still works fine with the new keywords.
func TestExistingForgeStillWorks(t *testing.T) {
	source := `lyric Example {
		struct Point {
			x: f64
			y: f64
		}
		func distance(self, other: Point) -> f64
	}`
	file, err := ParseString(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].Structs) != 1 {
		t.Errorf("expected 1 struct")
	}
	fn := file.Blocks[0].Functions[0]
	if fn.Body != nil {
		t.Error("expected nil body for .lyric func")
	}
}


func TestStructLiteral(t *testing.T) {
	fn := parseFuncBody(t, `func test() {
		let p = Point{X: 3.0, Y: 4.0}
		let _ = p
	}`)
	if fn.Body == nil || len(fn.Body.Stmts) < 1 {
		t.Fatal("expected at least 1 statement")
	}
	decl := fn.Body.Stmts[0].Data.(*ast.VarDeclStmt)
	if decl.Value == nil {
		t.Fatal("expected initializer")
	}
	if decl.Value.Kind != ast.ExprStructLit {
		t.Fatalf("expected ExprStructLit, got %v", decl.Value.Kind)
	}
	sl := decl.Value.Data.(*ast.StructLitExpr)
	if sl.TypeName != "Point" {
		t.Errorf("expected type name Point, got %s", sl.TypeName)
	}
	if len(sl.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(sl.Fields))
	}
	if sl.Fields[0].Name != "X" || sl.Fields[1].Name != "Y" {
		t.Errorf("unexpected field names: %s, %s", sl.Fields[0].Name, sl.Fields[1].Name)
	}
}

func TestEmptyStructLiteral(t *testing.T) {
	fn := parseFuncBody(t, `func test() {
		let p = Empty{}
		let _ = p
	}`)
	decl := fn.Body.Stmts[0].Data.(*ast.VarDeclStmt)
	sl := decl.Value.Data.(*ast.StructLitExpr)
	if sl.TypeName != "Empty" {
		t.Errorf("expected Empty, got %s", sl.TypeName)
	}
	if len(sl.Fields) != 0 {
		t.Errorf("expected 0 fields, got %d", len(sl.Fields))
	}
}

func TestPositionalStructLiteralInTuple(t *testing.T) {
	fn := parseFuncBody(t, `func test() -> (Point, i32) {
		return (Point { 3.0, 4.0 }, 0)
	}`)
	if fn.Body == nil || len(fn.Body.Stmts) < 1 {
		t.Fatal("expected at least 1 statement")
	}
	ret := fn.Body.Stmts[0].Data.(*ast.ReturnStmt)
	if ret.Value.Kind != ast.ExprTupleLit {
		t.Fatalf("expected tuple, got %v", ret.Value.Kind)
	}
	tup := ret.Value.Data.(*ast.TupleLitExpr)
	if len(tup.Elems) != 2 {
		t.Fatalf("expected 2 tuple elems, got %d", len(tup.Elems))
	}
	if tup.Elems[0].Kind != ast.ExprStructLit {
		t.Fatalf("expected struct lit, got %v", tup.Elems[0].Kind)
	}
	sl := tup.Elems[0].Data.(*ast.StructLitExpr)
	if sl.TypeName != "Point" {
		t.Errorf("expected Point, got %s", sl.TypeName)
	}
	if len(sl.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(sl.Fields))
	}
	// Positional fields have empty names
	if sl.Fields[0].Name != "" || sl.Fields[1].Name != "" {
		t.Errorf("expected empty names for positional fields, got %q and %q", sl.Fields[0].Name, sl.Fields[1].Name)
	}
}

func TestMixedStructLiteralInTuple(t *testing.T) {
	fn := parseFuncBody(t, `func test() -> (Point, i32) {
		return (Point { 3.0, y: 4.0 }, 0)
	}`)
	ret := fn.Body.Stmts[0].Data.(*ast.ReturnStmt)
	tup := ret.Value.Data.(*ast.TupleLitExpr)
	sl := tup.Elems[0].Data.(*ast.StructLitExpr)
	if sl.Fields[0].Name != "" {
		t.Errorf("expected positional first field, got %q", sl.Fields[0].Name)
	}
	if sl.Fields[1].Name != "y" {
		t.Errorf("expected named second field 'y', got %q", sl.Fields[1].Name)
	}
}

func TestLambdaExpr(t *testing.T) {
	fn := parseFuncBody(t, `func test() {
		let double = |x: i32| -> i32 { x * 2 }
		let _ = double
	}`)
	if fn.Body == nil || len(fn.Body.Stmts) < 1 {
		t.Fatal("expected at least 1 statement")
	}
}

func TestLambdaMultiParam(t *testing.T) {
	fn := parseFuncBody(t, `func test() {
		let add = |a: i32, b: i32| -> i32 { a + b }
		let _ = add
	}`)
	if fn.Body == nil || len(fn.Body.Stmts) < 1 {
		t.Fatal("expected at least 1 statement")
	}
}

func TestLambdaNoReturnType(t *testing.T) {
	fn := parseFuncBody(t, `func test() {
		let greet = |x: string| { let _ = x }
		let _ = greet
	}`)
	if fn.Body == nil || len(fn.Body.Stmts) < 1 {
		t.Fatal("expected at least 1 statement")
	}
}

func TestFuncTypeMultiParam(t *testing.T) {
	// (i32, i32) -> i32 should parse as a function type, not a tuple
	fn := parseFuncBody(t, `func apply(f: (i32, i32) -> i32, a: i32, b: i32) -> i32 {
		return f(a, b)
	}`)
	if len(fn.Params) != 3 {
		t.Fatalf("expected 3 params, got %d", len(fn.Params))
	}
	if fn.Params[0].Type.Kind != ast.TypeFunc {
		t.Errorf("expected first param to be TypeFunc, got %v", fn.Params[0].Type.Kind)
	}
}

func TestFuncTypeSingleParam(t *testing.T) {
	fn := parseFuncBody(t, `func apply(f: (i32) -> i32, x: i32) -> i32 {
		return f(x)
	}`)
	if fn.Params[0].Type.Kind != ast.TypeFunc {
		t.Errorf("expected first param to be TypeFunc, got %v", fn.Params[0].Type.Kind)
	}
	ft := fn.Params[0].Type.Data.(ast.FuncType)
	if len(ft.Params) != 1 {
		t.Errorf("expected 1 func param, got %d", len(ft.Params))
	}
}


func TestIfLet(t *testing.T) {
	fn := parseFuncBody(t, `func test(shape: Shape) {
		if let Circle(r) = shape {
			let _ = r
		}
	}`)
	if fn.Body == nil || len(fn.Body.Stmts) < 1 {
		t.Fatal("expected at least 1 statement")
	}
	stmt := fn.Body.Stmts[0]
	if stmt.Kind != ast.StmtIf {
		t.Fatalf("expected StmtIf, got %v", stmt.Kind)
	}
	ifStmt := stmt.Data.(*ast.IfStmt)
	if ifStmt.LetPattern == nil {
		t.Fatal("expected LetPattern to be non-nil")
	}
	if ifStmt.LetValue == nil {
		t.Fatal("expected LetValue to be non-nil")
	}
	if ifStmt.LetPattern.Kind != ast.PatVariant {
		t.Errorf("expected PatVariant, got %v", ifStmt.LetPattern.Kind)
	}
}

func TestIfLetWithElse(t *testing.T) {
	fn := parseFuncBody(t, `func test(shape: Shape) {
		if let Circle(r) = shape {
			let _ = r
		} else {
			let _ = 0
		}
	}`)
	stmt := fn.Body.Stmts[0]
	ifStmt := stmt.Data.(*ast.IfStmt)
	if ifStmt.LetPattern == nil {
		t.Fatal("expected LetPattern to be non-nil")
	}
	if ifStmt.Else == nil {
		t.Fatal("expected Else block to be non-nil")
	}
}

func TestLetElse(t *testing.T) {
	fn := parseFuncBody(t, `func test(shape: Shape) -> i32 {
		let Circle(r) = shape else { return 0 }
		return r
	}`)
	if fn.Body == nil || len(fn.Body.Stmts) < 2 {
		t.Fatal("expected at least 2 statements")
	}
	stmt := fn.Body.Stmts[0]
	if stmt.Kind != ast.StmtVarDecl {
		t.Fatalf("expected StmtVarDecl, got %v", stmt.Kind)
	}
	decl := stmt.Data.(*ast.VarDeclStmt)
	if decl.Pattern == nil {
		t.Fatal("expected Pattern to be non-nil")
	}
	if decl.ElseBlock == nil {
		t.Fatal("expected ElseBlock to be non-nil")
	}
	if decl.Value == nil {
		t.Fatal("expected Value to be non-nil")
	}
	if decl.Pattern.Kind != ast.PatVariant {
		t.Errorf("expected PatVariant, got %v", decl.Pattern.Kind)
	}
}
