// Tests for the v2 .ly.lyric pipeline: ExtractLy, GenerateLy, UpdateLy,
// VerifyLy. The v1 native-Lyric-source-as-LDD path is gone; this file
// replaces the legacy LDD test surface (there were no tests previously —
// this is green-field, mirroring Phase 3b's TypeScript test layout).
//
// The Lyric extractor shells out to the pre-compiled extract_api binary
// shipped from ~/projects/lyric/tools/. If the binary is unavailable, all
// tests are skipped — the suite is a no-op rather than a failure (lets
// CI runs without a Lyric build pass).
package lyric_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/waywardgeek/lyre/pkg/extract"
	"github.com/waywardgeek/lyre/pkg/extract/lyric"
	"github.com/waywardgeek/lyre/pkg/cdd"
)

// sampleSource is a small Lyric module exercising:
//   - typedef-as-enum
//   - value-type struct (Point)
//   - reference-type class with methods (permanent class Circle)
//   - interface with one method (Drawable)
//   - top-level function (new_circle)
//
// The shapes/Point/Circle/Drawable shape mirrors Phase 3b's TS sample so
// the test structure is recognizably parallel.
const sampleSource = `lyric shapes {

  enum Color {
    Red Green Blue
  }

  struct Point {
    x: f64
    y: f64
  }

  permanent class Circle {
    center: Point
    radius: f64

    func area(self) -> f64 {
      return 3.14 * self.radius * self.radius
    }

    func scale(mut self, k: f64) {
      self.radius = self.radius * k
    }
  }

  interface Drawable {
    func draw(self) -> string
  }

  func new_circle(x: f64, y: f64, r: f64) -> Circle {
    return Circle{center: Point{x: x, y: y}, radius: r}
  }
}
`

// extraFunc is a one-line top-level function used to test
// "undocumented export" / "update adds" / "verify drift" paths.
const extraFunc = `
  func describe(c: Circle) -> string {
    return "circle"
  }
`

// requireExtractor skips the test if the extract_api binary isn't
// available — keeps the suite green on machines without Lyric built.
func requireExtractor(t *testing.T) {
	t.Helper()
	// A tiny extraction probe: write an empty-module file and attempt
	// to extract; if it errors with "cannot find extract_api", skip.
	dir := filepath.Join(t.TempDir(), "probe")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.ly"),
		[]byte("lyric probe {\n  func touch() {}\n}\n"), 0644); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	if _, err := lyric.ExtractLy(dir); err != nil {
		msg := err.Error()
		// Skip cleanly if the binary can't be located OR if the
		// discovered binary can't execute on this host (e.g. the
		// checked-in Mac arm64 binary on a Linux x86_64 CI runner).
		// Both conditions mean the host can't run Lyric tooling, not
		// that our Go code is wrong.
		for _, sig := range []string{
			"cannot find extract_api",
			"Exec format error",
			"exec format error",
			"cannot execute binary file",
		} {
			if strings.Contains(msg, sig) {
				t.Skipf("extract_api unusable on this host (%s); skipping. Run `make tools` in ~/projects/lyric to build for this arch.", sig)
			}
		}
		t.Fatalf("probe ExtractLy: %v", err)
	}
}

// writeTempLy writes src to <tmp>/shapes/shapes.ly and returns the dir.
// Stable subdir name keeps the .ly.lyric module identifier valid.
func writeTempLy(t *testing.T, src string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "shapes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shapes.ly"), []byte(src), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	return dir
}

// --- ExtractLy ------------------------------------------------------------

func TestExtractLy_ClassAndMethods(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	p, err := lyric.ExtractLy(dir)
	if err != nil {
		t.Fatalf("ExtractLy: %v", err)
	}
	if p.Name != filepath.Base(dir) {
		t.Errorf("package name: want %q, got %q", filepath.Base(dir), p.Name)
	}
	if len(p.ModuleSource) != 1 || p.ModuleSource[0] != "shapes.ly" {
		t.Errorf("ModuleSource: want [shapes.ly], got %v", p.ModuleSource)
	}

	circle, ok := p.Structs["Circle"]
	if !ok {
		t.Fatal("Circle class missing")
	}
	if !circle.IsClass {
		t.Error("Circle.IsClass should be true (permanent class)")
	}
	if circle.File != "shapes.ly" || circle.Line == 0 {
		t.Errorf("Circle file/line: want shapes.ly:N, got %s:%d", circle.File, circle.Line)
	}
	if got, _ := circle.FieldSig("radius"); got != "f64" {
		t.Errorf("Circle.radius: want f64, got %q", got)
	}

	area, ok := circle.Methods["area"]
	if !ok {
		t.Fatal("Circle.area missing")
	}
	if area.SignatureText != "area(self) -> f64" {
		t.Errorf("area signature: want %q, got %q", "area(self) -> f64", area.SignatureText)
	}
	if !strings.HasPrefix(area.Source, "shapes.ly:") {
		t.Errorf("area source: want shapes.ly:N, got %q", area.Source)
	}

	scale, ok := circle.Methods["scale"]
	if !ok {
		t.Fatal("Circle.scale missing")
	}
	// scale returns void; signature has no `-> R` clause.
	if scale.SignatureText != "scale(self, k: f64)" {
		t.Errorf("scale signature: want %q, got %q", "scale(self, k: f64)", scale.SignatureText)
	}
}

func TestExtractLy_StructValueType(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	p, err := lyric.ExtractLy(dir)
	if err != nil {
		t.Fatalf("ExtractLy: %v", err)
	}
	point, ok := p.Structs["Point"]
	if !ok {
		t.Fatal("Point struct missing")
	}
	if point.IsClass {
		t.Error("Point.IsClass should be false (struct, not class)")
	}
	if got, _ := point.FieldSig("x"); got != "f64" {
		t.Errorf("Point.x: want f64, got %q", got)
	}
}

func TestExtractLy_Interface(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	p, err := lyric.ExtractLy(dir)
	if err != nil {
		t.Fatalf("ExtractLy: %v", err)
	}
	drawable, ok := p.Interfaces["Drawable"]
	if !ok {
		t.Fatal("Drawable interface missing")
	}
	draw, ok := drawable.Methods["draw"]
	if !ok {
		t.Fatal("Drawable.draw missing")
	}
	if draw.SignatureText != "draw(self) -> string" {
		t.Errorf("Drawable.draw signature: want %q, got %q", "draw(self) -> string", draw.SignatureText)
	}
}

func TestExtractLy_TypedefAndFunction(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	p, err := lyric.ExtractLy(dir)
	if err != nil {
		t.Fatalf("ExtractLy: %v", err)
	}
	color, ok := p.TypeDefs["Color"]
	if !ok {
		t.Fatal("Color typedef missing")
	}
	// enum extractor underlying is "enum { Red, Green, Blue }".
	if !strings.HasPrefix(color.Underlying, "enum {") {
		t.Errorf("Color underlying: want prefix 'enum {', got %q", color.Underlying)
	}
	for _, v := range []string{"Red", "Green", "Blue"} {
		if !strings.Contains(color.Underlying, v) {
			t.Errorf("Color underlying missing variant %q: %q", v, color.Underlying)
		}
	}

	nc, ok := p.Functions["new_circle"]
	if !ok {
		t.Fatal("new_circle function missing")
	}
	if nc.SignatureText != "new_circle(x: f64, y: f64, r: f64) -> Circle" {
		t.Errorf("new_circle signature: got %q", nc.SignatureText)
	}
}

func TestExtractLy_SkipsTestFiles(t *testing.T) {
	requireExtractor(t)
	dir := filepath.Join(t.TempDir(), "shapes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "shapes.ly"), []byte(sampleSource), 0644)
	os.WriteFile(filepath.Join(dir, "shapes_test.ly"),
		[]byte("lyric shapes_test {\n  func test_helper() -> i32 { return 0 }\n}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "test_more.ly"),
		[]byte("lyric test_more {\n  func also_test() -> i32 { return 0 }\n}\n"), 0644)

	p, err := lyric.ExtractLy(dir)
	if err != nil {
		t.Fatalf("ExtractLy: %v", err)
	}
	if _, ok := p.Functions["test_helper"]; ok {
		t.Error("test_helper from *_test.ly should be skipped")
	}
	if _, ok := p.Functions["also_test"]; ok {
		t.Error("also_test from test_*.ly should be skipped")
	}
	if len(p.ModuleSource) != 1 || p.ModuleSource[0] != "shapes.ly" {
		t.Errorf("ModuleSource should only include shapes.ly; got %v", p.ModuleSource)
	}
}

// --- GenerateLy + round-trip through cdd.Parse -----------------------------

func TestGenerateLy_OutputFormat(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	outPath, content, err := lyric.GenerateLy(dir)
	if err != nil {
		t.Fatalf("GenerateLy: %v", err)
	}
	if !strings.HasSuffix(outPath, ".ly.lyric") {
		t.Errorf("output path: want suffix .ly.lyric, got %s", outPath)
	}
	pkgName := filepath.Base(dir)
	for _, want := range []string{
		"module " + pkgName,
		`source: ["shapes.ly"]`,
		"class Circle",
		"struct Point",
		"field radius: f64",
		"method area(self) -> f64",
		"interface Drawable",
		"typedef Color:",
		"func new_circle(x: f64, y: f64, r: f64) -> Circle",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated content missing %q\n--- content ---\n%s", want, content)
		}
	}
	for _, bad := range []string{"//ldd:", "// --- index ---"} {
		if strings.Contains(content, bad) {
			t.Errorf("generated content should not contain %q", bad)
		}
	}
}

func TestGenerateLy_RoundTripsThroughCDD(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	_, content, err := lyric.GenerateLy(dir)
	if err != nil {
		t.Fatalf("GenerateLy: %v", err)
	}
	p, err := cdd.Parse(content, "shapes.ly.lyric")
	if err != nil {
		t.Fatalf("cdd.Parse: %v\n--- content ---\n%s", err, content)
	}
	// Both class and struct should round-trip with correct IsClass flag.
	circle, ok := p.Structs["Circle"]
	if !ok {
		t.Fatal("Circle missing after round-trip")
	}
	if !circle.IsClass {
		t.Error("Circle.IsClass lost on round-trip (should be class, got struct)")
	}
	point, ok := p.Structs["Point"]
	if !ok {
		t.Fatal("Point missing after round-trip")
	}
	if point.IsClass {
		t.Error("Point.IsClass wrong on round-trip (should be struct, got class)")
	}
	if _, ok := p.Interfaces["Drawable"]; !ok {
		t.Error("Drawable missing after round-trip")
	}
	if _, ok := p.Functions["new_circle"]; !ok {
		t.Error("new_circle missing after round-trip")
	}
	if _, ok := p.TypeDefs["Color"]; !ok {
		t.Error("Color missing after round-trip")
	}
}

// --- VerifyLy --------------------------------------------------------------

func generateAndWrite(t *testing.T, dir string) string {
	t.Helper()
	outPath, content, err := lyric.GenerateLy(dir)
	if err != nil {
		t.Fatalf("GenerateLy: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing .ly.lyric: %v", err)
	}
	return outPath
}

func TestVerifyLy_Clean(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	outPath := generateAndWrite(t, dir)
	result, err := lyric.VerifyLy(outPath)
	if err != nil {
		t.Fatalf("VerifyLy: %v", err)
	}
	if result.ErrorCount() > 0 {
		for _, f := range result.Findings {
			if f.Severity == lyric.SevError {
				t.Errorf("unexpected error: %s", f.Message)
			}
		}
	}
}

func TestVerifyLy_MissingFunction(t *testing.T) {
	requireExtractor(t)
	// Start from a source that declares describe; generate the .lyric;
	// then remove describe from source — verify reports drift.
	dir := writeTempLy(t, sampleSource+extraFunc)
	outPath := generateAndWrite(t, dir)
	// Remove the extra function from source.
	if err := os.WriteFile(filepath.Join(dir, "shapes.ly"), []byte(sampleSource), 0644); err != nil {
		t.Fatalf("writing stripped source: %v", err)
	}
	result, err := lyric.VerifyLy(outPath)
	if err != nil {
		t.Fatalf("VerifyLy: %v", err)
	}
	found := false
	for _, f := range result.Findings {
		if strings.Contains(f.Message, "describe") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected finding about describe; got: %v", result.Findings)
	}
}

func TestVerifyLy_UndocumentedExport(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "shapes.ly"),
		[]byte(strings.TrimSuffix(sampleSource, "}\n")+extraFunc+"}\n"), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	result, err := lyric.VerifyLy(outPath)
	if err != nil {
		t.Fatalf("VerifyLy: %v", err)
	}
	found := false
	for _, f := range result.Findings {
		if strings.Contains(f.Message, "describe") {
			found = true
		}
	}
	if !found {
		t.Error("expected finding about undocumented describe")
	}
}

func TestVerifyLy_FieldTypeMismatch(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	outPath, content, err := lyric.GenerateLy(dir)
	if err != nil {
		t.Fatalf("GenerateLy: %v", err)
	}
	corrupted := strings.Replace(content, "field radius: f64", "field radius: string", 1)
	if err := os.WriteFile(outPath, []byte(corrupted), 0644); err != nil {
		t.Fatalf("writing corrupted .ly.lyric: %v", err)
	}
	result, err := lyric.VerifyLy(outPath)
	if err != nil {
		t.Fatalf("VerifyLy: %v", err)
	}
	found := false
	for _, f := range result.Findings {
		if f.Severity == lyric.SevError && strings.Contains(f.Message, "radius") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error about radius type mismatch; got: %v", result.Findings)
	}
}

// --- UpdateLy --------------------------------------------------------------

func TestUpdateLy_AddsNewExport(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "shapes.ly"),
		[]byte(strings.TrimSuffix(sampleSource, "}\n")+extraFunc+"}\n"), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	added, err := lyric.UpdateLy(outPath)
	if err != nil {
		t.Fatalf("UpdateLy: %v", err)
	}
	foundInAdded := false
	for _, name := range added {
		if strings.Contains(name, "describe") {
			foundInAdded = true
		}
	}
	if !foundInAdded {
		t.Errorf("expected describe in added list, got: %v", added)
	}

	updated, _ := os.ReadFile(outPath)
	if !strings.Contains(string(updated), "func describe(c: Circle) -> string") {
		t.Errorf("updated .ly.lyric should contain describe declaration; got:\n%s", updated)
	}
}

func TestUpdateLy_AlreadyUpToDate(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	outPath := generateAndWrite(t, dir)
	added, err := lyric.UpdateLy(outPath)
	if err != nil {
		t.Fatalf("UpdateLy: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("expected no additions, got: %v", added)
	}
}

// TestUpdateLy_PreservesHumanProse uses the cleaner approach: construct a
// PackageInfo with prose set, write it via cdd.Write to seed the fixture,
// then run UpdateLy. No fragile string-splicing.
func TestUpdateLy_PreservesHumanProse(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	outPath := filepath.Join(dir, filepath.Base(dir)+".ly.lyric")

	p, err := lyric.ExtractLy(dir)
	if err != nil {
		t.Fatalf("ExtractLy: %v", err)
	}
	p.ModuleWhy = "geometry primitives"
	if c, ok := p.Structs["Circle"]; ok {
		c.Why = "a flat round geometric primitive"
		for i, f := range c.Fields {
			if f.Name == "radius" {
				c.Fields[i].Doc = "in scene units"
			}
		}
	}
	if err := os.WriteFile(outPath, []byte(cdd.Write(p)), 0644); err != nil {
		t.Fatalf("writing seed .ly.lyric: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "shapes.ly"),
		[]byte(strings.TrimSuffix(sampleSource, "}\n")+extraFunc+"}\n"), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}
	if _, err := lyric.UpdateLy(outPath); err != nil {
		t.Fatalf("UpdateLy: %v", err)
	}

	updated, _ := os.ReadFile(outPath)
	updatedStr := string(updated)
	for _, want := range []string{
		`why: "geometry primitives"`,
		`why: "a flat round geometric primitive"`,
		`doc: "in scene units"`,
		`func describe(c: Circle) -> string`,
	} {
		if !strings.Contains(updatedStr, want) {
			t.Errorf("update lost or failed to add %q\n--- file ---\n%s", want, updatedStr)
		}
	}
}

func TestUpdateLy_RefreshesPositionsAndSource(t *testing.T) {
	requireExtractor(t)
	dir := writeTempLy(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	// Pad with 5 lines of blank/comment so new_circle shifts down.
	shifted := "// pad\n// pad\n// pad\n// pad\n// pad\n" + sampleSource
	if err := os.WriteFile(filepath.Join(dir, "shapes.ly"), []byte(shifted), 0644); err != nil {
		t.Fatalf("writing shifted source: %v", err)
	}
	if _, err := lyric.UpdateLy(outPath); err != nil {
		t.Fatalf("UpdateLy: %v", err)
	}

	raw, _ := os.ReadFile(outPath)
	p, err := cdd.Parse(string(raw), outPath)
	if err != nil {
		t.Fatalf("cdd.Parse: %v", err)
	}
	nc, ok := p.Functions["new_circle"]
	if !ok {
		t.Fatal("new_circle missing after update")
	}
	if nc.Line <= 5 {
		t.Errorf("new_circle line should have shifted past the 5-line pad; got line %d", nc.Line)
	}
}

// TestUpdateLy_MultipleFiles ensures multi-file extraction merges cleanly.
func TestUpdateLy_MultipleFiles(t *testing.T) {
	requireExtractor(t)
	dir := filepath.Join(t.TempDir(), "shapes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "shapes.ly"), []byte(sampleSource), 0644)
	os.WriteFile(filepath.Join(dir, "utils.ly"),
		[]byte("lyric utils {\n  func clamp(n: f64, lo: f64, hi: f64) -> f64 {\n    return n\n  }\n}\n"), 0644)

	_, content, err := lyric.GenerateLy(dir)
	if err != nil {
		t.Fatalf("GenerateLy: %v", err)
	}
	if !strings.Contains(content, "func clamp") {
		t.Error("expected clamp from utils.ly in output")
	}
	if !strings.Contains(content, "class Circle") {
		t.Error("expected Circle from shapes.ly in output")
	}
	if !strings.Contains(content, `source: ["shapes.ly","utils.ly"]`) {
		t.Errorf("expected both source files in module source list; got:\n%s", content)
	}
}

// Compile-time assertion that ExtractLy returns the expected type.
var _ func(string) (*extract.PackageInfo, error) = lyric.ExtractLy
