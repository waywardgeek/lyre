// Round-trip and rich-doc tests for the shared data model. Added in Phase 1
// of the rich-doc upgrade (see cr/docs/rich-doc-upgrade-plan.md) to lock in
// the breaking change of Fields from map[string]string to []FieldInfo and the
// new rich-doc fields (ModuleWhy, Docs, Invariants, per-decl Why/Source,
// per-field Doc).

package extract

import (
	"encoding/json"
	"reflect"
	"testing"
)

// populatedPackage builds a PackageInfo exercising every rich-doc field at
// least once. Used by both the round-trip test and as documentation of the
// shape the LDL writer will need to handle in Phase 2.
func populatedPackage() *PackageInfo {
	p := NewPackageInfo("checker")
	p.ModuleWhy = "Three-phase type checker with expression annotation."
	p.Docs = []DocBlock{
		{Title: "Architecture", Content: "Phase 0 pre-registers all class names.\nPhase 1 registers fields and methods.\nPhase 2 checks bodies."},
	}
	p.Invariants = []Invariant{
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

	s := NewStructInfo()
	s.IsClass = true
	s.File = "checker.ly"
	s.Line = 147
	s.Why = "Tracks nesting depth inside loops for break/continue validation."
	s.Source = "checker.ly:147"
	s.SetField("errors", "[string]")
	s.SetField("iface_decls", "Dict<Sym, InterfaceDecl>")
	s.SetFieldDoc("iface_decls", "Used during Phase 1.5 to link impl blocks across blocks.")
	s.Methods["CheckFile"] = &FuncInfo{
		SignatureText: "CheckFile(self, file: File)",
		File:          "checker.ly",
		Line:          4695,
		Why:           "Primary entry point. Registers types, then checks bodies.",
		Source:        "checker.ly:4695",
	}
	p.Structs["Checker"] = s

	i := NewInterfaceInfo()
	i.File = "checker.ly"
	i.Line = 200
	i.Why = "Type-checking dispatch surface."
	i.Source = "checker.ly:200"
	p.Interfaces["TypeChecker"] = i

	p.Functions["pkg_init"] = &FuncInfo{
		SignatureText: "pkg_init() -> error",
		File:          "init.ly",
		Line:          1,
		Why:           "Package-level initialization.",
		Source:        "init.ly:1",
	}

	p.TypeDefs["Sym"] = &TypeDefInfo{
		Underlying: "u64",
		Why:        "Interned symbol handle.",
		File:       "sym.ly",
		Line:       12,
		Source:     "sym.ly:12",
	}

	return p
}

// TestPackageInfo_JSONRoundTrip locks in that every rich-doc field marshals
// and unmarshals losslessly. Pre-Phase-1, half of these fields didn't exist.
// Post-Phase-1 they're load-bearing, so a regression here breaks the LDL
// writer in Phase 2.
func TestPackageInfo_JSONRoundTrip(t *testing.T) {
	p := populatedPackage()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got PackageInfo
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(p, &got) {
		t.Fatalf("round-trip mismatch.\n want: %#v\n  got: %#v", p, &got)
	}
}

// TestStructInfo_FieldHelpers verifies the helper methods that bridge legacy
// callers (map-style access) to the new slice-of-FieldInfo storage.
func TestStructInfo_FieldHelpers(t *testing.T) {
	s := NewStructInfo()

	// Empty starts.
	if s.HasField("x") {
		t.Fatalf("empty struct should not have field x")
	}
	if _, ok := s.FieldSig("x"); ok {
		t.Fatalf("empty struct should not have signature for x")
	}

	// SetField appends.
	s.SetField("first", "int")
	s.SetField("second", "string")
	if len(s.Fields) != 2 {
		t.Fatalf("want 2 fields, got %d", len(s.Fields))
	}
	if s.Fields[0].Name != "first" || s.Fields[1].Name != "second" {
		t.Fatalf("source-order broken: %#v", s.Fields)
	}

	// SetField on existing name updates in place (no append).
	s.SetField("first", "int64")
	if len(s.Fields) != 2 {
		t.Fatalf("re-set should not append; got %d fields", len(s.Fields))
	}
	if sig, _ := s.FieldSig("first"); sig != "int64" {
		t.Fatalf("re-set didn't update sig, got %q", sig)
	}

	// SetFieldDoc on existing field preserves SignatureText.
	s.SetFieldDoc("first", "the first field")
	if s.Fields[0].SignatureText != "int64" {
		t.Fatalf("SetFieldDoc clobbered SignatureText: %#v", s.Fields[0])
	}
	if s.Fields[0].Doc != "the first field" {
		t.Fatalf("SetFieldDoc didn't set Doc: %#v", s.Fields[0])
	}

	// SetFieldDoc on missing field creates with empty SignatureText.
	s.SetFieldDoc("third", "doc only")
	if len(s.Fields) != 3 || s.Fields[2].SignatureText != "" || s.Fields[2].Doc != "doc only" {
		t.Fatalf("SetFieldDoc-on-missing broken: %#v", s.Fields)
	}

	// FieldNames preserves source order.
	want := []string{"first", "second", "third"}
	if got := s.FieldNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("FieldNames want %v, got %v", want, got)
	}
}

// TestSortedFieldsByName confirms the helper used by legacy emit code.
func TestSortedFieldsByName(t *testing.T) {
	in := []FieldInfo{
		{Name: "zeta"},
		{Name: "alpha"},
		{Name: "mu"},
	}
	got := SortedFieldsByName(in)
	want := []string{"alpha", "mu", "zeta"}
	for i, f := range got {
		if f.Name != want[i] {
			t.Fatalf("at %d want %s got %s", i, want[i], f.Name)
		}
	}
	// Input must not be mutated.
	if in[0].Name != "zeta" {
		t.Fatalf("SortedFieldsByName mutated input slice")
	}
}

// TestSanitizeModuleName locks in the rule used by Generate*/Extract* paths
// to convert a directory base name into a valid Lyric identifier. The .lyric
// parser's leadingIdentifier rule (pkg/cdd/parser.go) accepts only
// [A-Za-z_][A-Za-z0-9_]*; any directory containing hyphens, dots, spaces,
// etc. used to produce an unparseable `module foo-bar` line.
func TestSanitizeModuleName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"foo-bar", "foo_bar"},
		{"foo.bar", "foo_bar"},
		{"123abc", "_123abc"},
		{"", "_module"},
		{"valid_name", "valid_name"},
		{"with spaces", "with_spaces"},
		{"auth-desktop-callback", "auth_desktop_callback"},
		// Multiple separators collapse to multiple underscores (one per char).
		{"a--b", "a__b"},
		// Already-valid identifiers including digits in the middle survive.
		{"abc123", "abc123"},
		// Leading underscore is preserved.
		{"_private", "_private"},
		// All-invalid input still yields a valid identifier.
		{"---", "___"},
		// Unicode / high bytes are mapped to underscores (we are ASCII-only).
		{"café", "caf__"},
	}
	for _, c := range cases {
		got := SanitizeModuleName(c.in)
		if got != c.want {
			t.Errorf("SanitizeModuleName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanDocLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"// hello world", "hello world"},
		{"// first line\n// second", "first line"},
		{"\n\n// after blanks", "after blanks"},
		{"# python style", "python style"},
		{"/** javadoc */", "javadoc */"}, // /** stripped, */ remains for now
		{"  collapse   internal  whitespace  ", "collapse internal whitespace"},
	}
	for _, tc := range cases {
		got := CleanDocLine(tc.in)
		if got != tc.want {
			t.Errorf("CleanDocLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPreferFresh(t *testing.T) {
	if got := PreferFresh("old", "new"); got != "new" {
		t.Errorf("PreferFresh(old,new) = %q, want new (source wins)", got)
	}
	if got := PreferFresh("old", ""); got != "old" {
		t.Errorf("PreferFresh(old,\"\") = %q, want old (preserve prose when no source comment)", got)
	}
	if got := PreferFresh("old", "  "); got != "old" {
		t.Errorf("PreferFresh(old, whitespace) = %q, want old", got)
	}
}

func TestSeedWhyFromDoc(t *testing.T) {
	p := NewPackageInfo("m")
	s := NewStructInfo()
	s.Doc = "// Widget renders a thing.\n// More detail here."
	s.Methods["Draw"] = &FuncInfo{Doc: "Draw paints the widget."}
	s.Fields = []FieldInfo{{Name: "X", Doc: "the x coord\nwrapped"}}
	p.Structs["Widget"] = s
	ifc := NewInterfaceInfo()
	ifc.Doc = "Drawable can be drawn."
	p.Interfaces["Drawable"] = ifc
	p.Functions["New"] = &FuncInfo{Doc: "New makes one.", Why: "hand-written keep"}
	p.TypeDefs["ID"] = &TypeDefInfo{Doc: "ID identifies a widget."}

	SeedWhyFromDoc(p)

	if s.Why != "Widget renders a thing." {
		t.Errorf("struct Why = %q", s.Why)
	}
	if s.Methods["Draw"].Why != "Draw paints the widget." {
		t.Errorf("method Why = %q", s.Methods["Draw"].Why)
	}
	if s.Fields[0].Doc != "the x coord" {
		t.Errorf("field Doc = %q (want one-line)", s.Fields[0].Doc)
	}
	if ifc.Why != "Drawable can be drawn." {
		t.Errorf("iface Why = %q", ifc.Why)
	}
	if p.Functions["New"].Why != "hand-written keep" {
		t.Errorf("func Why = %q (should not clobber existing prose)", p.Functions["New"].Why)
	}
	if p.TypeDefs["ID"].Why != "ID identifies a widget." {
		t.Errorf("typedef Why = %q", p.TypeDefs["ID"].Why)
	}
}
