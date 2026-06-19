package lint

import (
	"strings"
	"testing"

	"github.com/waywardgeek/lyre/pkg/extract"
)

// cleanPackage builds a PackageInfo that should produce ZERO findings.
// Individual tests mutate clones of it to trigger specific warnings.
func cleanPackage() *extract.PackageInfo {
	p := extract.NewPackageInfo("clean")
	p.ModuleWhy = "A small, complete module that should pass all lint checks."
	p.Docs = []extract.DocBlock{
		{Title: "Architecture", Content: "Simple single-class design."},
	}
	p.Invariants = []extract.Invariant{
		{
			Title:      "OneAndOnly",
			Content:    "Documented and tested.",
			VerifiedBy: []string{"TestInvariant_OneAndOnly"},
		},
		{
			Title:      "Procedural",
			Content:    "Use &slice[i], never range copies.",
			Procedural: true,
		},
	}
	// One class with 4 methods, all with why:.
	c := extract.NewStructInfo()
	c.IsClass = true
	c.Why = "Owns one piece of state."
	c.Methods["Start"] = &extract.FuncInfo{SignatureText: "Start()", Why: "Begin."}
	c.Methods["Stop"] = &extract.FuncInfo{SignatureText: "Stop()", Why: "End."}
	c.Methods["Step"] = &extract.FuncInfo{SignatureText: "Step()", Why: "One tick."}
	c.Methods["Reset"] = &extract.FuncInfo{SignatureText: "Reset()", Why: "Clear."}
	p.Structs["Worker"] = c
	return p
}

func findingCodes(r *Result) []string {
	out := make([]string, 0, len(r.Findings))
	for _, f := range r.Findings {
		out = append(out, f.Code)
	}
	return out
}

func hasCode(r *Result, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestLint_CleanPackage(t *testing.T) {
	r := Lint(cleanPackage(), "clean.lyric", Opts{})
	if len(r.Findings) != 0 {
		t.Fatalf("clean package produced findings: %v", findingCodes(r))
	}
}

func TestLint_W001_EmptyModuleWhy(t *testing.T) {
	p := cleanPackage()
	p.ModuleWhy = "   "
	r := Lint(p, "f.lyric", Opts{})
	if !hasCode(r, "W001") {
		t.Fatalf("expected W001, got %v", findingCodes(r))
	}
}

func TestLint_W002_NoArchitectureDoc(t *testing.T) {
	p := cleanPackage()
	p.Docs = nil
	r := Lint(p, "f.lyric", Opts{})
	if !hasCode(r, "W002") {
		t.Fatalf("expected W002, got %v", findingCodes(r))
	}
}

func TestLint_W002_AcceptsCaseInsensitive(t *testing.T) {
	p := cleanPackage()
	p.Docs = []extract.DocBlock{{Title: "architecture", Content: "x"}}
	r := Lint(p, "f.lyric", Opts{})
	if hasCode(r, "W002") {
		t.Fatalf("W002 should be case-insensitive on title; got %v", findingCodes(r))
	}
}

func TestLint_W003_HeavyClassNoInvariants(t *testing.T) {
	p := cleanPackage()
	p.Invariants = nil
	// Worker already has 4 methods → ≥3 → triggers W003.
	r := Lint(p, "f.lyric", Opts{})
	if !hasCode(r, "W003") {
		t.Fatalf("expected W003, got %v", findingCodes(r))
	}
}

func TestLint_W003_LightModuleNoTrigger(t *testing.T) {
	p := cleanPackage()
	p.Invariants = nil
	// Reduce methods below 3.
	c := p.Structs["Worker"]
	delete(c.Methods, "Reset")
	delete(c.Methods, "Step")
	r := Lint(p, "f.lyric", Opts{})
	if hasCode(r, "W003") {
		t.Fatalf("light module should not trigger W003; got %v", findingCodes(r))
	}
}

func TestLint_W003_TriggeredByInterface(t *testing.T) {
	p := extract.NewPackageInfo("x")
	p.ModuleWhy = "y"
	p.Docs = []extract.DocBlock{{Title: "Architecture", Content: "z"}}
	ifc := extract.NewInterfaceInfo()
	ifc.Methods["A"] = &extract.FuncInfo{SignatureText: "A()"}
	ifc.Methods["B"] = &extract.FuncInfo{SignatureText: "B()"}
	ifc.Methods["C"] = &extract.FuncInfo{SignatureText: "C()"}
	p.Interfaces["Big"] = ifc
	r := Lint(p, "f.lyric", Opts{})
	if !hasCode(r, "W003") {
		t.Fatalf("expected W003 from heavy interface, got %v", findingCodes(r))
	}
}

func TestLint_W004_HeavyClassNoMethodWhy(t *testing.T) {
	p := cleanPackage()
	for _, m := range p.Structs["Worker"].Methods {
		m.Why = ""
	}
	r := Lint(p, "f.lyric", Opts{})
	if !hasCode(r, "W004") {
		t.Fatalf("expected W004, got %v", findingCodes(r))
	}
}

func TestLint_W004_OneMethodWhyIsEnough(t *testing.T) {
	p := cleanPackage()
	// Strip all but one why:.
	first := true
	for _, m := range p.Structs["Worker"].Methods {
		if first {
			first = false
			continue
		}
		m.Why = ""
	}
	r := Lint(p, "f.lyric", Opts{})
	if hasCode(r, "W004") {
		t.Fatalf("one why: should suffice; got %v", findingCodes(r))
	}
}

func TestLint_W005_EnumFieldNoDoc(t *testing.T) {
	p := cleanPackage()
	p.TypeDefs["TokenKind"] = &extract.TypeDefInfo{Underlying: "int"}
	s := extract.NewStructInfo()
	s.Why = "x"
	s.Fields = []extract.FieldInfo{
		{Name: "kind", SignatureText: "TokenKind"},
		{Name: "bits", SignatureText: "i32"},
		{Name: "name", SignatureText: "string"},
	}
	p.Structs["Type"] = s
	r := Lint(p, "f.lyric", Opts{})
	if !hasCode(r, "W005") {
		t.Fatalf("expected W005, got %v", findingCodes(r))
	}
}

func TestLint_W005_NoEnumNoTrigger(t *testing.T) {
	p := cleanPackage()
	s := extract.NewStructInfo()
	s.Fields = []extract.FieldInfo{
		{Name: "a", SignatureText: "int"},
		{Name: "b", SignatureText: "string"},
		{Name: "c", SignatureText: "bool"},
	}
	p.Structs["Plain"] = s
	r := Lint(p, "f.lyric", Opts{})
	if hasCode(r, "W005") {
		t.Fatalf("W005 should not fire without an enum field; got %v", findingCodes(r))
	}
}

func TestLint_W005_AnyFieldDocSuppresses(t *testing.T) {
	p := cleanPackage()
	p.TypeDefs["K"] = &extract.TypeDefInfo{Underlying: "int"}
	s := extract.NewStructInfo()
	s.Fields = []extract.FieldInfo{
		{Name: "kind", SignatureText: "K", Doc: "the kind"},
		{Name: "x", SignatureText: "int"},
		{Name: "y", SignatureText: "int"},
	}
	p.Structs["T"] = s
	r := Lint(p, "f.lyric", Opts{})
	if hasCode(r, "W005") {
		t.Fatalf("any field doc: should suppress W005; got %v", findingCodes(r))
	}
}

func TestLint_W006_InvariantWithoutVerification(t *testing.T) {
	p := cleanPackage()
	p.Invariants = []extract.Invariant{
		{Title: "Bare", Content: "Nothing backs this."},
	}
	r := Lint(p, "f.lyric", Opts{})
	if !hasCode(r, "W006") {
		t.Fatalf("expected W006, got %v", findingCodes(r))
	}
}

func TestLint_W006_ProceduralSuppresses(t *testing.T) {
	p := cleanPackage()
	p.Invariants = []extract.Invariant{
		{Title: "Hand", Content: "x", Procedural: true},
	}
	r := Lint(p, "f.lyric", Opts{})
	if hasCode(r, "W006") {
		t.Fatalf("procedural should suppress W006; got %v", findingCodes(r))
	}
}

func TestLint_W007_UnknownTest(t *testing.T) {
	p := cleanPackage()
	// cleanPackage's OneAndOnly invariant says verified-by TestInvariant_OneAndOnly.
	known := map[string]bool{"TestSomethingElse": true}
	r := Lint(p, "f.lyric", Opts{KnownTests: known})
	if !hasCode(r, "W007") {
		t.Fatalf("expected W007, got %v", findingCodes(r))
	}
}

func TestLint_W007_KnownTestSuppresses(t *testing.T) {
	p := cleanPackage()
	known := map[string]bool{"TestInvariant_OneAndOnly": true}
	r := Lint(p, "f.lyric", Opts{KnownTests: known})
	if hasCode(r, "W007") {
		t.Fatalf("known test should suppress W007; got %v", findingCodes(r))
	}
}

func TestLint_W007_DormantWhenNilSet(t *testing.T) {
	p := cleanPackage()
	r := Lint(p, "f.lyric", Opts{KnownTests: nil})
	if hasCode(r, "W007") {
		t.Fatalf("nil KnownTests should disable W007; got %v", findingCodes(r))
	}
}

func TestLint_W008_TODOInModuleWhy(t *testing.T) {
	p := cleanPackage()
	p.ModuleWhy = "Explain me TODO"
	r := Lint(p, "f.lyric", Opts{})
	if !hasCode(r, "W008") {
		t.Fatalf("expected W008, got %v", findingCodes(r))
	}
}

func TestLint_W008_TODOInDoc(t *testing.T) {
	p := cleanPackage()
	p.Docs[0].Content = "TODO write this section"
	r := Lint(p, "f.lyric", Opts{})
	if !hasCode(r, "W008") {
		t.Fatalf("expected W008, got %v", findingCodes(r))
	}
}

func TestLint_W008_TODOInMethodWhy(t *testing.T) {
	p := cleanPackage()
	p.Structs["Worker"].Methods["Start"].Why = "TODO: explain"
	r := Lint(p, "f.lyric", Opts{})
	if !hasCode(r, "W008") {
		t.Fatalf("expected W008, got %v", findingCodes(r))
	}
}

func TestLint_W008_TODOInFieldDoc(t *testing.T) {
	p := cleanPackage()
	s := extract.NewStructInfo()
	s.Fields = []extract.FieldInfo{{Name: "x", SignatureText: "int", Doc: "TODO describe"}}
	p.Structs["X"] = s
	r := Lint(p, "f.lyric", Opts{})
	if !hasCode(r, "W008") {
		t.Fatalf("expected W008, got %v", findingCodes(r))
	}
}

func TestLint_W008_CaseSensitive(t *testing.T) {
	p := cleanPackage()
	p.ModuleWhy = "lowercase todo is fine, that's just a word"
	r := Lint(p, "f.lyric", Opts{})
	if hasCode(r, "W008") {
		t.Fatalf("lowercase 'todo' should not trigger W008; got %v", findingCodes(r))
	}
}

func TestLint_FindingFormatting(t *testing.T) {
	f := Finding{
		Code: "W001", Severity: SevWarning,
		File: "x.lyric", Where: "module",
		Message: "missing why:",
	}
	got := f.String()
	if !strings.Contains(got, "W001") || !strings.Contains(got, "WARNING") || !strings.Contains(got, "x.lyric") {
		t.Fatalf("unexpected format: %q", got)
	}
}

func TestLint_DeterministicOrder(t *testing.T) {
	// Build a package that triggers W001 and W002 and W008 — verify order is stable.
	p := extract.NewPackageInfo("d")
	// ModuleWhy empty → W001.
	// No Architecture doc → W002.
	// TODO in nothing else; add a doc with TODO to also trigger W008.
	p.Docs = []extract.DocBlock{{Title: "Intro", Content: "TODO body"}}
	r1 := Lint(p, "f.lyric", Opts{})
	r2 := Lint(p, "f.lyric", Opts{})
	if len(r1.Findings) != len(r2.Findings) {
		t.Fatalf("len mismatch: %d vs %d", len(r1.Findings), len(r2.Findings))
	}
	for i := range r1.Findings {
		if r1.Findings[i] != r2.Findings[i] {
			t.Fatalf("non-deterministic at %d: %v vs %v", i, r1.Findings[i], r2.Findings[i])
		}
	}
	// Check sort: codes should be non-decreasing.
	for i := 1; i < len(r1.Findings); i++ {
		if r1.Findings[i-1].Code > r1.Findings[i].Code {
			t.Fatalf("findings not sorted by code: %v", findingCodes(r1))
		}
	}
}

func TestLint_WarningCount(t *testing.T) {
	r := &Result{Findings: []Finding{
		{Code: "W001", Severity: SevWarning},
		{Code: "W002", Severity: SevWarning},
		{Code: "W008", Severity: SevInfo},
	}}
	if got := r.WarningCount(); got != 2 {
		t.Fatalf("WarningCount = %d, want 2", got)
	}
}
