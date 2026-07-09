package gen

import (
	"strings"
	"testing"

	"github.com/waywardgeek/lyre/pkg/extract"
	"github.com/waywardgeek/lyre/pkg/lint"
)

// emptyPackage builds an empty PackageInfo with just a name.
func emptyPackage() *extract.PackageInfo {
	return extract.NewPackageInfo("mymod")
}

// richPackage builds a package with one heavy class and two methods, all
// fields pre-seeded with native-source Doc comments. Used to exercise the
// Doc→Why fallback path.
func richPackage() *extract.PackageInfo {
	p := extract.NewPackageInfo("checker")
	c := extract.NewStructInfo()
	c.IsClass = true
	c.Doc = "// Checker owns per-package type-checking state."
	c.Methods["CheckFile"] = &extract.FuncInfo{
		SignatureText: "CheckFile(self, file: File)",
		Doc:           "// CheckFile is the primary entry point.\n// Registers types, then bodies.",
	}
	c.Methods["CheckFiles"] = &extract.FuncInfo{
		SignatureText: "CheckFiles(self, files: [File])",
	}
	c.Fields = []extract.FieldInfo{
		{Name: "kind", SignatureText: "TypeKind", Doc: "// the type kind"},
	}
	p.Structs["Checker"] = c
	return p
}

func TestSeedRich_EmptyPackage(t *testing.T) {
	p := emptyPackage()
	SeedRichPlaceholders(p)
	if !strings.Contains(p.ModuleWhy, "TODO") {
		t.Errorf("ModuleWhy not seeded: %q", p.ModuleWhy)
	}
	if !strings.Contains(p.ModuleWhy, "mymod") {
		t.Errorf("ModuleWhy missing module name: %q", p.ModuleWhy)
	}
	if !hasDocTitle(p.Docs, "Architecture") {
		t.Errorf("Architecture doc block not seeded")
	}
}

func TestSeedRich_Idempotent(t *testing.T) {
	p := emptyPackage()
	SeedRichPlaceholders(p)
	SeedRichPlaceholders(p)
	if len(p.Docs) != 1 {
		t.Errorf("expected 1 doc block after double-seed, got %d", len(p.Docs))
	}
}

func TestSeedRich_PreservesExisting(t *testing.T) {
	p := emptyPackage()
	p.ModuleWhy = "Real prose."
	p.Docs = []extract.DocBlock{{Title: "Architecture", Content: "Real architecture."}}
	c := extract.NewStructInfo()
	c.Why = "Real class why."
	c.Methods["Foo"] = &extract.FuncInfo{Why: "Real method why."}
	p.Structs["C"] = c

	SeedRichPlaceholders(p)

	if p.ModuleWhy != "Real prose." {
		t.Errorf("ModuleWhy overwritten: %q", p.ModuleWhy)
	}
	if len(p.Docs) != 1 || p.Docs[0].Content != "Real architecture." {
		t.Errorf("Architecture doc altered: %+v", p.Docs)
	}
	if p.Structs["C"].Why != "Real class why." {
		t.Errorf("class Why overwritten: %q", p.Structs["C"].Why)
	}
	if p.Structs["C"].Methods["Foo"].Why != "Real method why." {
		t.Errorf("method Why overwritten")
	}
}

func TestSeedRich_ArchitectureCaseInsensitive(t *testing.T) {
	p := emptyPackage()
	p.Docs = []extract.DocBlock{{Title: "architecture", Content: "lowercase title"}}
	SeedRichPlaceholders(p)
	if len(p.Docs) != 1 {
		t.Errorf("expected 1 doc block (case-insensitive match), got %d: %+v", len(p.Docs), p.Docs)
	}
}

func TestSeedRich_SeedsFromDoc(t *testing.T) {
	p := richPackage()
	SeedRichPlaceholders(p)
	c := p.Structs["Checker"]
	if c.Why != "Checker owns per-package type-checking state." {
		t.Errorf("class Why not seeded from Doc: %q", c.Why)
	}
	m1 := c.Methods["CheckFile"]
	if m1.Why != "CheckFile is the primary entry point." {
		t.Errorf("method Why not seeded from first line of Doc: %q", m1.Why)
	}
	m2 := c.Methods["CheckFiles"]
	if !strings.Contains(m2.Why, "TODO") || !strings.Contains(m2.Why, "Checker.CheckFiles") {
		t.Errorf("method without Doc should get TODO: %q", m2.Why)
	}
}

func TestSeedRich_CleansFieldDoc(t *testing.T) {
	p := richPackage()
	SeedRichPlaceholders(p)
	f := p.Structs["Checker"].Fields[0]
	if f.Doc != "the type kind" {
		t.Errorf("field Doc not cleaned: %q", f.Doc)
	}
}

func TestSeedRich_LintContract_EmptyPackage(t *testing.T) {
	// Contract: after seeding an empty package, lint reports ONLY W008
	// findings (the TODO placeholders themselves). W001/W002/W004 are
	// satisfied. W003/W005/W006/W007 are dormant (no heavy classes, no
	// enum fields, no invariants, no KnownTests).
	p := emptyPackage()
	SeedRichPlaceholders(p)
	r := lint.Lint(p, "f.lyric", lint.Opts{})
	for _, f := range r.Findings {
		if f.Code != "W008" {
			t.Errorf("unexpected non-W008 finding after seed: %s", f)
		}
	}
	// And at least one W008 should fire (we seeded TODOs).
	got := false
	for _, f := range r.Findings {
		if f.Code == "W008" {
			got = true
			break
		}
	}
	if !got {
		t.Errorf("expected at least one W008 after seeding empty package")
	}
}

func TestSeedRich_LintContract_HeavyClass(t *testing.T) {
	// With a heavy class (≥3 methods) and no invariants, W003 fires by
	// design — seeding does NOT manufacture invariants.
	p := emptyPackage()
	c := extract.NewStructInfo()
	c.IsClass = true
	c.Methods["A"] = &extract.FuncInfo{SignatureText: "A()"}
	c.Methods["B"] = &extract.FuncInfo{SignatureText: "B()"}
	c.Methods["C"] = &extract.FuncInfo{SignatureText: "C()"}
	c.Methods["D"] = &extract.FuncInfo{SignatureText: "D()"}
	p.Structs["Big"] = c
	SeedRichPlaceholders(p)
	r := lint.Lint(p, "f.lyric", lint.Opts{})
	// Must NOT produce W001, W002, W004.
	for _, f := range r.Findings {
		switch f.Code {
		case "W001", "W002", "W004":
			t.Errorf("seed should have satisfied %s: %s", f.Code, f)
		}
	}
	// SHOULD produce W003 (heavy class, no invariants).
	got := false
	for _, f := range r.Findings {
		if f.Code == "W003" {
			got = true
		}
	}
	if !got {
		t.Errorf("expected W003 (heavy class, no invariants) after seeding")
	}
}
