// Tests for the v2 .ts.lyric pipeline: ExtractTs, GenerateTs, UpdateTs,
// VerifyTs. The v1 native-TS-source-as-LDD path is gone; this file replaces
// the legacy LDD test suite.
package typescript_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/waywardgeek/lyre/pkg/extract"
	"github.com/waywardgeek/lyre/pkg/extract/typescript"
	"github.com/waywardgeek/lyre/pkg/cdd"
)

// sampleSource is a simple TypeScript module used as test input.
const sampleSource = `
export interface Drawable {
  draw(ctx: string): void;
  getColor(): string;
}

export class Circle implements Drawable {
  public radius: number;

  constructor(public center: [number, number], radius: number) {
    this.radius = radius;
  }

  draw(ctx: string): void {
    console.log(ctx);
  }

  getColor(): string {
    return "red";
  }
}

export type Color = "red" | "green" | "blue";

export function newCircle(x: number, y: number, r: number): Circle {
  return new Circle([x, y], r);
}

function _internal(): void {}
`

func writeTempTs(t *testing.T, src string) string {
	t.Helper()
	// Use a stable subdir name ("shapes") rather than t.TempDir()'s "/001"
	// basename so the .ts.lyric module name is a valid identifier.
	dir := filepath.Join(t.TempDir(), "shapes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(src), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	return dir
}

// --- ExtractTs --------------------------------------------------------------

func TestExtractTs_ClassAndMethods(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	p, err := typescript.ExtractTs(dir)
	if err != nil {
		t.Fatalf("ExtractTs: %v", err)
	}
	if p.Name != filepath.Base(dir) {
		t.Errorf("package name: want %q, got %q", filepath.Base(dir), p.Name)
	}
	if len(p.ModuleSource) != 1 || p.ModuleSource[0] != "shapes.ts" {
		t.Errorf("ModuleSource: want [shapes.ts], got %v", p.ModuleSource)
	}

	circle, ok := p.Structs["Circle"]
	if !ok {
		t.Fatal("Circle class missing")
	}
	if circle.File != "shapes.ts" || circle.Line == 0 {
		t.Errorf("Circle file/line: want shapes.ts:N, got %s:%d", circle.File, circle.Line)
	}
	if got, _ := circle.FieldSig("radius"); got != "number" {
		t.Errorf("Circle.radius signature: want number, got %q", got)
	}
	// constructor parameter property `public center: [number, number]` should
	// surface as a field.
	if got, _ := circle.FieldSig("center"); got != "[number, number]" {
		t.Errorf("Circle.center signature: want [number, number], got %q", got)
	}

	draw, ok := circle.Methods["draw"]
	if !ok {
		t.Fatal("Circle.draw missing")
	}
	if draw.SignatureText != "draw(ctx: string): void" {
		t.Errorf("draw signature: want %q, got %q", "draw(ctx: string): void", draw.SignatureText)
	}
	if draw.Source == "" || !strings.HasPrefix(draw.Source, "shapes.ts:") {
		t.Errorf("draw source: want shapes.ts:N, got %q", draw.Source)
	}
}

func TestExtractTs_Interface(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	p, err := typescript.ExtractTs(dir)
	if err != nil {
		t.Fatalf("ExtractTs: %v", err)
	}
	drawable, ok := p.Interfaces["Drawable"]
	if !ok {
		t.Fatal("Drawable interface missing")
	}
	if drawable.Methods["draw"].SignatureText != "draw(ctx: string): void" {
		t.Errorf("Drawable.draw signature: got %q", drawable.Methods["draw"].SignatureText)
	}
	if drawable.Methods["getColor"].SignatureText != "getColor(): string" {
		t.Errorf("Drawable.getColor signature: got %q", drawable.Methods["getColor"].SignatureText)
	}
}

func TestExtractTs_TypedefAndFunction(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	p, err := typescript.ExtractTs(dir)
	if err != nil {
		t.Fatalf("ExtractTs: %v", err)
	}
	color, ok := p.TypeDefs["Color"]
	if !ok {
		t.Fatal("Color typedef missing")
	}
	// Spec §7 normalizes runs of whitespace; the raw text from
	// node.getText() includes the literal source whitespace.
	want := `"red" | "green" | "blue"`
	if strings.Join(strings.Fields(color.Underlying), " ") != want {
		t.Errorf("Color underlying: want %q, got %q", want, color.Underlying)
	}

	nc, ok := p.Functions["newCircle"]
	if !ok {
		t.Fatal("newCircle function missing")
	}
	if nc.SignatureText != "newCircle(x: number, y: number, r: number): Circle" {
		t.Errorf("newCircle signature: got %q", nc.SignatureText)
	}
}

func TestExtractTs_SkipsUnexportedAndUnderscored(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	p, err := typescript.ExtractTs(dir)
	if err != nil {
		t.Fatalf("ExtractTs: %v", err)
	}
	if _, ok := p.Functions["_internal"]; ok {
		t.Error("_internal should not appear (unexported, underscore-prefixed)")
	}
}

func TestExtractTs_SkipsTestAndSpecFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shapes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(sampleSource), 0644)
	os.WriteFile(filepath.Join(dir, "shapes.test.ts"), []byte(`export function testHelper(): void {}`), 0644)
	os.WriteFile(filepath.Join(dir, "shapes.spec.ts"), []byte(`export function specHelper(): void {}`), 0644)

	p, err := typescript.ExtractTs(dir)
	if err != nil {
		t.Fatalf("ExtractTs: %v", err)
	}
	if _, ok := p.Functions["testHelper"]; ok {
		t.Error("testHelper from .test.ts should be skipped")
	}
	if _, ok := p.Functions["specHelper"]; ok {
		t.Error("specHelper from .spec.ts should be skipped")
	}
}

// --- GenerateTs + round-trip through cdd.Parse -----------------------------

func TestGenerateTs_OutputFormat(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	outPath, content, err := typescript.GenerateTs(dir)
	if err != nil {
		t.Fatalf("GenerateTs: %v", err)
	}
	if !strings.HasSuffix(outPath, ".ts.lyric") {
		t.Errorf("output path: want suffix .ts.lyric, got %s", outPath)
	}
	pkgName := filepath.Base(dir)
	for _, want := range []string{
		"module " + pkgName,
		`source: ["shapes.ts"]`,
		"class Circle",
		"field radius: number",
		"method draw(ctx: string): void",
		"interface Drawable",
		`typedef Color:`,
		"func newCircle(x: number, y: number, r: number): Circle",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated content missing %q\n--- content ---\n%s", want, content)
		}
	}
	for _, bad := range []string{"_internal", "//ldd:", "// --- index ---"} {
		if strings.Contains(content, bad) {
			t.Errorf("generated content should not contain %q", bad)
		}
	}
}

func TestGenerateTs_RoundTripsThroughCDD(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	_, content, err := typescript.GenerateTs(dir)
	if err != nil {
		t.Fatalf("GenerateTs: %v", err)
	}
	p, err := cdd.Parse(content, "shapes.ts.lyric")
	if err != nil {
		t.Fatalf("cdd.Parse: %v\n--- content ---\n%s", err, content)
	}
	if _, ok := p.Structs["Circle"]; !ok {
		t.Error("Circle missing after round-trip")
	}
	if _, ok := p.Interfaces["Drawable"]; !ok {
		t.Error("Drawable missing after round-trip")
	}
	if _, ok := p.Functions["newCircle"]; !ok {
		t.Error("newCircle missing after round-trip")
	}
	if _, ok := p.TypeDefs["Color"]; !ok {
		t.Error("Color missing after round-trip")
	}
}

// --- VerifyTs --------------------------------------------------------------

func generateAndWrite(t *testing.T, dir string) string {
	t.Helper()
	outPath, content, err := typescript.GenerateTs(dir)
	if err != nil {
		t.Fatalf("GenerateTs: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing .ts.lyric: %v", err)
	}
	return outPath
}

func TestVerifyTs_Clean(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	outPath := generateAndWrite(t, dir)
	result, err := typescript.VerifyTs(outPath)
	if err != nil {
		t.Fatalf("VerifyTs: %v", err)
	}
	if result.ErrorCount() > 0 {
		for _, f := range result.Findings {
			if f.Severity == typescript.SevError {
				t.Errorf("unexpected error: %s", f.Message)
			}
		}
	}
}

func TestVerifyTs_MissingFunction(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	stripped := strings.ReplaceAll(sampleSource,
		"export function newCircle(x: number, y: number, r: number): Circle {\n  return new Circle([x, y], r);\n}\n", "")
	if err := os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(stripped), 0644); err != nil {
		t.Fatalf("writing stripped source: %v", err)
	}

	result, err := typescript.VerifyTs(outPath)
	if err != nil {
		t.Fatalf("VerifyTs: %v", err)
	}
	found := false
	for _, f := range result.Findings {
		if strings.Contains(f.Message, "newCircle") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected finding about newCircle, got: %v", result.Findings)
	}
}

func TestVerifyTs_UndocumentedExport(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	extended := sampleSource + "\nexport function describe(c: Circle): string {\n  return \"\";\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	result, err := typescript.VerifyTs(outPath)
	if err != nil {
		t.Fatalf("VerifyTs: %v", err)
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

func TestVerifyTs_FieldTypeMismatch(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	outPath, content, err := typescript.GenerateTs(dir)
	if err != nil {
		t.Fatalf("GenerateTs: %v", err)
	}
	corrupted := strings.Replace(content, "field radius: number", "field radius: string", 1)
	if err := os.WriteFile(outPath, []byte(corrupted), 0644); err != nil {
		t.Fatalf("writing corrupted .ts.lyric: %v", err)
	}

	result, err := typescript.VerifyTs(outPath)
	if err != nil {
		t.Fatalf("VerifyTs: %v", err)
	}
	found := false
	for _, f := range result.Findings {
		if f.Severity == typescript.SevError && strings.Contains(f.Message, "radius") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error about radius type mismatch; got: %v", result.Findings)
	}
}

// --- UpdateTs --------------------------------------------------------------

func TestUpdateTs_AddsNewExport(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	extended := sampleSource + "\nexport function describe(c: Circle): string {\n  return \"\";\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	added, _, err := typescript.UpdateTs(outPath)
	if err != nil {
		t.Fatalf("UpdateTs: %v", err)
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
	if !strings.Contains(string(updated), "func describe(c: Circle): string") {
		t.Errorf("updated .ts.lyric should contain describe declaration; got:\n%s", updated)
	}
}

func TestUpdateTs_AlreadyUpToDate(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	added, _, err := typescript.UpdateTs(outPath)
	if err != nil {
		t.Fatalf("UpdateTs: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("expected no additions, got: %v", added)
	}
}

// TestUpdateTs_PreservesHumanProse uses the cleaner approach: construct a
// PackageInfo with the prose set, write it via cdd.Write to seed the
// fixture, then run UpdateTs. No fragile string-splicing.
func TestUpdateTs_PreservesHumanProse(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	outPath := filepath.Join(dir, filepath.Base(dir)+".ts.lyric")

	// Step 1: extract from source to get a baseline PackageInfo.
	p, err := typescript.ExtractTs(dir)
	if err != nil {
		t.Fatalf("ExtractTs: %v", err)
	}
	// Step 2: layer in human prose on the PackageInfo directly.
	p.ModuleWhy = "geometry primitives"
	if c, ok := p.Structs["Circle"]; ok {
		c.Why = "a flat round geometric primitive"
		// Mark per-field doc on radius.
		for i, f := range c.Fields {
			if f.Name == "radius" {
				c.Fields[i].Doc = "in scene units"
			}
		}
	}
	// Step 3: write via cdd.Write — clean, no string splicing.
	if err := os.WriteFile(outPath, []byte(cdd.Write(p)), 0644); err != nil {
		t.Fatalf("writing seed .ts.lyric: %v", err)
	}

	// Step 4: trigger an update by adding a new export to the source.
	extended := sampleSource + "\nexport function clone(c: Circle): Circle {\n  return c;\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}
	if _, _, err := typescript.UpdateTs(outPath); err != nil {
		t.Fatalf("UpdateTs: %v", err)
	}

	// Step 5: assert prose preserved + new export added.
	updated, _ := os.ReadFile(outPath)
	updatedStr := string(updated)
	for _, want := range []string{
		`why: "geometry primitives"`,
		`why: "a flat round geometric primitive"`,
		`doc: "in scene units"`,
		`func clone(c: Circle): Circle`,
	} {
		if !strings.Contains(updatedStr, want) {
			t.Errorf("update lost or failed to add %q\n--- file ---\n%s", want, updatedStr)
		}
	}
}

func TestUpdateTs_RefreshesPositionsAndSource(t *testing.T) {
	dir := writeTempTs(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	// Pad the source so symbols shift down by 5 lines.
	shifted := "// pad\n// pad\n// pad\n// pad\n// pad\n" + sampleSource
	if err := os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(shifted), 0644); err != nil {
		t.Fatalf("writing shifted source: %v", err)
	}
	if _, _, err := typescript.UpdateTs(outPath); err != nil {
		t.Fatalf("UpdateTs: %v", err)
	}

	raw, _ := os.ReadFile(outPath)
	p, err := cdd.Parse(string(raw), outPath)
	if err != nil {
		t.Fatalf("cdd.Parse: %v", err)
	}
	nc, ok := p.Functions["newCircle"]
	if !ok {
		t.Fatal("newCircle missing after update")
	}
	if nc.Line <= 5 {
		t.Errorf("newCircle line should have shifted past the 5-line pad; got line %d", nc.Line)
	}
}

// TestUpdateTs_MultipleFiles ensures multi-file extraction merges cleanly.
func TestUpdateTs_MultipleFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shapes")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(sampleSource), 0644)
	os.WriteFile(filepath.Join(dir, "utils.ts"),
		[]byte(`export function clamp(n: number, min: number, max: number): number { return Math.min(Math.max(n, min), max); }`), 0644)

	_, content, err := typescript.GenerateTs(dir)
	if err != nil {
		t.Fatalf("GenerateTs: %v", err)
	}
	if !strings.Contains(content, "func clamp") {
		t.Error("expected clamp from utils.ts in output")
	}
	if !strings.Contains(content, "class Circle") {
		t.Error("expected Circle from shapes.ts in output")
	}
	if !strings.Contains(content, `source: ["shapes.ts","utils.ts"]`) {
		t.Errorf("expected both source files in module source list; got:\n%s", content)
	}
}

// Compile-time assertion that ExtractTs returns the expected type — caught
// in case the shared data model shifts under us.
var _ func(string) (*extract.PackageInfo, error) = typescript.ExtractTs

// TestUpdateTs_PrunesRemovedExport proves prune-by-default: a function removed
// from source is dropped from the .ts.lyric and reported in `removed`.
func TestUpdateTs_PrunesRemovedExport(t *testing.T) {
	dir := writeTempTs(t, sampleSource+"\nexport function describe(c: Circle): string {\n  return \"\";\n}\n")
	outPath := generateAndWrite(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(sampleSource), 0644); err != nil {
		t.Fatalf("writing reduced source: %v", err)
	}
	_, removed, err := typescript.UpdateTs(outPath)
	if err != nil {
		t.Fatalf("UpdateTs: %v", err)
	}
	found := false
	for _, name := range removed {
		if strings.Contains(name, "describe") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected describe in removed list, got: %v", removed)
	}
	updated, _ := os.ReadFile(outPath)
	if strings.Contains(string(updated), "describe") {
		t.Errorf("pruned .ts.lyric should not mention describe; got:\n%s", updated)
	}
}
