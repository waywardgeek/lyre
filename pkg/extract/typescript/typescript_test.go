package typescript_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/waywardgeek/lyre/pkg/extract/typescript"
)

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

  get area(): number {
    return Math.PI * this.radius ** 2;
  }
}

export type Color = "red" | "green" | "blue";

export function newCircle(x: number, y: number, r: number): Circle {
  return new Circle([x, y], r);
}

function _internal(): void {}
`

func writeTempTsSource(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(src), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	return dir
}

func TestGenerateTsLDDFile_OutputFormat(t *testing.T) {
	dir := writeTempTsSource(t, sampleSource)
	outPath, content, err := typescript.GenerateTsLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateTsLDDFile: %v", err)
	}

	if !strings.HasSuffix(outPath, ".ts.lyric") {
		t.Errorf("expected output path ending in .ts.lyric, got %s", outPath)
	}

	if !strings.Contains(content, "//ldd:source") {
		t.Error("missing //ldd:source directive")
	}
	if !strings.Contains(content, "//ldd:why") {
		t.Error("missing //ldd:why directive")
	}
	if !strings.Contains(content, "// --- index ---") {
		t.Error("missing index marker")
	}
}

func TestGenerateTsLDDFile_ContainsDeclarations(t *testing.T) {
	dir := writeTempTsSource(t, sampleSource)
	_, content, err := typescript.GenerateTsLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateTsLDDFile: %v", err)
	}

	for _, want := range []string{"class Circle", "interface Drawable", "function newCircle", "type Color"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in output, not found", want)
		}
	}

	// Private/internal should NOT appear
	if strings.Contains(content, "_internal") {
		t.Error("_internal should not appear in LDD output")
	}
}

func TestGenerateTsLDDFile_ClassFields(t *testing.T) {
	dir := writeTempTsSource(t, sampleSource)
	_, content, err := typescript.GenerateTsLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateTsLDDFile: %v", err)
	}

	if !strings.Contains(content, "radius: number") {
		t.Error("expected 'radius: number' field in Circle class")
	}
}

func TestVerifyTsLDD_MissingSource(t *testing.T) {
	dir := t.TempDir()
	lddPath := filepath.Join(dir, "test.ts.lyric")
	os.WriteFile(lddPath, []byte("// no source directive\nclass Foo {}\n"), 0644)

	result, err := typescript.VerifyTsLDD(lddPath)
	if err != nil {
		t.Fatalf("VerifyTsLDD: %v", err)
	}
	if len(result.Findings) == 0 {
		t.Error("expected error for missing //ldd:source")
	}
}

func TestVerifyTsLDD_UndocumentedSymbol(t *testing.T) {
	dir := writeTempTsSource(t, sampleSource)
	outPath, _, err := typescript.GenerateTsLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateTsLDDFile: %v", err)
	}

	// Write a partial LDD that's missing newCircle
	partial := "//ldd:source shapes.ts\n//ldd:why \"\"\n\nclass Circle {}\ninterface Drawable {}\ntype Color = string;\n"
	os.WriteFile(outPath, []byte(partial), 0644)

	result, err := typescript.VerifyTsLDD(outPath)
	if err != nil {
		t.Fatalf("VerifyTsLDD: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if strings.Contains(f.Message, "newCircle") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about undocumented function newCircle")
	}
}

func TestUpdateTsLDD_AddsNewSymbol(t *testing.T) {
	dir := writeTempTsSource(t, sampleSource)
	outPath, content, err := typescript.GenerateTsLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateTsLDDFile: %v", err)
	}
	os.WriteFile(outPath, []byte(content), 0644)

	// Add a new function to the source
	extended := sampleSource + "\nexport function describe(c: Circle): string {\n  return `Circle(${c.radius})`;\n}\n"
	os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(extended), 0644)

	added, err := typescript.UpdateTsLDD(outPath)
	if err != nil {
		t.Fatalf("UpdateTsLDD: %v", err)
	}

	found := false
	for _, name := range added {
		if strings.Contains(name, "describe") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected describe in added list, got: %v", added)
	}

	updated, _ := os.ReadFile(outPath)
	if !strings.Contains(string(updated), "describe") {
		t.Error("updated LDD file should contain describe declaration")
	}
}

func TestUpdateTsLDD_AlreadyUpToDate(t *testing.T) {
	dir := writeTempTsSource(t, sampleSource)
	outPath, content, err := typescript.GenerateTsLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateTsLDDFile: %v", err)
	}
	os.WriteFile(outPath, []byte(content), 0644)

	added, err := typescript.UpdateTsLDD(outPath)
	if err != nil {
		t.Fatalf("UpdateTsLDD: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("expected no additions for up-to-date LDD, got: %v", added)
	}
}

func TestUpdateTsLDD_PreservesHumanWhy(t *testing.T) {
	dir := writeTempTsSource(t, sampleSource)
	outPath, content, err := typescript.GenerateTsLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateTsLDDFile: %v", err)
	}

	content = strings.Replace(content, `//ldd:why ""`, `//ldd:why "geometry primitives"`, 1)
	os.WriteFile(outPath, []byte(content), 0644)

	// Add a new function to trigger update
	extended := sampleSource + "\nexport function clone(c: Circle): Circle {\n  return c;\n}\n"
	os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(extended), 0644)

	_, err = typescript.UpdateTsLDD(outPath)
	if err != nil {
		t.Fatalf("UpdateTsLDD: %v", err)
	}

	updated, _ := os.ReadFile(outPath)
	if !strings.Contains(string(updated), `//ldd:why "geometry primitives"`) {
		t.Error("UpdateTsLDD should preserve the human-written //ldd:why annotation")
	}
}

func TestParseTsLDDMeta(t *testing.T) {
	src := `//ldd:source foo.ts, bar.ts
//ldd:why "handles request routing"

interface Router {
  route(path: string): void;
}
`
	meta := typescript.ParseTsLDDMeta(src)
	if meta.Lang != "typescript" {
		t.Errorf("expected lang=typescript, got %s", meta.Lang)
	}
	if len(meta.Source) != 2 {
		t.Errorf("expected 2 source files, got %d: %v", len(meta.Source), meta.Source)
	}
	if meta.Source[0] != "foo.ts" || meta.Source[1] != "bar.ts" {
		t.Errorf("unexpected source files: %v", meta.Source)
	}
	if meta.Why != "handles request routing" {
		t.Errorf("unexpected why: %q", meta.Why)
	}
}

func TestGenerateTsLDDFile_SkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(sampleSource), 0644)
	os.WriteFile(filepath.Join(dir, "shapes.test.ts"), []byte(`export function testHelper(): void {}`), 0644)
	os.WriteFile(filepath.Join(dir, "shapes.spec.ts"), []byte(`export function specHelper(): void {}`), 0644)

	_, content, err := typescript.GenerateTsLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateTsLDDFile: %v", err)
	}

	if strings.Contains(content, "testHelper") || strings.Contains(content, "specHelper") {
		t.Error("test/spec files should be excluded from generation")
	}
}

func TestGenerateTsLDDFile_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "shapes.ts"), []byte(sampleSource), 0644)
	os.WriteFile(filepath.Join(dir, "utils.ts"), []byte(`export function clamp(n: number, min: number, max: number): number { return Math.min(Math.max(n, min), max); }`), 0644)

	_, content, err := typescript.GenerateTsLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateTsLDDFile: %v", err)
	}

	if !strings.Contains(content, "clamp") {
		t.Error("expected clamp from utils.ts in output")
	}
	if !strings.Contains(content, "Circle") {
		t.Error("expected Circle from shapes.ts in output")
	}
	// Source should list both files
	if !strings.Contains(content, "shapes.ts") || !strings.Contains(content, "utils.ts") {
		t.Error("expected both source files in //ldd:source")
	}
}
