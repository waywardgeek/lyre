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

// TestWrite_MultiLineSignatureFlattened covers the regression where the
// TypeScript extractor copies a multi-line inline-object type literal
// verbatim into FieldInfo.SignatureText / FuncInfo.SignatureText (e.g.
// `sessionCost: {\n    totalCostUSD: number;\n    totalInputTokens: number;\n  }`),
// and Write then emitted it across multiple physical lines. The .lyric v2
// grammar (spec §3) is line-oriented: any deeper-indented follow-up of a
// `field`/`method`/`func`/`typedef` head is parsed as a child key, so the
// just-written file was rejected by Parse on its next read — breaking the
// round-trip property and making `lyre verify` fail on bare scaffolds.
// Write must collapse internal whitespace/newlines so the head occupies
// exactly one physical line, and Parse(Write(p)) must round-trip.
func TestWrite_MultiLineSignatureFlattened(t *testing.T) {
	p := extract.NewPackageInfo("ts")
	s := extract.NewStructInfo()
	s.IsClass = true
	s.File = "foo.ts"
	s.Line = 1
	s.Source = "foo.ts:1"
	// Field signature with embedded newlines (TS inline object literal).
	s.SetField("sessionCost", "{\n    totalCostUSD: number;\n    totalInputTokens: number;\n  }")
	// Method signature with a parameter that spans lines (also seen in TS).
	s.Methods["update"] = &extract.FuncInfo{
		SignatureText: "update(opts: {\n    a: number;\n    b: string;\n  }): void",
		File:          "foo.ts",
		Line:          8,
		Source:        "foo.ts:8",
	}
	p.Structs["Foo"] = s
	// Free function with multi-line return-type object.
	p.Functions["mk"] = &extract.FuncInfo{
		SignatureText: "mk(): {\n    x: number;\n    y: number;\n  }",
		File:          "foo.ts",
		Line:          20,
		Source:        "foo.ts:20",
	}
	// Typedef whose underlying contains an embedded newline.
	p.TypeDefs["Point"] = &extract.TypeDefInfo{
		Underlying: "{\n    x: number;\n    y: number;\n  }",
		File:       "foo.ts",
		Line:       30,
		Source:     "foo.ts:30",
	}

	text := Write(p)

	// Every head line introducing a field/method/func/typedef body must be
	// a single physical line — no embedded newlines allowed in the head.
	for i, ln := range strings.Split(text, "\n") {
		trim := strings.TrimLeft(ln, " ")
		for _, kw := range []string{"field ", "method ", "func ", "typedef "} {
			if strings.HasPrefix(trim, kw) {
				// trim already excludes the leading indent; the line itself
				// is by construction one physical line. The real test is
				// that Parse below succeeds. But also guard the obvious:
				// the head must not be empty after the keyword.
				if strings.TrimSpace(trim) == strings.TrimSpace(kw) {
					t.Errorf("line %d: empty head after %q: %q", i+1, kw, ln)
				}
			}
		}
	}

	// Round-trip: Parse must accept what Write just emitted.
	got, err := Parse(text, "test.ly.lyric")
	if err != nil {
		t.Fatalf("Parse rejected writer output: %v\n--- text ---\n%s", err, text)
	}

	// Field, method, func, typedef signatures must all be collapsed to a
	// single line with single-spaced internal whitespace.
	gf := got.Structs["Foo"]
	if gf == nil {
		t.Fatalf("struct Foo missing")
	}
	if len(gf.Fields) != 1 {
		t.Fatalf("want 1 field, got %d", len(gf.Fields))
	}
	wantField := "{ totalCostUSD: number; totalInputTokens: number; }"
	if gf.Fields[0].SignatureText != wantField {
		t.Errorf("field sig: want %q, got %q", wantField, gf.Fields[0].SignatureText)
	}
	wantMethod := "update(opts: { a: number; b: string; }): void"
	if gm := gf.Methods["update"]; gm == nil {
		t.Errorf("method update missing")
	} else if gm.SignatureText != wantMethod {
		t.Errorf("method sig: want %q, got %q", wantMethod, gm.SignatureText)
	}
	wantFunc := "mk(): { x: number; y: number; }"
	if gfn := got.Functions["mk"]; gfn == nil {
		t.Errorf("func mk missing")
	} else if gfn.SignatureText != wantFunc {
		t.Errorf("func sig: want %q, got %q", wantFunc, gfn.SignatureText)
	}
	wantTypedef := "{ x: number; y: number; }"
	if td := got.TypeDefs["Point"]; td == nil {
		t.Errorf("typedef Point missing")
	} else if td.Underlying != wantTypedef {
		t.Errorf("typedef underlying: want %q, got %q", wantTypedef, td.Underlying)
	}
}
