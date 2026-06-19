// Spec-by-example tests for the .lyric v2 parser. Each test feeds a tiny
// snippet and asserts the resulting PackageInfo structure. Error tests
// confirm the parser rejects malformed input with line-number-bearing
// messages.

package cdd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/waywardgeek/lyre/pkg/extract"
)

func mustParse(t *testing.T, text string) *extract.PackageInfo {
	t.Helper()
	p, err := Parse(text, "snippet.ly.lyric")
	if err != nil {
		t.Fatalf("Parse failed: %v\n---input---\n%s", err, text)
	}
	return p
}

func mustFail(t *testing.T, text, wantSubstr string) {
	t.Helper()
	_, err := Parse(text, "snippet.ly.lyric")
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("error %q does not contain %q", err.Error(), wantSubstr)
	}
}

// --- positive: each construct -------------------------------------------

func TestParse_MinimalModule(t *testing.T) {
	p := mustParse(t, "module hello\n")
	if p.Name != "hello" {
		t.Fatalf("name = %q", p.Name)
	}
}

func TestParse_ModuleSourceAndWhy(t *testing.T) {
	p := mustParse(t, `module pkg
  source: ["a.go", "b.go"]
  why: "package purpose"
`)
	if !reflect.DeepEqual(p.ModuleSource, []string{"a.go", "b.go"}) {
		t.Fatalf("ModuleSource = %v", p.ModuleSource)
	}
	if p.ModuleWhy != "package purpose" {
		t.Fatalf("ModuleWhy = %q", p.ModuleWhy)
	}
}

func TestParse_DocBlock(t *testing.T) {
	p := mustParse(t, `module m
  doc "Architecture":
    """
    line one
    line two
    """
`)
	if len(p.Docs) != 1 || p.Docs[0].Title != "Architecture" || p.Docs[0].Content != "line one\nline two" {
		t.Fatalf("docs = %#v", p.Docs)
	}
}

func TestParse_DocBlock_PreservesBlankLines(t *testing.T) {
	p := mustParse(t, `module m
  doc "T":
    """
    a

    b
    """
`)
	if got, want := p.Docs[0].Content, "a\n\nb"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestParse_InvariantWithVerifiedBy(t *testing.T) {
	p := mustParse(t, `module m
  invariant "Foo":
    verified-by: TestA, TestB
    """
    body
    """
`)
	if len(p.Invariants) != 1 {
		t.Fatalf("invariants = %v", p.Invariants)
	}
	inv := p.Invariants[0]
	if inv.Title != "Foo" || inv.Content != "body" || inv.Procedural {
		t.Fatalf("inv = %#v", inv)
	}
	if !reflect.DeepEqual(inv.VerifiedBy, []string{"TestA", "TestB"}) {
		t.Fatalf("verified-by = %v", inv.VerifiedBy)
	}
}

func TestParse_InvariantProcedural(t *testing.T) {
	p := mustParse(t, `module m
  invariant "Bar":
    procedural
    """
    rule
    """
`)
	if !p.Invariants[0].Procedural {
		t.Fatal("expected procedural")
	}
}

func TestParse_ClassWithFieldsAndMethod(t *testing.T) {
	p := mustParse(t, `module m
  class Foo
    source: foo.ly:10
    why: "the foo class"
    field a: int
    field b: string
      doc: "the b field"
    method Bar(self)
      source: foo.ly:20
      why: "do the bar"
`)
	s := p.Structs["Foo"]
	if s == nil || !s.IsClass {
		t.Fatalf("class Foo not parsed: %v", s)
	}
	if s.Source != "foo.ly:10" || s.File != "foo.ly" || s.Line != 10 {
		t.Fatalf("source not split: %#v", s)
	}
	if s.Why != "the foo class" {
		t.Fatalf("why = %q", s.Why)
	}
	if len(s.Fields) != 2 {
		t.Fatalf("fields = %v", s.Fields)
	}
	if s.Fields[0] != (extract.FieldInfo{Name: "a", SignatureText: "int"}) {
		t.Fatalf("field 0: %#v", s.Fields[0])
	}
	if s.Fields[1] != (extract.FieldInfo{Name: "b", SignatureText: "string", Doc: "the b field"}) {
		t.Fatalf("field 1: %#v", s.Fields[1])
	}
	m := s.Methods["Bar"]
	if m == nil || m.SignatureText != "Bar(self)" || m.Why != "do the bar" || m.Source != "foo.ly:20" || m.File != "foo.ly" || m.Line != 20 {
		t.Fatalf("method: %#v", m)
	}
}

func TestParse_StructIsClassFalse(t *testing.T) {
	p := mustParse(t, "module m\n  struct V\n    field x: int\n")
	if p.Structs["V"].IsClass {
		t.Fatal("struct should not be IsClass")
	}
}

func TestParse_Interface(t *testing.T) {
	p := mustParse(t, `module m
  interface Reader
    why: "read stuff"
    method Read(self, b: bytes) -> int
`)
	r := p.Interfaces["Reader"]
	if r == nil || r.Why != "read stuff" {
		t.Fatalf("interface: %#v", r)
	}
	if r.Methods["Read"].SignatureText != "Read(self, b: bytes) -> int" {
		t.Fatalf("method sig: %q", r.Methods["Read"].SignatureText)
	}
}

func TestParse_FuncTopLevel(t *testing.T) {
	p := mustParse(t, `module m
  func main() -> int
    source: main.ly:1
    why: "entry"
`)
	f := p.Functions["main"]
	if f == nil || f.SignatureText != "main() -> int" || f.Source != "main.ly:1" {
		t.Fatalf("func: %#v", f)
	}
}

func TestParse_Typedef(t *testing.T) {
	p := mustParse(t, `module m
  typedef Sym: u64
    why: "symbol"
`)
	td := p.TypeDefs["Sym"]
	if td == nil || td.Underlying != "u64" || td.Why != "symbol" {
		t.Fatalf("typedef: %#v", td)
	}
}

func TestParse_TypedefNoUnderlying(t *testing.T) {
	p := mustParse(t, "module m\n  typedef Opaque\n")
	td := p.TypeDefs["Opaque"]
	if td == nil || td.Underlying != "" {
		t.Fatalf("typedef: %#v", td)
	}
}

func TestParse_CommentsAndBlanksIgnored(t *testing.T) {
	p := mustParse(t, `module m
  # this is a comment
  why: "x"

  # blank above

  class Foo
    field a: int
`)
	if p.ModuleWhy != "x" || p.Structs["Foo"] == nil {
		t.Fatalf("parsed: %#v", p)
	}
}

func TestParse_QuotedStringEscapes(t *testing.T) {
	p := mustParse(t, `module m
  why: "has \"quoted\" and \\ backslash"
`)
	if p.ModuleWhy != `has "quoted" and \ backslash` {
		t.Fatalf("why = %q", p.ModuleWhy)
	}
}

func TestParse_WorkedExample(t *testing.T) {
	// Smaller version of spec §8 covering the major constructs at once.
	text := `module checker
  source: ["checker.ly"]
  why: "type checker"

  doc "Architecture":
    """
    Phase 0 pre-registers.
    """

  invariant "Ordering":
    verified-by: TestOrdering
    """
    P0 before P1.
    """

  class Checker
    source: checker.ly:147
    field errors: [string]
    method Run(self)
`
	p := mustParse(t, text)
	if p.Name != "checker" || p.ModuleWhy != "type checker" || len(p.Docs) != 1 || len(p.Invariants) != 1 {
		t.Fatalf("module: %#v", p)
	}
	if p.Structs["Checker"].Fields[0].Name != "errors" || p.Structs["Checker"].Methods["Run"] == nil {
		t.Fatalf("checker: %#v", p.Structs["Checker"])
	}
}

// --- negative: error reporting ------------------------------------------

func TestParse_Error_TabInIndent(t *testing.T) {
	mustFail(t, "module m\n\tfield a: int\n", "tab in indentation")
}

func TestParse_Error_OddIndent(t *testing.T) {
	mustFail(t, "module m\n why: \"x\"\n", "odd number of leading spaces")
}

func TestParse_Error_UnrecognizedKey(t *testing.T) {
	mustFail(t, "module m\n  garbage: x\n", "unrecognized block head or key")
}

func TestParse_Error_UnterminatedHeredoc(t *testing.T) {
	mustFail(t, `module m
  doc "T":
    """
    a
    b
`, "unterminated heredoc")
}

func TestParse_Error_NoModule(t *testing.T) {
	mustFail(t, "class Foo\n  field a: int\n", "expected `module <name>`")
}

func TestParse_Error_FieldWhyRejected(t *testing.T) {
	mustFail(t, `module m
  class Foo
    field a: int
      why: "no field whys"
`, "`why:` is not valid at field scope")
}

func TestParse_Error_HeredocUnderIndented(t *testing.T) {
	// Heredoc body line at fewer than the opener's indent → error.
	mustFail(t, `module m
  doc "T":
    """
   short
    """
`, "indented less than opener")
}

func TestParse_Error_TitleMissingColon(t *testing.T) {
	mustFail(t, `module m
  doc "T"
    """
    x
    """
`, "expected `:` after doc title")
}
