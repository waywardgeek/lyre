// Tests for the v2 .py.lyric pipeline: ExtractPy, GeneratePy, UpdatePy,
// VerifyPy. The v1 native-Python-source-as-LDD path is gone; this file is
// a green-field rewrite mirroring Phase 3c's Lyric test layout.
//
// The Python extractor shells out to python3 + a temp-extracted copy of
// extract_api.py. If python3 isn't on PATH or the script errors fatally,
// all tests are skipped — the suite is a no-op rather than a failure
// (lets CI runs without a Python toolchain pass).
package python_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/waywardgeek/lyre/pkg/cdd"
	"github.com/waywardgeek/lyre/pkg/extract"
	"github.com/waywardgeek/lyre/pkg/extract/python"
)

// sampleSource is a small Python module exercising:
//   - reference-type class with methods (Circle)
//   - dataclass-style struct (Point) — still IsClass=true in Python
//   - Protocol-as-interface (Drawable)
//   - TypeAlias (Color)
//   - top-level function (new_circle)
//   - private function `_helper` skipped via is_public()
//
// The shapes/Point/Circle/Drawable shape mirrors Phase 3b/3c so the test
// structure is recognizably parallel.
const sampleSource = `"""shapes — geometry primitives."""

from typing import Protocol, TypeAlias


Color: TypeAlias = str


class Point:
    x: float
    y: float


class Circle:
    """A 2D circle."""
    center: Point
    radius: float

    def area(self) -> float:
        return 3.14 * self.radius * self.radius

    def scale(self, k: float):
        self.radius = self.radius * k


class Drawable(Protocol):
    def draw(self) -> str: ...


def new_circle(x: float, y: float, r: float) -> Circle:
    return Circle()


def _helper() -> int:
    return 0
`

// extraFunc is a single top-level function used to test add/drift paths.
const extraFunc = `


def describe(c: Circle) -> str:
    return "circle"
`

// requireExtractor skips if python3 isn't available or the extractor
// errors fatally on a probe module.
func requireExtractor(t *testing.T) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "probe")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.py"),
		[]byte("def touch() -> int:\n    return 0\n"), 0644); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	if _, err := python.ExtractPy(dir); err != nil {
		msg := err.Error()
		for _, sig := range []string{
			"executable file not found",
			"no such file or directory",
			"running python3",
		} {
			if strings.Contains(msg, sig) {
				t.Skipf("python3 unusable on this host (%s); skipping.", sig)
			}
		}
		t.Fatalf("probe ExtractPy: %v", err)
	}
}

// writeTempPy writes src to <tmp>/shapes/shapes.py and returns the dir.
// Stable subdir name keeps the .py.lyric module identifier valid.
func writeTempPy(t *testing.T, src string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "shapes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shapes.py"), []byte(src), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	return dir
}

// --- ExtractPy ------------------------------------------------------------

func TestExtractPy_ClassAndMethods(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	p, err := python.ExtractPy(dir)
	if err != nil {
		t.Fatalf("ExtractPy: %v", err)
	}
	if p.Name != filepath.Base(dir) {
		t.Errorf("package name: want %q, got %q", filepath.Base(dir), p.Name)
	}
	if len(p.ModuleSource) != 1 || p.ModuleSource[0] != "shapes.py" {
		t.Errorf("ModuleSource: want [shapes.py], got %v", p.ModuleSource)
	}

	circle, ok := p.Structs["Circle"]
	if !ok {
		t.Fatal("Circle class missing")
	}
	if !circle.IsClass {
		t.Error("Circle.IsClass should be true (all Python classes)")
	}
	if circle.File != "shapes.py" || circle.Line == 0 {
		t.Errorf("Circle file/line: want shapes.py:N, got %s:%d", circle.File, circle.Line)
	}
	if got, _ := circle.FieldSig("radius"); got != "float" {
		t.Errorf("Circle.radius: want float, got %q", got)
	}

	area, ok := circle.Methods["area"]
	if !ok {
		t.Fatal("Circle.area missing")
	}
	if area.SignatureText != "area(self) -> float" {
		t.Errorf("area signature: want %q, got %q", "area(self) -> float", area.SignatureText)
	}
	if !strings.HasPrefix(area.Source, "shapes.py:") {
		t.Errorf("area source: want shapes.py:N, got %q", area.Source)
	}

	scale, ok := circle.Methods["scale"]
	if !ok {
		t.Fatal("Circle.scale missing")
	}
	// scale returns None; signature has no `-> R` clause.
	if scale.SignatureText != "scale(self, k: float)" {
		t.Errorf("scale signature: want %q, got %q", "scale(self, k: float)", scale.SignatureText)
	}
}

func TestExtractPy_PlainStructClass(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	p, err := python.ExtractPy(dir)
	if err != nil {
		t.Fatalf("ExtractPy: %v", err)
	}
	point, ok := p.Structs["Point"]
	if !ok {
		t.Fatal("Point class missing")
	}
	if !point.IsClass {
		t.Error("Point.IsClass should be true (Python has no value-vs-reference distinction)")
	}
	if got, _ := point.FieldSig("x"); got != "float" {
		t.Errorf("Point.x: want float, got %q", got)
	}
	if got, _ := point.FieldSig("y"); got != "float" {
		t.Errorf("Point.y: want float, got %q", got)
	}
}

func TestExtractPy_Interface(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	p, err := python.ExtractPy(dir)
	if err != nil {
		t.Fatalf("ExtractPy: %v", err)
	}
	drawable, ok := p.Interfaces["Drawable"]
	if !ok {
		t.Fatalf("Drawable interface missing; structs=%v interfaces=%v",
			structNames(p), ifaceNames(p))
	}
	draw, ok := drawable.Methods["draw"]
	if !ok {
		t.Fatal("Drawable.draw missing")
	}
	if draw.SignatureText != "draw(self) -> str" {
		t.Errorf("Drawable.draw signature: want %q, got %q", "draw(self) -> str", draw.SignatureText)
	}
}

func TestExtractPy_TypedefAndFunction(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	p, err := python.ExtractPy(dir)
	if err != nil {
		t.Fatalf("ExtractPy: %v", err)
	}
	color, ok := p.TypeDefs["Color"]
	if !ok {
		t.Fatal("Color typedef missing")
	}
	if color.Underlying != "str" {
		t.Errorf("Color underlying: want str, got %q", color.Underlying)
	}

	nc, ok := p.Functions["new_circle"]
	if !ok {
		t.Fatal("new_circle function missing")
	}
	if nc.SignatureText != "new_circle(x: float, y: float, r: float) -> Circle" {
		t.Errorf("new_circle signature: got %q", nc.SignatureText)
	}

	// Private function _helper should be excluded by is_public() in extractor.
	if _, ok := p.Functions["_helper"]; ok {
		t.Error("_helper (underscore-prefixed) should be excluded")
	}
}

func TestExtractPy_SkipsTestAndUnderscoreFiles(t *testing.T) {
	requireExtractor(t)
	dir := filepath.Join(t.TempDir(), "shapes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "shapes.py"), []byte(sampleSource), 0644)
	os.WriteFile(filepath.Join(dir, "shapes_test.py"),
		[]byte("def test_helper() -> int:\n    return 0\n"), 0644)
	os.WriteFile(filepath.Join(dir, "test_more.py"),
		[]byte("def also_test() -> int:\n    return 0\n"), 0644)
	os.WriteFile(filepath.Join(dir, "__init__.py"),
		[]byte("def init_only() -> int:\n    return 0\n"), 0644)
	os.WriteFile(filepath.Join(dir, "_internal.py"),
		[]byte("def internal_only() -> int:\n    return 0\n"), 0644)

	p, err := python.ExtractPy(dir)
	if err != nil {
		t.Fatalf("ExtractPy: %v", err)
	}
	for _, n := range []string{"test_helper", "also_test", "init_only", "internal_only"} {
		if _, ok := p.Functions[n]; ok {
			t.Errorf("%s should be skipped (test/underscore file)", n)
		}
	}
	if len(p.ModuleSource) != 1 || p.ModuleSource[0] != "shapes.py" {
		t.Errorf("ModuleSource should only include shapes.py; got %v", p.ModuleSource)
	}
}

// --- GeneratePy + round-trip through cdd.Parse ----------------------------

func TestGeneratePy_OutputFormat(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	outPath, content, err := python.GeneratePy(dir)
	if err != nil {
		t.Fatalf("GeneratePy: %v", err)
	}
	if !strings.HasSuffix(outPath, ".py.lyric") {
		t.Errorf("output path: want suffix .py.lyric, got %s", outPath)
	}
	pkgName := filepath.Base(dir)
	for _, want := range []string{
		"module " + pkgName,
		`source: ["shapes.py"]`,
		"class Circle",
		"class Point",
		"field radius: float",
		"method area(self) -> float",
		"interface Drawable",
		"typedef Color:",
		"func new_circle(x: float, y: float, r: float) -> Circle",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated content missing %q\n--- content ---\n%s", want, content)
		}
	}
	for _, bad := range []string{"#ldd:", "// --- index ---"} {
		if strings.Contains(content, bad) {
			t.Errorf("generated content should not contain %q", bad)
		}
	}
}

func TestGeneratePy_RoundTripsThroughCDD(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	_, content, err := python.GeneratePy(dir)
	if err != nil {
		t.Fatalf("GeneratePy: %v", err)
	}
	p, err := cdd.Parse(content, "shapes.py.lyric")
	if err != nil {
		t.Fatalf("cdd.Parse: %v\n--- content ---\n%s", err, content)
	}
	circle, ok := p.Structs["Circle"]
	if !ok {
		t.Fatal("Circle missing after round-trip")
	}
	if !circle.IsClass {
		t.Error("Circle.IsClass lost on round-trip (should be class)")
	}
	if _, ok := p.Structs["Point"]; !ok {
		t.Error("Point missing after round-trip")
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

// --- VerifyPy -------------------------------------------------------------

func generateAndWrite(t *testing.T, dir string) string {
	t.Helper()
	outPath, content, err := python.GeneratePy(dir)
	if err != nil {
		t.Fatalf("GeneratePy: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing .py.lyric: %v", err)
	}
	return outPath
}

func TestVerifyPy_Clean(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	outPath := generateAndWrite(t, dir)
	result, err := python.VerifyPy(outPath)
	if err != nil {
		t.Fatalf("VerifyPy: %v", err)
	}
	if result.ErrorCount() > 0 {
		for _, f := range result.Findings {
			if f.Severity == python.SevError {
				t.Errorf("unexpected error: %s", f.Message)
			}
		}
	}
}

func TestVerifyPy_MissingFunction(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource+extraFunc)
	outPath := generateAndWrite(t, dir)
	// Remove describe from source.
	if err := os.WriteFile(filepath.Join(dir, "shapes.py"), []byte(sampleSource), 0644); err != nil {
		t.Fatalf("writing stripped source: %v", err)
	}
	result, err := python.VerifyPy(outPath)
	if err != nil {
		t.Fatalf("VerifyPy: %v", err)
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

func TestVerifyPy_UndocumentedExport(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "shapes.py"),
		[]byte(sampleSource+extraFunc), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	result, err := python.VerifyPy(outPath)
	if err != nil {
		t.Fatalf("VerifyPy: %v", err)
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

func TestVerifyPy_FieldTypeMismatch(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	outPath, content, err := python.GeneratePy(dir)
	if err != nil {
		t.Fatalf("GeneratePy: %v", err)
	}
	corrupted := strings.Replace(content, "field radius: float", "field radius: str", 1)
	if err := os.WriteFile(outPath, []byte(corrupted), 0644); err != nil {
		t.Fatalf("writing corrupted .py.lyric: %v", err)
	}
	result, err := python.VerifyPy(outPath)
	if err != nil {
		t.Fatalf("VerifyPy: %v", err)
	}
	found := false
	for _, f := range result.Findings {
		if f.Severity == python.SevError && strings.Contains(f.Message, "radius") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error about radius type mismatch; got: %v", result.Findings)
	}
}

// --- UpdatePy -------------------------------------------------------------

func TestUpdatePy_AddsNewExport(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "shapes.py"),
		[]byte(sampleSource+extraFunc), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	added, err := python.UpdatePy(outPath)
	if err != nil {
		t.Fatalf("UpdatePy: %v", err)
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
	if !strings.Contains(string(updated), "func describe(c: Circle) -> str") {
		t.Errorf("updated .py.lyric should contain describe declaration; got:\n%s", updated)
	}
}

func TestUpdatePy_AlreadyUpToDate(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	outPath := generateAndWrite(t, dir)
	added, err := python.UpdatePy(outPath)
	if err != nil {
		t.Fatalf("UpdatePy: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("expected no additions, got: %v", added)
	}
}

// TestUpdatePy_PreservesHumanProse uses the cleaner approach: construct a
// PackageInfo with prose set, write it via cdd.Write to seed the fixture,
// then run UpdatePy. No fragile string-splicing.
// TestUpdatePy_RefreshesWhyFromSource verifies the source-of-truth policy:
// module-level prose is preserved (human/design-curated), per-decl why: is
// refreshed from the Python docstring (source wins), and a field with no source
// comment keeps its hand-written doc.
func TestUpdatePy_RefreshesWhyFromSource(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	outPath := filepath.Join(dir, filepath.Base(dir)+".py.lyric")

	p, err := python.ExtractPy(dir)
	if err != nil {
		t.Fatalf("ExtractPy: %v", err)
	}
	p.ModuleWhy = "geometry primitives"
	if c, ok := p.Structs["Circle"]; ok {
		c.Why = "a flat round geometric primitive" // stale: Circle has a docstring, so update overwrites this
		for i, f := range c.Fields {
			if f.Name == "radius" {
				c.Fields[i].Doc = "in scene units" // radius has no source comment → preserved
			}
		}
	}
	if err := os.WriteFile(outPath, []byte(cdd.Write(p)), 0644); err != nil {
		t.Fatalf("writing seed .py.lyric: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "shapes.py"),
		[]byte(sampleSource+extraFunc), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}
	if _, err := python.UpdatePy(outPath); err != nil {
		t.Fatalf("UpdatePy: %v", err)
	}

	updated, _ := os.ReadFile(outPath)
	updatedStr := string(updated)
	for _, want := range []string{
		`why: "geometry primitives"`,      // module why: human-curated, preserved
		`why: "A 2D circle."`,             // per-decl why: refreshed from docstring
		`doc: "in scene units"`,           // field doc: no source comment, preserved
		`func describe(c: Circle) -> str`, // new symbol added
	} {
		if !strings.Contains(updatedStr, want) {
			t.Errorf("update lost or failed to add %q\n--- file ---\n%s", want, updatedStr)
		}
	}
	// The stale hand-written Circle why must be overwritten by the docstring.
	if strings.Contains(updatedStr, "a flat round geometric primitive") {
		t.Errorf("stale hand-written Circle why should be overwritten by docstring\n--- file ---\n%s", updatedStr)
	}
}

func TestUpdatePy_RefreshesPositionsAndSource(t *testing.T) {
	requireExtractor(t)
	dir := writeTempPy(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	// Pad with 5 lines of comments so new_circle shifts down.
	shifted := "# pad\n# pad\n# pad\n# pad\n# pad\n" + sampleSource
	if err := os.WriteFile(filepath.Join(dir, "shapes.py"), []byte(shifted), 0644); err != nil {
		t.Fatalf("writing shifted source: %v", err)
	}
	if _, err := python.UpdatePy(outPath); err != nil {
		t.Fatalf("UpdatePy: %v", err)
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

// TestUpdatePy_MultipleFiles ensures multi-file extraction merges cleanly.
func TestUpdatePy_MultipleFiles(t *testing.T) {
	requireExtractor(t)
	dir := filepath.Join(t.TempDir(), "shapes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "shapes.py"), []byte(sampleSource), 0644)
	os.WriteFile(filepath.Join(dir, "utils.py"),
		[]byte("def clamp(n: float, lo: float, hi: float) -> float:\n    return n\n"), 0644)

	_, content, err := python.GeneratePy(dir)
	if err != nil {
		t.Fatalf("GeneratePy: %v", err)
	}
	if !strings.Contains(content, "func clamp") {
		t.Error("expected clamp from utils.py in output")
	}
	if !strings.Contains(content, "class Circle") {
		t.Error("expected Circle from shapes.py in output")
	}
	if !strings.Contains(content, `source: ["shapes.py","utils.py"]`) {
		t.Errorf("expected both source files in module source list; got:\n%s", content)
	}
}

// --- helpers --------------------------------------------------------------

func structNames(p *extract.PackageInfo) []string {
	var out []string
	for k := range p.Structs {
		out = append(out, k)
	}
	return out
}

func ifaceNames(p *extract.PackageInfo) []string {
	var out []string
	for k := range p.Interfaces {
		out = append(out, k)
	}
	return out
}

// Compile-time assertion that ExtractPy returns the expected type.
var _ func(string) (*extract.PackageInfo, error) = python.ExtractPy
