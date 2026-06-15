package parser

import (
	"os"
	"testing"

	"github.com/waywardgeek/lyre/pkg/ast"
)

func TestParseMinimalForge(t *testing.T) {
	input := `lyric Foo {
  why: "A test module."
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(file.Blocks))
	}
	if file.Blocks[0].Name != "Foo" {
		t.Errorf("expected name Foo, got %s", file.Blocks[0].Name)
	}
	if file.Blocks[0].Why != "A test module." {
		t.Errorf("expected why, got %q", file.Blocks[0].Why)
	}
}

func TestParseInterfaceFields(t *testing.T) {
	input := `lyric Test {
  interface DoublyLinked<P, C> {
    func P.children(self) -> [C]
    field P.first: C?
    field P.last: C?
    field C.prev: C?
    field C.next: C?
    field C.parent: P?
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	iface := file.Blocks[0].Interfaces[0]
	if len(iface.Methods) != 1 {
		t.Errorf("expected 1 method, got %d", len(iface.Methods))
	}
	if len(iface.Fields) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(iface.Fields))
	}
	f := iface.Fields[0]
	if f.TypeParam != "P" || f.Name != "first" {
		t.Errorf("field 0: expected P.first, got %s.%s", f.TypeParam, f.Name)
	}
	if f.Type.Kind != ast.TypeOptional {
		t.Errorf("field 0: expected optional type")
	}
	f4 := iface.Fields[4]
	if f4.TypeParam != "C" || f4.Name != "parent" {
		t.Errorf("field 4: expected C.parent, got %s.%s", f4.TypeParam, f4.Name)
	}
}
func TestParseStruct(t *testing.T) {
	input := `lyric Test {
  struct Point {
    x: f64
    y: f64
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].Structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(file.Blocks[0].Structs))
	}
	s := file.Blocks[0].Structs[0]
	if s.Name != "Point" {
		t.Errorf("expected Point, got %s", s.Name)
	}
	if len(s.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(s.Fields))
	}
}

func TestParseEnum(t *testing.T) {
	input := `lyric Test {
  enum Direction { North South East West }
  enum Shape {
    Circle(radius: f64)
    Rectangle(width: f64, height: f64)
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].Enums) != 2 {
		t.Fatalf("expected 2 enums, got %d", len(file.Blocks[0].Enums))
	}
	d := file.Blocks[0].Enums[0]
	if len(d.Variants) != 4 {
		t.Errorf("expected 4 Direction variants, got %d", len(d.Variants))
	}
	s := file.Blocks[0].Enums[1]
	if len(s.Variants) != 2 {
		t.Errorf("expected 2 Shape variants, got %d", len(s.Variants))
	}
	if len(s.Variants[1].Fields) != 2 {
		t.Errorf("expected 2 Rectangle fields, got %d", len(s.Variants[1].Fields))
	}
}

func TestParseInterface(t *testing.T) {
	input := `lyric Test {
  interface Writer {
    func write(mut self, data: [u8]) -> (u64, error)
    func close(mut self) -> unit
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].Interfaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(file.Blocks[0].Interfaces))
	}
	iface := file.Blocks[0].Interfaces[0]
	if iface.Name != "Writer" {
		t.Errorf("expected Writer, got %s", iface.Name)
	}
	if len(iface.Methods) != 2 {
		t.Errorf("expected 2 methods, got %d", len(iface.Methods))
	}
}

func TestParseMultiClassInterface(t *testing.T) {
	input := `lyric Test {
  interface Graph<G, N, E> {
    func G.nodes(self) -> [N]
    func G.edges(self) -> [E]
    func N.out_edges(self) -> [E]
    func E.src_node(self) -> N
    func E.tgt_node(self) -> N
    func distance(a: N, b: N) -> i32
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	iface := file.Blocks[0].Interfaces[0]
	if iface.Name != "Graph" {
		t.Errorf("expected Graph, got %s", iface.Name)
	}
	if len(iface.TypeParams) != 3 {
		t.Fatalf("expected 3 type params, got %d", len(iface.TypeParams))
	}
	if len(iface.Methods) != 6 {
		t.Fatalf("expected 6 methods, got %d", len(iface.Methods))
	}
	// Typed methods
	if iface.Methods[0].ReceiverType != "G" || iface.Methods[0].Name != "nodes" {
		t.Errorf("method 0: expected G.nodes, got %s.%s", iface.Methods[0].ReceiverType, iface.Methods[0].Name)
	}
	if iface.Methods[3].ReceiverType != "E" || iface.Methods[3].Name != "src_node" {
		t.Errorf("method 3: expected E.src_node, got %s.%s", iface.Methods[3].ReceiverType, iface.Methods[3].Name)
	}
	// Free function (no receiver)
	if iface.Methods[5].ReceiverType != "" || iface.Methods[5].Name != "distance" {
		t.Errorf("method 5: expected free function 'distance', got ReceiverType=%q Name=%q", iface.Methods[5].ReceiverType, iface.Methods[5].Name)
	}
}

func TestParseImplBlock(t *testing.T) {
	input := `lyric Test {
  impl Graph<MyGraph, MyNode, MyEdge> {
    G.nodes = MyGraph.components
    G.edges = MyGraph.wires
    N.out_edges = MyNode.outWires
    E.src_node = MyEdge.driver
    E.tgt_node = MyEdge.receiver
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].ImplBlocks) != 1 {
		t.Fatalf("expected 1 impl block, got %d", len(file.Blocks[0].ImplBlocks))
	}
	impl := file.Blocks[0].ImplBlocks[0]
	if impl.InterfaceName != "Graph" {
		t.Errorf("expected Graph, got %s", impl.InterfaceName)
	}
	if len(impl.TypeArgs) != 3 {
		t.Fatalf("expected 3 type args, got %d", len(impl.TypeArgs))
	}
	if len(impl.Mappings) != 5 {
		t.Fatalf("expected 5 mappings, got %d", len(impl.Mappings))
	}
	m := impl.Mappings[0]
	if m.TypeParam != "G" || m.MethodName != "nodes" || m.TargetClass != "MyGraph" || m.TargetMember != "components" {
		t.Errorf("mapping 0: got %+v", m)
	}
}

func TestParseImplBlockFieldBinding(t *testing.T) {
	input := `lyric Test {
  impl DoublyLinked<Folder, File> {
    P.first <-> Folder.firstFile
    P.last <-> Folder.lastFile
    C.parent <-> File.folder
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	impl := file.Blocks[0].ImplBlocks[0]
	if len(impl.Mappings) != 3 {
		t.Fatalf("expected 3 mappings, got %d", len(impl.Mappings))
	}
	m := impl.Mappings[0]
	if m.Kind != ast.ImplFieldBind {
		t.Errorf("expected ImplFieldBind, got %d", m.Kind)
	}
	if m.TargetClass != "Folder" || m.TargetMember != "firstFile" {
		t.Errorf("field bind: got %s.%s", m.TargetClass, m.TargetMember)
	}
}

func TestParseImplBlockNamedLabel(t *testing.T) {
	input := `lyric Test {
  impl Hashed<UserStore, User, string> as byEmail {
    P.lookup = UserStore.findByEmail
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	impl := file.Blocks[0].ImplBlocks[0]
	if impl.Label != "byEmail" {
		t.Errorf("expected label 'byEmail', got %q", impl.Label)
	}
}

func TestParseBareRelationalConstraint(t *testing.T) {
	input := `lyric Test {
  func min_cut<G, N, E>(graph: G) -> i32
    where Graph<G, N, E>, E: Weighted
  {
    return 0
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	fn := file.Blocks[0].Functions[0]
	if len(fn.Where) != 2 {
		t.Fatalf("expected 2 where clauses, got %d", len(fn.Where))
	}
	// Bare relational: Graph<G, N, E>
	wc0 := fn.Where[0]
	if wc0.Variable != "" || wc0.Constraint != "Graph" || len(wc0.TypeArgs) != 3 {
		t.Errorf("where[0]: expected bare Graph<G,N,E>, got Variable=%q Constraint=%q TypeArgs=%d", wc0.Variable, wc0.Constraint, len(wc0.TypeArgs))
	}
	// Single-type: E: Weighted
	wc1 := fn.Where[1]
	if wc1.Variable != "E" || wc1.Constraint != "Weighted" || len(wc1.TypeArgs) != 0 {
		t.Errorf("where[1]: expected E: Weighted, got Variable=%q Constraint=%q TypeArgs=%d", wc1.Variable, wc1.Constraint, len(wc1.TypeArgs))
	}
}

func TestParseClass(t *testing.T) {
	input := `lyric Test {
  class HttpClient {
    why: "Manages HTTP connections."

    base_url: string
    timeout: u32
    pool: ConnectionPool guarded_by(mu)
    mu: lock

    func get(mut self, path: string) -> (Response, error)
      concurrent: true
      excludes_lock(mu)
      raises: Timeout, NotFound
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].Classes) != 1 {
		t.Fatalf("expected 1 class, got %d", len(file.Blocks[0].Classes))
	}
	cls := file.Blocks[0].Classes[0]
	if cls.Name != "HttpClient" {
		t.Errorf("expected HttpClient, got %s", cls.Name)
	}
	if len(cls.Fields) != 4 {
		t.Errorf("expected 4 fields, got %d", len(cls.Fields))
	}
	if cls.Fields[2].GuardedBy != "mu" {
		t.Errorf("expected guarded_by(mu), got %q", cls.Fields[2].GuardedBy)
	}
	if len(cls.Methods) != 1 {
		t.Errorf("expected 1 method, got %d", len(cls.Methods))
	}
	m := cls.Methods[0]
	if m.Annotations.Concurrent == nil || !*m.Annotations.Concurrent {
		t.Error("expected concurrent: true")
	}
	if len(m.Annotations.ExcludesLock) != 1 || m.Annotations.ExcludesLock[0] != "mu" {
		t.Errorf("expected excludes_lock(mu), got %v", m.Annotations.ExcludesLock)
	}
	if len(m.Annotations.Raises) != 2 {
		t.Errorf("expected 2 raises, got %v", m.Annotations.Raises)
	}
}

func TestParseRelation(t *testing.T) {
	input := `lyric Test {
  relation DoublyLinked Node:outEdges owns [Edge:fromNode]
  relation Agent refs Config
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].Relations) != 2 {
		t.Fatalf("expected 2 relations, got %d", len(file.Blocks[0].Relations))
	}
	r1 := file.Blocks[0].Relations[0]
	if r1.Hint != "DoublyLinked" {
		t.Errorf("expected hint DoublyLinked, got %q", r1.Hint)
	}
	if r1.Parent.TypeName != "Node" || r1.Parent.Label != "outEdges" {
		t.Errorf("unexpected parent: %+v", r1.Parent)
	}
	if r1.Kind != ast.Owns {
		t.Error("expected owns")
	}
	if !r1.IsMany {
		t.Error("expected one-to-many")
	}
	if r1.Child.TypeName != "Edge" || r1.Child.Label != "fromNode" {
		t.Errorf("unexpected child: %+v", r1.Child)
	}

	r2 := file.Blocks[0].Relations[1]
	if r2.Kind != ast.Refs {
		t.Error("expected refs")
	}
	if r2.IsMany {
		t.Error("expected one-to-one")
	}
}

func TestParseDoc(t *testing.T) {
	input := `lyric Test {
  doc "Architecture": """
    This is the architecture.
    It spans lines.
  """
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].Docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(file.Blocks[0].Docs))
	}
	if file.Blocks[0].Docs[0].Section != "Architecture" {
		t.Errorf("expected section Architecture, got %q", file.Blocks[0].Docs[0].Section)
	}
}

func TestParseInvariant(t *testing.T) {
	input := `lyric Test {
  invariant: "count never exceeds max"
    verified_at: "a3f9c12"
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].Invariants) != 1 {
		t.Fatalf("expected 1 invariant, got %d", len(file.Blocks[0].Invariants))
	}
	inv := file.Blocks[0].Invariants[0]
	if inv.Claim != "count never exceeds max" {
		t.Errorf("unexpected claim: %q", inv.Claim)
	}
	if inv.VerifiedAt != "a3f9c12" {
		t.Errorf("unexpected verified_at: %q", inv.VerifiedAt)
	}
}

func TestParseSource(t *testing.T) {
	input := `lyric Test {
  source: ["pkg/foo.go", "pkg/bar.go"]
  fake: "pkg/fake_foo.go"
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].Source) != 2 {
		t.Errorf("expected 2 source files, got %d", len(file.Blocks[0].Source))
	}
	if file.Blocks[0].Fake != "pkg/fake_foo.go" {
		t.Errorf("unexpected fake: %q", file.Blocks[0].Fake)
	}
}

func TestParseImport(t *testing.T) {
	input := `lyric Test {
  import database from "database.lyric"
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(file.Blocks[0].Imports))
	}
	if file.Blocks[0].Imports[0].Alias != "database" || file.Blocks[0].Imports[0].Path != "database.lyric" {
		t.Errorf("unexpected import: %+v", file.Blocks[0].Imports[0])
	}
}

func TestParseGenericClass(t *testing.T) {
	input := `lyric Test {
  class MutableStack<T> {
    items: [T] guarded_by(mu)
    mu: lock

    func push(mut self, item: T) -> unit
      concurrent: true
      excludes_lock(mu)

    func pop(mut self) -> T?
      concurrent: true
      excludes_lock(mu)

    func peek(self) -> T?
      concurrent: true
      excludes_lock(mu)
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cls := file.Blocks[0].Classes[0]
	if cls.Name != "MutableStack" {
		t.Errorf("expected MutableStack, got %s", cls.Name)
	}
	if len(cls.TypeParams) != 1 || cls.TypeParams[0].Name != "T" {
		t.Errorf("expected type param T, got %v", cls.TypeParams)
	}
	if len(cls.Methods) != 3 {
		t.Errorf("expected 3 methods, got %d", len(cls.Methods))
	}
}

func TestParseWhereClause(t *testing.T) {
	input := `lyric Test {
  func factorial(n: T) -> T
    where T: Integer
    requires: n >= 0
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	fn := file.Blocks[0].Functions[0]
	if len(fn.Where) != 1 {
		t.Fatalf("expected 1 where clause, got %d", len(fn.Where))
	}
	if fn.Where[0].Variable != "T" || fn.Where[0].Constraint != "Integer" {
		t.Errorf("unexpected where: %+v", fn.Where[0])
	}
	if len(fn.Annotations.Requires) != 1 {
		t.Errorf("expected 1 requires, got %v", fn.Annotations.Requires)
	}
}

func TestParseClassImplements(t *testing.T) {
	input := `lyric Test {
  class MemBuf implements Reader, Writer {
    data: [u8] guarded_by(mu)
    mu: lock
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cls := file.Blocks[0].Classes[0]
	if len(cls.Implements) != 2 {
		t.Errorf("expected 2 implements, got %v", cls.Implements)
	}
}

func TestParsePureFunc(t *testing.T) {
	input := `lyric Test {
  func transform(xs: [T], f: T -> U) -> [U]
    pure:
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	fn := file.Blocks[0].Functions[0]
	if !fn.Annotations.Pure {
		t.Error("expected pure: true")
	}
}

// TestParseOwnForgeFile parses forge/parser.lyric — the parser's own understanding.
func TestParseOwnForgeFile(t *testing.T) {
	data, err := os.ReadFile("../../forge/parser.lyric")
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	file, err := ParseFile(string(data), "parser.lyric")
	if err != nil {
		t.Fatalf("failed to parse own forge file: %v", err)
	}
	if len(file.Blocks) != 1 {
		t.Fatalf("expected 1 forge block, got %d", len(file.Blocks))
	}
	block := file.Blocks[0]
	if block.Name != "Parser" {
		t.Errorf("expected Parser, got %s", block.Name)
	}

	// Should have structs, enums, classes, and functions
	t.Logf("Parsed: %d structs, %d enums, %d classes, %d interfaces, %d functions, %d relations",
		len(block.Structs), len(block.Enums), len(block.Classes),
		len(block.Interfaces), len(block.Functions), len(block.Relations))

	if len(block.Structs) == 0 {
		t.Error("expected structs")
	}
	if len(block.Enums) == 0 {
		t.Error("expected enums")
	}
	if len(block.Classes) == 0 {
		t.Error("expected classes")
	}
}


func TestLexFString(t *testing.T) {
	input := `f"hello {name}!"`
	lex := NewLexer(input, "test.fg")
	tok := lex.Next()
	if tok.Kind != TFStringLit {
		t.Fatalf("expected TFStringLit, got %v", tokenNames[tok.Kind])
	}
	if tok.Text != "hello {name}!" {
		t.Errorf("expected raw text 'hello {name}!', got %q", tok.Text)
	}
}

func TestParseFString(t *testing.T) {
	input := `lyric test {
  func main() {
    let x = f"hello {name}!"
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	fn := file.Blocks[0].Functions[0]
	if fn.Body == nil || len(fn.Body.Stmts) == 0 {
		t.Fatal("expected function body")
	}
}


func TestParseCastExpr(t *testing.T) {
	input := `lyric test {
  func f() {
    let x = 42 as i64
    let y = x as int
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	fn := file.Blocks[0].Functions[0]
	if fn.Body == nil || len(fn.Body.Stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(fn.Body.Stmts))
	}
	// First: let x = 42 as i64
	decl := fn.Body.Stmts[0].Data.(*ast.VarDeclStmt)
	if decl.Value.Kind != ast.ExprCast {
		t.Fatalf("expected ExprCast, got %v", decl.Value.Kind)
	}
	cast := decl.Value.Data.(*ast.CastExpr)
	if cast.TargetType.Kind != ast.TypeNamed {
		t.Fatalf("expected TypeNamed, got %v", cast.TargetType.Kind)
	}
	nt := cast.TargetType.Data.(ast.NamedType)
	if nt.Name != "i64" {
		t.Errorf("expected target type i64, got %s", nt.Name)
	}
	if cast.Operand.Kind != ast.ExprIntLit {
		t.Errorf("expected IntLit operand, got %v", cast.Operand.Kind)
	}
}

func TestParseFuncTypeParams(t *testing.T) {
	input := `lyric Test {
  func identity<T>(x: T) -> T
  func clamp<T: Comparable>(value: T, lo: T, hi: T) -> T
  func transform<T, U>(xs: [T], f: T -> U) -> [U]
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	fns := file.Blocks[0].Functions
	if len(fns) != 3 {
		t.Fatalf("expected 3 functions, got %d", len(fns))
	}

	// identity<T>
	if len(fns[0].TypeParams) != 1 {
		t.Fatalf("identity: expected 1 type param, got %d", len(fns[0].TypeParams))
	}
	if fns[0].TypeParams[0].Name != "T" {
		t.Errorf("expected T, got %s", fns[0].TypeParams[0].Name)
	}
	if fns[0].TypeParams[0].Constraint != "" {
		t.Errorf("expected no constraint, got %s", fns[0].TypeParams[0].Constraint)
	}

	// clamp<T: Comparable>
	if len(fns[1].TypeParams) != 1 {
		t.Fatalf("clamp: expected 1 type param, got %d", len(fns[1].TypeParams))
	}
	if fns[1].TypeParams[0].Constraint != "Comparable" {
		t.Errorf("expected Comparable, got %s", fns[1].TypeParams[0].Constraint)
	}

	// transform<T, U>
	if len(fns[2].TypeParams) != 2 {
		t.Fatalf("transform: expected 2 type params, got %d", len(fns[2].TypeParams))
	}
	if fns[2].TypeParams[0].Name != "T" || fns[2].TypeParams[1].Name != "U" {
		t.Errorf("expected T, U, got %s, %s", fns[2].TypeParams[0].Name, fns[2].TypeParams[1].Name)
	}
}

func TestParseFnTypeSyntax(t *testing.T) {
	input := `lyric Test {
  func apply(f: func(i32) -> string, x: i32) -> string
  func combine(f: func(i32, i32) -> i32) -> i32
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	fns := file.Blocks[0].Functions
	if len(fns) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(fns))
	}

	// apply: first param should be func(i32) -> string
	fType := fns[0].Params[0].Type
	if fType.Kind != ast.TypeFunc {
		t.Fatalf("expected TypeFunc, got %v", fType.Kind)
	}
	ft := fType.Data.(ast.FuncType)
	if len(ft.Params) != 1 {
		t.Errorf("expected 1 param in fn type, got %d", len(ft.Params))
	}

	// combine: func(i32, i32) -> i32
	fType2 := fns[1].Params[0].Type
	ft2 := fType2.Data.(ast.FuncType)
	if len(ft2.Params) != 2 {
		t.Errorf("expected 2 params in fn type, got %d", len(ft2.Params))
	}
}

func TestParseGenericCallSite(t *testing.T) {
	input := `lyric Test {
  func identity<T>(x: T) -> T {
    return x
  }
  func main() -> unit {
    let a = identity<i32>(42)
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	fns := file.Blocks[0].Functions
	mainFn := fns[1]
	if mainFn.Body == nil || len(mainFn.Body.Stmts) != 1 {
		t.Fatalf("expected 1 statement in main")
	}
	decl := mainFn.Body.Stmts[0].Data.(*ast.VarDeclStmt)
	if decl.Value.Kind != ast.ExprCall {
		t.Fatalf("expected ExprCall, got %v", decl.Value.Kind)
	}
	call := decl.Value.Data.(*ast.CallExpr)
	if len(call.TypeArgs) != 1 {
		t.Fatalf("expected 1 type arg, got %d", len(call.TypeArgs))
	}
	ta := call.TypeArgs[0]
	if ta.Kind != ast.TypeNamed {
		t.Fatalf("expected TypeNamed, got %v", ta.Kind)
	}
	nt := ta.Data.(ast.NamedType)
	if nt.Name != "i32" {
		t.Errorf("expected i32, got %s", nt.Name)
	}
}

func TestParseUnwrapExpr(t *testing.T) {
	input := `lyric test {
  func f(x: i32?) {
    let y = x!
  }
}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	fn := file.Blocks[0].Functions[0]
	if fn.Body == nil || len(fn.Body.Stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(fn.Body.Stmts))
	}
	decl := fn.Body.Stmts[0].Data.(*ast.VarDeclStmt)
	if decl.Value.Kind != ast.ExprUnwrap {
		t.Fatalf("expected ExprUnwrap, got %v", decl.Value.Kind)
	}
	unwrap := decl.Value.Data.(*ast.UnwrapExpr)
	if unwrap.Operand.Kind != ast.ExprIdent {
		t.Fatalf("expected ExprIdent operand, got %v", unwrap.Operand.Kind)
	}
}

func TestParseTypeAlias(t *testing.T) {
	input := `lyric test {
		type StringList = [string]
	}`
	file, err := ParseString(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(file.Blocks[0].TypeAliases) != 1 {
		t.Fatalf("expected 1 type alias, got %d", len(file.Blocks[0].TypeAliases))
	}
	ta := file.Blocks[0].TypeAliases[0]
	if ta.Name != "StringList" {
		t.Errorf("expected name 'StringList', got %q", ta.Name)
	}
}
