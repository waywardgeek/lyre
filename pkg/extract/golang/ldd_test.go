// Tests for the v2 .go.lyric pipeline: ExtractGo, GenerateGo, UpdateGo,
// VerifyGo. The v1 native-Go-source-as-LDD path is gone; this file replaces
// the legacy LDD test suite.
package golang_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/waywardgeek/lyre/pkg/extract/golang"
	"github.com/waywardgeek/lyre/pkg/udd"
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

// --- GenerateGo + round-trip through udd.Parse -----------------------------

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

func TestGenerateGo_RoundTripsThroughUDD(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	_, content, err := golang.GenerateGo(dir)
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}
	p, err := udd.Parse(content, "shapes.go.lyric")
	if err != nil {
		t.Fatalf("udd.Parse: %v\n--- content ---\n%s", err, content)
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

	added, err := golang.UpdateGo(outPath)
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

	added, err := golang.UpdateGo(outPath)
	if err != nil {
		t.Fatalf("UpdateGo: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("expected no additions, got: %v", added)
	}
}

func TestUpdateGo_PreservesHumanProse(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath := generateAndWrite(t, dir)

	// Splice in module-level why + a doc block + a per-decl why on Circle.
	raw, _ := os.ReadFile(outPath)
	annotated := strings.Replace(string(raw),
		"module shapes\n",
		"module shapes\n  why: \"geometry primitives\"\n", 1)
	annotated = strings.Replace(annotated,
		"struct Circle\n    source:",
		"struct Circle\n    source:", 1)
	annotated = strings.Replace(annotated,
		"struct Circle\n",
		"struct Circle\n", 1)
	// Inject a per-decl why on Circle: find the source: line under struct Circle and append a why:.
	annotated = strings.Replace(annotated,
		"struct Circle\n    source:",
		"struct Circle\n    why: \"a flat round geometric primitive\"\n    source:", 1)
	// Add per-field doc on Radius.
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
	if _, err := golang.UpdateGo(outPath); err != nil {
		t.Fatalf("UpdateGo: %v", err)
	}

	updated, _ := os.ReadFile(outPath)
	updatedStr := string(updated)
	for _, want := range []string{
		`why: "geometry primitives"`,
		`why: "a flat round geometric primitive"`,
		`doc: "in scene units"`,
		`func Clone(c *Circle) *Circle`,
	} {
		if !strings.Contains(updatedStr, want) {
			t.Errorf("update lost or failed to add %q\n--- file ---\n%s", want, updatedStr)
		}
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
	if _, err := golang.UpdateGo(outPath); err != nil {
		t.Fatalf("UpdateGo: %v", err)
	}

	// Parse the resulting file and confirm NewCircle now points deeper.
	raw, _ := os.ReadFile(outPath)
	p, err := udd.Parse(string(raw), outPath)
	if err != nil {
		t.Fatalf("udd.Parse: %v", err)
	}
	nc, ok := p.Functions["NewCircle"]
	if !ok {
		t.Fatal("NewCircle missing after update")
	}
	if nc.Line <= 5 {
		t.Errorf("NewCircle line should have shifted past the 5-line pad; got line %d", nc.Line)
	}
}
