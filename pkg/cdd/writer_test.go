// Round-trip and determinism tests for the .lyric v2 writer.
// Phase 2 acceptance: Parse(Write(p)) deep-equals p for any well-formed
// PackageInfo whose round-trippable fields are set. The canonical fixture
// is extract.populatedPackage() in pkg/extract/extract_test.go.
//
// Because populatedPackage is unexported (package extract), this file
// constructs an equivalent fixture locally.

package cdd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/waywardgeek/lyre/pkg/extract"
)

// canonicalPackage mirrors extract.populatedPackage but uses only the fields
// that round-trip through `.lyric` v2. It also exercises ModuleSource (which
// the extract package's fixture doesn't set, because that field was added in
// Phase 2).
func canonicalPackage() *extract.PackageInfo {
	p := extract.NewPackageInfo("checker")
	p.ModuleSource = []string{"checker.ly", "checker_helpers.ly"}
	p.ModuleWhy = "Three-phase type checker with expression annotation."
	p.Docs = []extract.DocBlock{
		{Title: "Architecture", Content: "Phase 0 pre-registers all class names.\nPhase 1 registers fields and methods.\nPhase 2 checks bodies."},
	}
	p.Invariants = []extract.Invariant{
		{
			Title:      "Three-Phase Ordering",
			Content:    "Phase 0 MUST complete on ALL blocks before ANY Phase 1 begins.",
			VerifiedBy: []string{"TestInvariant_Checker_ThreePhaseOrdering"},
		},
		{
			Title:      "AST Expr Pointer Stability",
			Content:    "Use &slice[i], never range copies, because checkExpr annotates ResolvedType.",
			Procedural: true,
		},
	}

	s := extract.NewStructInfo()
	s.IsClass = true
	s.File = "checker.ly"
	s.Line = 147
	s.Why = "Tracks nesting depth inside loops for break/continue validation."
	s.Source = "checker.ly:147"
	s.SetField("errors", "[string]")
	s.SetField("iface_decls", "Dict<Sym, InterfaceDecl>")
	s.SetFieldDoc("iface_decls", "Used during Phase 1.5 to link impl blocks across blocks.")
	s.Methods["CheckFile"] = &extract.FuncInfo{
		SignatureText: "CheckFile(self, file: File)",
		File:          "checker.ly",
		Line:          4695,
		Why:           "Primary entry point. Registers types, then checks bodies.",
		Source:        "checker.ly:4695",
	}
	p.Structs["Checker"] = s

	i := extract.NewInterfaceInfo()
	i.File = "checker.ly"
	i.Line = 200
	i.Why = "Type-checking dispatch surface."
	i.Source = "checker.ly:200"
	p.Interfaces["TypeChecker"] = i

	p.Functions["pkg_init"] = &extract.FuncInfo{
		SignatureText: "pkg_init() -> error",
		File:          "init.ly",
		Line:          1,
		Why:           "Package-level initialization.",
		Source:        "init.ly:1",
	}

	p.TypeDefs["Sym"] = &extract.TypeDefInfo{
		Underlying: "u64",
		Why:        "Interned symbol handle.",
		File:       "sym.ly",
		Line:       12,
		Source:     "sym.ly:12",
	}

	return p
}

// TestRoundTrip_CanonicalPackage is the Phase 2 acceptance test:
// Parse(Write(p)) is structurally equal to p.
func TestRoundTrip_CanonicalPackage(t *testing.T) {
	want := canonicalPackage()
	text := Write(want)
	got, err := Parse(text, "test.ly.lyric")
	if err != nil {
		t.Fatalf("Parse failed: %v\n\n--- written text ---\n%s", err, text)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("round-trip mismatch.\n--- want ---\n%#v\n--- got ---\n%#v\n--- text ---\n%s", want, got, text)
	}
}

// TestWrite_Deterministic verifies the same input produces byte-identical
// output on repeated calls. (Map iteration order is randomized in Go, so this
// would catch any unsorted code path.)
func TestWrite_Deterministic(t *testing.T) {
	p := canonicalPackage()
	first := Write(p)
	for i := 0; i < 20; i++ {
		if got := Write(p); got != first {
			t.Fatalf("non-deterministic output on iteration %d:\n--- first ---\n%s\n--- got ---\n%s", i, first, got)
		}
	}
}

// TestWrite_NoTrailingWhitespace verifies no emitted line has trailing spaces.
func TestWrite_NoTrailingWhitespace(t *testing.T) {
	out := Write(canonicalPackage())
	for i, ln := range strings.Split(out, "\n") {
		if strings.TrimRight(ln, " \t") != ln {
			t.Errorf("line %d has trailing whitespace: %q", i+1, ln)
		}
	}
}

// TestWrite_ExactlyOneFinalNewline verifies the output ends with exactly one LF.
func TestWrite_ExactlyOneFinalNewline(t *testing.T) {
	out := Write(canonicalPackage())
	if len(out) == 0 || out[len(out)-1] != '\n' {
		t.Fatalf("output does not end with newline")
	}
	if len(out) >= 2 && out[len(out)-2] == '\n' {
		t.Fatalf("output ends with multiple newlines")
	}
}
