// Tests for the v2 .go.lyric pipeline: ExtractGo, GenerateGo, UpdateGo,
// VerifyGo. The v1 native-Go-source-as-LDD path is gone; this file replaces
// the legacy LDD test suite.
package golang_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/waywardgeek/lyre/pkg/cdd"
	"github.com/waywardgeek/lyre/pkg/extract/golang"
)

// sampleSource is a simple Go package used as test input.
const sampleSource = `package shapes

// Circle is a round shape.
type Circle struct {
	Radius float64
	Color  string
}

// Area returns the area of the circle.
func (c *Circle) Area() float64 { return 0 }

// Perimeter returns the perimeter of the circle.
func (c *Circle) Perimeter() float64 { return 0 }

// Sizer can compute area and perimeter.
type Sizer interface {
	Area() float64
	Perimeter() float64
}

// Scale is a unit of measurement.
type Scale int

// NewCircle creates a circle with the given radius.
func NewCircle(radius float64, color string) *Circle { return nil }

// unexported should be ignored.
func unexported() {}
`

// writeTempSource writes the source into a temp dir and returns the dir path.
func writeTempSource(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(src), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	return dir
}

// --- ExtractGo --------------------------------------------------------------

func TestExtractGo_StructAndMethods(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	p, err := golang.ExtractGo(dir)
	if err != nil {
		t.Fatalf("ExtractGo: %v", err)
	}
	if p.Name != "shapes" {
		t.Errorf("package name: want shapes, got %q", p.Name)
	}
	if len(p.ModuleSource) != 1 || p.ModuleSource[0] != "shapes.go" {
		t.Errorf("ModuleSource: want [shapes.go], got %v", p.ModuleSource)
	}

	circle, ok := p.Structs["Circle"]
	if !ok {
		t.Fatal("Circle struct missing")
	}
	if circle.File != "shapes.go" || circle.Line == 0 {
		t.Errorf("Circle file/line: want shapes.go:N, got %s:%d", circle.File, circle.Line)
	}
	if got, _ := circle.FieldSig("Radius"); got != "float64" {
		t.Errorf("Circle.Radius signature: want float64, got %q", got)
	}
	if got, _ := circle.FieldSig("Color"); got != "string" {
		t.Errorf("Circle.Color signature: want string, got %q", got)
	}

	area, ok := circle.Methods["Area"]
	if !ok {
		t.Fatal("Circle.Area missing")
	}
	if area.SignatureText != "Area() float64" {
		t.Errorf("Area signature: want %q, got %q", "Area() float64", area.SignatureText)
	}
	if area.Source == "" || !strings.HasPrefix(area.Source, "shapes.go:") {
		t.Errorf("Area source: want shapes.go:N, got %q", area.Source)
	}
}

func TestExtractGo_Interface(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	p, err := golang.ExtractGo(dir)
	if err != nil {
		t.Fatalf("ExtractGo: %v", err)
	}
	sizer, ok := p.Interfaces["Sizer"]
	if !ok {
		t.Fatal("Sizer interface missing")
	}
	if sizer.Methods["Area"].SignatureText != "Area() float64" {
		t.Errorf("Sizer.Area signature: got %q", sizer.Methods["Area"].SignatureText)
	}
	if sizer.Methods["Perimeter"].SignatureText != "Perimeter() float64" {
		t.Errorf("Sizer.Perimeter signature: got %q", sizer.Methods["Perimeter"].SignatureText)
	}
}

func TestExtractGo_TypedefAndFunction(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	p, err := golang.ExtractGo(dir)
	if err != nil {
		t.Fatalf("ExtractGo: %v", err)
	}
	scale, ok := p.TypeDefs["Scale"]
	if !ok {
		t.Fatal("Scale typedef missing")
	}
	if scale.Underlying != "int" {
		t.Errorf("Scale underlying: want int, got %q", scale.Underlying)
	}

	nc, ok := p.Functions["NewCircle"]
	if !ok {
		t.Fatal("NewCircle function missing")
	}
	if nc.SignatureText != "NewCircle(radius float64, color string) *Circle" {
		t.Errorf("NewCircle signature: got %q", nc.SignatureText)
	}
}

func TestExtractGo_SkipsUnexported(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	p, err := golang.ExtractGo(dir)
	if err != nil {
		t.Fatalf("ExtractGo: %v", err)
	}
	if _, ok := p.Functions["unexported"]; ok {
		t.Error("unexported function should not appear in PackageInfo")
	}
}

// --- GenerateGo + round-trip through cdd.Parse -----------------------------

func TestGenerateGo_OutputFormat(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath, content, err := golang.GenerateGo(dir)
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}
	if !strings.HasSuffix(outPath, "shapes.go.lyric") {
		t.Errorf("output path: want suffix shapes.go.lyric, got %s", outPath)
	}
	for _, want := range []string{
		"module shapes",
		"source: [\"shapes.go\"]",
		"struct Circle",
		"field Radius: float64",
		"method Area() float64",
		"interface Sizer",
		"typedef Scale: int",
		"func NewCircle(radius float64, color string) *Circle",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated content missing %q\n--- content ---\n%s", want, content)
		}
	}
	// Must NOT contain anything referring to unexported names or legacy //ldd:.
	for _, bad := range []string{"unexported", "//ldd:", "//go:build", "// --- index ---"} {
		if strings.Contains(content, bad) {
			t.Errorf("generated content should not contain %q", bad)
		}
	}
}

func TestGenerateGo_RoundTripsThroughCDD(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	_, content, err := golang.GenerateGo(dir)
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}
	p, err := cdd.Parse(content, "shapes.go.lyric")
	if err != nil {
		t.Fatalf("cdd.Parse: %v\n--- content ---\n%s", err, content)
	}
	if p.Name != "shapes" {
		t.Errorf("parsed name: want shapes, got %q", p.Name)
	}
	if _, ok := p.Structs["Circle"]; !ok {
		t.Error("Circle missing after round-trip")
	}
	if _, ok := p.Interfaces["Sizer"]; !ok {
		t.Error("Sizer missing after round-trip")
	}
	if _, ok := p.Functions["NewCircle"]; !ok {
		t.Error("NewCircle missing after round-trip")
	}
	if _, ok := p.TypeDefs["Scale"]; !ok {
		t.Error("Scale missing after round-trip")
	}
}

// --- VerifyGo --------------------------------------------------------------

func generateAndWrite(t *testing.T, dir string) string {
	t.Helper()
	outPath, content, err := golang.GenerateGo(dir)
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing .go.lyric: %v", err)
	}
	return outPath
}

func TestVerifyGo_Clean(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath := generateAndWrite(t, dir)
	result, err := golang.VerifyGo(outPath)
	if err != nil {
		t.Fatalf("VerifyGo: %v", err)
	}
	if result.ErrorCount() > 0 {
		for _, f := range result.Findings {
			if f.Severity == golang.SevError {
				t.Errorf("unexpected error: %s", f.Message)
			}
		}
	}
}

func TestVerifyGo_MissingFunction(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	stripped := strings.ReplaceAll(sampleSource, "func NewCircle(radius float64, color string) *Circle { return nil }", "")
	if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(stripped), 0644); err != nil {
		t.Fatalf("writing stripped source: %v", err)
	}

	result, err := golang.VerifyGo(outPath)
	if err != nil {
		t.Fatalf("VerifyGo: %v", err)
	}
	found := false
	for _, f := range result.Findings {
		if strings.Contains(f.Message, "NewCircle") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected finding about NewCircle, got: %v", result.Findings)
	}
}

func TestVerifyGo_UndocumentedExport(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	extended := sampleSource + "\nfunc Describe(c *Circle) string { return \"\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	result, err := golang.VerifyGo(outPath)
	if err != nil {
		t.Fatalf("VerifyGo: %v", err)
	}
	found := false
	for _, f := range result.Findings {
		if strings.Contains(f.Message, "Describe") {
			found = true
		}
	}
	if !found {
		t.Error("expected finding about undocumented Describe")
	}
}

func TestVerifyGo_FieldTypeMismatch(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath, content, err := golang.GenerateGo(dir)
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}
	corrupted := strings.Replace(content, "field Radius: float64", "field Radius: int", 1)
	if err := os.WriteFile(outPath, []byte(corrupted), 0644); err != nil {
		t.Fatalf("writing corrupted .go.lyric: %v", err)
	}

	result, err := golang.VerifyGo(outPath)
	if err != nil {
		t.Fatalf("VerifyGo: %v", err)
	}
	found := false
	for _, f := range result.Findings {
		if f.Severity == golang.SevError && strings.Contains(f.Message, "Radius") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error about Radius type mismatch; got: %v", result.Findings)
	}
}

// --- UpdateGo --------------------------------------------------------------

func TestUpdateGo_AddsNewExport(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	extended := sampleSource + "\nfunc Describe(c *Circle) string { return \"\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	added, _, err := golang.UpdateGo(outPath)
	if err != nil {
		t.Fatalf("UpdateGo: %v", err)
	}
	foundInAdded := false
	for _, name := range added {
		if strings.Contains(name, "Describe") {
			foundInAdded = true
		}
	}
	if !foundInAdded {
		t.Errorf("expected Describe in added list, got: %v", added)
	}

	updated, _ := os.ReadFile(outPath)
	if !strings.Contains(string(updated), "func Describe(c *Circle) string") {
		t.Errorf("updated .go.lyric should contain Describe declaration; got:\n%s", updated)
	}
}

func TestUpdateGo_AlreadyUpToDate(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	added, _, err := golang.UpdateGo(outPath)
	if err != nil {
		t.Fatalf("UpdateGo: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("expected no additions, got: %v", added)
	}
}

// TestUpdateGo_RefreshesWhyFromSource verifies the source-of-truth policy:
// module-level prose (human/design-curated) is preserved, but per-decl why: is
// refreshed from the Go doc comment (source wins), while a field with no source
// comment keeps its hand-written doc.
func TestUpdateGo_RefreshesWhyFromSource(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	// Splice in module-level why + a doc block + a per-decl why on Circle.
	raw, _ := os.ReadFile(outPath)
	annotated := strings.Replace(string(raw),
		"module shapes\n",
		"module shapes\n  why: \"geometry primitives\"\n", 1)
	// Inject a (stale) hand-written per-decl why on Circle. Circle HAS a source
	// doc comment ("Circle is a round shape."), so update must overwrite this.
	annotated = strings.Replace(annotated,
		"struct Circle\n    source:",
		"struct Circle\n    why: \"a flat round geometric primitive\"\n    source:", 1)
	// Add per-field doc on Radius. Radius has NO source comment, so it must be
	// preserved.
	annotated = strings.Replace(annotated,
		"field Radius: float64\n",
		"field Radius: float64\n      doc: \"in scene units\"\n", 1)
	if err := os.WriteFile(outPath, []byte(annotated), 0644); err != nil {
		t.Fatalf("writing annotated .go.lyric: %v", err)
	}

	// Trigger an update by adding a new symbol to the source.
	extended := sampleSource + "\nfunc Clone(c *Circle) *Circle { return nil }\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}
	if _, _, err := golang.UpdateGo(outPath); err != nil {
		t.Fatalf("UpdateGo: %v", err)
	}

	updated, _ := os.ReadFile(outPath)
	updatedStr := string(updated)
	for _, want := range []string{
		`why: "geometry primitives"`,      // module why: human-curated, preserved
		`why: "Circle is a round shape."`, // per-decl why: refreshed from source
		`doc: "in scene units"`,           // field doc: no source comment, preserved
		`func Clone(c *Circle) *Circle`,   // new symbol added
	} {
		if !strings.Contains(updatedStr, want) {
			t.Errorf("update lost or failed to add %q\n--- file ---\n%s", want, updatedStr)
		}
	}
	// The stale hand-written Circle why must be overwritten by the source comment.
	if strings.Contains(updatedStr, "a flat round geometric primitive") {
		t.Errorf("stale hand-written Circle why should be overwritten by source comment\n--- file ---\n%s", updatedStr)
	}
}

func TestUpdateGo_RefreshesPositionsAndSource(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	// Re-write the source with NewCircle pushed further down.
	shifted := "// pad\n// pad\n// pad\n// pad\n// pad\n" + sampleSource
	if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(shifted), 0644); err != nil {
		t.Fatalf("writing shifted source: %v", err)
	}
	if _, _, err := golang.UpdateGo(outPath); err != nil {
		t.Fatalf("UpdateGo: %v", err)
	}

	// Parse the resulting file and confirm NewCircle now points deeper.
	raw, _ := os.ReadFile(outPath)
	p, err := cdd.Parse(string(raw), outPath)
	if err != nil {
		t.Fatalf("cdd.Parse: %v", err)
	}
	nc, ok := p.Functions["NewCircle"]
	if !ok {
		t.Fatal("NewCircle missing after update")
	}
	if nc.Line <= 5 {
		t.Errorf("NewCircle line should have shifted past the 5-line pad; got line %d", nc.Line)
	}
}

// TestUpdateGo_PrunesRemovedExport proves prune-by-default end to end: a decl
// removed from source is dropped from the .lyric, reported in `removed`, and
// leaves verify clean (no orphan drift).
func TestUpdateGo_PrunesRemovedExport(t *testing.T) {
	dir := writeTempSource(t, sampleSource+"\nfunc Describe(c *Circle) string { return \"\" }\n")
	outPath := generateAndWrite(t, dir)

	before, _ := os.ReadFile(outPath)
	if !strings.Contains(string(before), "Describe") {
		t.Fatalf("precondition: generated .lyric should contain Describe;\n%s", before)
	}

	// Remove Describe from source.
	if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(sampleSource), 0644); err != nil {
		t.Fatalf("writing reduced source: %v", err)
	}

	_, removed, err := golang.UpdateGo(outPath)
	if err != nil {
		t.Fatalf("UpdateGo: %v", err)
	}
	found := false
	for _, name := range removed {
		if strings.Contains(name, "Describe") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Describe in removed list, got: %v", removed)
	}

	updated, _ := os.ReadFile(outPath)
	if strings.Contains(string(updated), "Describe") {
		t.Errorf("pruned .go.lyric should not mention Describe; got:\n%s", updated)
	}

	res, err := golang.VerifyGo(outPath)
	if err != nil {
		t.Fatalf("VerifyGo: %v", err)
	}
	if res.ErrorCount() != 0 {
		t.Errorf("expected clean verify after prune, got %d errors", res.ErrorCount())
	}
}

// TestDiscoverTestFuncs verifies whole-module test discovery for W007:
// Test/Benchmark/Fuzz/Example funcs are collected across packages; non-test
// funcs, method receivers, and vendored/skipped trees are excluded; and a
// non-compiling _test.go is skipped rather than aborting discovery.
func TestDiscoverTestFuncs(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write("go.mod", "module example.com/w007\n\ngo 1.21\n")
	// Package a: a real test, a benchmark, a helper (not a test), and a
	// method named like a test (must be excluded — has a receiver).
	write("a/a_test.go", `package a
import "testing"
func TestAlpha(t *testing.T) {}
func BenchmarkAlpha(b *testing.B) {}
func helper() {}
type S struct{}
func (S) TestMethodShaped() {}
`)
	// Package b in a different directory: proves whole-module (not per-dir)
	// scope, plus Fuzz/Example prefixes.
	write("b/nested/b_test.go", `package nested
import "testing"
func FuzzBeta(f *testing.F) {}
func ExampleBeta() {}
`)
	// A non-test .go file must contribute nothing.
	write("a/a.go", `package a
func TestLooksLikeATestButNotInTestFile() {}
`)
	// A non-compiling _test.go must be skipped, not fatal.
	write("c/broken_test.go", "package c\nthis is not valid go\n")
	// vendor/ must be skipped entirely.
	write("vendor/x/x_test.go", `package x
import "testing"
func TestVendored(t *testing.T) {}
`)

	// Start discovery from a nested package dir; it must walk up to go.mod.
	got, err := golang.DiscoverTestFuncs(filepath.Join(root, "b", "nested"))
	if err != nil {
		t.Fatalf("DiscoverTestFuncs: %v", err)
	}

	want := map[string]bool{
		"TestAlpha":      true,
		"BenchmarkAlpha": true,
		"FuzzBeta":       true,
		"ExampleBeta":    true,
	}
	for name := range want {
		if !got[name] {
			t.Errorf("expected test %q to be discovered; got %v", name, got)
		}
	}
	for _, bad := range []string{
		"helper",                             // non-test func
		"TestMethodShaped",                   // method receiver, not top-level
		"TestLooksLikeATestButNotInTestFile", // in a non-_test.go file
		"TestVendored",                       // under vendor/
	} {
		if got[bad] {
			t.Errorf("did not expect %q to be discovered", bad)
		}
	}
}

// TestDiscoverTestFuncs_NoGoMod falls back to scanning startDir when no
// enclosing go.mod exists, still finding tests there.
func TestDiscoverTestFuncs_NoGoMod(t *testing.T) {
	root := t.TempDir() // no go.mod anywhere up to the OS temp root's module, but
	// t.TempDir is under /tmp which has no go.mod, so moduleRoot falls back to root.
	if err := os.WriteFile(filepath.Join(root, "z_test.go"), []byte(
		"package z\nimport \"testing\"\nfunc TestZeta(t *testing.T) {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := golang.DiscoverTestFuncs(root)
	if err != nil {
		t.Fatalf("DiscoverTestFuncs: %v", err)
	}
	if !got["TestZeta"] {
		t.Errorf("expected TestZeta to be discovered; got %v", got)
	}
}

// TestVerifyGo_OrphanDetection exercises VerifyGo's "declared in .lyric but
// not found in source" branches — the safety net that flags documentation
// left behind when a declaration is removed from source. TestVerifyGo_Missing-
// Function already covers the top-level-function branch; this covers the
// struct, interface, typedef, struct-field, struct-method, and interface-
// method branches. For each case we generate a .lyric from `full`, then
// rewrite the source to `reduced` and assert VerifyGo reports the orphan.
func TestVerifyGo_OrphanDetection(t *testing.T) {
	cases := []struct {
		name    string
		full    string
		reduced string
		want    string
	}{
		{
			name:    "struct",
			full:    "package shapes\n\n// Widget does things.\ntype Widget struct {\n\tN int\n}\n",
			reduced: "package shapes\n",
			want:    "struct Widget declared in .lyric but not found in source",
		},
		{
			name:    "interface",
			full:    "package shapes\n\n// Reader reads.\ntype Reader interface {\n\tRead() int\n}\n",
			reduced: "package shapes\n",
			want:    "interface Reader declared in .lyric but not found in source",
		},
		{
			name:    "typedef",
			full:    "package shapes\n\n// Meters is a distance.\ntype Meters int\n",
			reduced: "package shapes\n",
			want:    "typedef Meters declared in .lyric but not found in source",
		},
		{
			name:    "struct field",
			full:    "package shapes\n\n// Box holds stuff.\ntype Box struct {\n\tW int\n\tH int\n}\n",
			reduced: "package shapes\n\n// Box holds stuff.\ntype Box struct {\n\tW int\n}\n",
			want:    "struct Box: field H not found in source",
		},
		{
			name:    "struct method",
			full:    "package shapes\n\n// Gadget spins.\ntype Gadget struct{}\n\n// Spin spins it.\nfunc (Gadget) Spin() {}\n",
			reduced: "package shapes\n\n// Gadget spins.\ntype Gadget struct{}\n",
			want:    "struct Gadget: method Spin not found in source",
		},
		{
			name:    "interface method",
			full:    "package shapes\n\n// Doer does and undoes.\ntype Doer interface {\n\tDo()\n\tUndo()\n}\n",
			reduced: "package shapes\n\n// Doer does and undoes.\ntype Doer interface {\n\tDo()\n}\n",
			want:    "interface Doer: method Undo not found in source",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTempSource(t, tc.full)
			outPath := generateAndWrite(t, dir)
			// Replace source with the reduced variant so the .lyric now
			// documents a declaration source no longer has.
			if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(tc.reduced), 0644); err != nil {
				t.Fatalf("writing reduced source: %v", err)
			}
			result, err := golang.VerifyGo(outPath)
			if err != nil {
				t.Fatalf("VerifyGo: %v", err)
			}
			found := false
			for _, f := range result.Findings {
				if f.Severity == golang.SevError && strings.Contains(f.Message, tc.want) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected SevError containing %q; got %v", tc.want, result.Findings)
			}
		})
	}
}
