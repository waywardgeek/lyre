package golang_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// TestGenerateLDDFile_OutputFormat checks that GenerateLDDFile produces a file
// with the expected structure and key declarations.
func TestGenerateLDDFile_OutputFormat(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath, content, err := golang.GenerateLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateLDDFile: %v", err)
	}

	// Should be named after the package.
	if !strings.HasSuffix(outPath, "shapes.go.lyric") {
		t.Errorf("expected output path ending in shapes.go.lyric, got %s", outPath)
	}

	// Must have the build tag.
	if !strings.Contains(content, "//go:build ignore") {
		t.Error("missing //go:build ignore")
	}

	// Must have ldd directives.
	if !strings.Contains(content, "//ldd:source") {
		t.Error("missing //ldd:source")
	}
	if !strings.Contains(content, "//ldd:why") {
		t.Error("missing //ldd:why")
	}

	// Must have struct, interface, typedef, function.
	for _, want := range []string{"type Circle struct", "type Sizer interface", "type Scale", "func NewCircle"} {
		if !strings.Contains(content, want) {
			t.Errorf("content missing %q", want)
		}
	}

	// Must NOT contain unexported names.
	if strings.Contains(content, "unexported") {
		t.Error("content should not contain unexported function")
	}

	// Must have index marker.
	if !strings.Contains(content, "// --- index ---") {
		t.Error("missing // --- index --- marker")
	}
}

// TestRoundTrip generates a .go.lyric file and immediately parses it back;
// the parsed PackageInfo should contain all the exported symbols.
func TestRoundTrip(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath, content, err := golang.GenerateLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateLDDFile: %v", err)
	}

	// Write the generated file to disk so ParseLDDFile can read it.
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD file: %v", err)
	}

	info, meta, err := golang.ParseLDDFile(outPath)
	if err != nil {
		t.Fatalf("ParseLDDFile: %v", err)
	}

	// Meta should list source files.
	if len(meta.Source) == 0 {
		t.Error("ParseLDDFile: no source files in meta")
	}

	// Struct Circle must be present with its fields.
	circle, ok := info.Structs["Circle"]
	if !ok {
		t.Fatal("struct Circle not found in parsed LDD")
	}
	for _, field := range []string{"Radius", "Color"} {
		if _, ok := circle.Fields[field]; !ok {
			t.Errorf("Circle missing field %s", field)
		}
	}

	// Interface Sizer must be present.
	if _, ok := info.Interfaces["Sizer"]; !ok {
		t.Error("interface Sizer not found in parsed LDD")
	}

	// Function NewCircle must be present.
	if _, ok := info.Functions["NewCircle"]; !ok {
		t.Error("function NewCircle not found in parsed LDD")
	}
}

// TestVerifyGoLDD_Clean verifies that a freshly generated LDD reports no errors
// against the same source it was generated from.
func TestVerifyGoLDD_Clean(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath, content, err := golang.GenerateLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateLDDFile: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD file: %v", err)
	}

	result, err := golang.VerifyGoLDD(outPath)
	if err != nil {
		t.Fatalf("VerifyGoLDD: %v", err)
	}

	if result.ErrorCount() > 0 {
		for _, f := range result.Findings {
			if f.Severity == golang.SevError {
				t.Errorf("unexpected error: %s", f.Message)
			}
		}
	}
}

// TestVerifyGoLDD_MissingFunction checks that VerifyGoLDD catches a function
// declared in LDD that no longer exists in source.
func TestVerifyGoLDD_MissingFunction(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath, content, err := golang.GenerateLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateLDDFile: %v", err)
	}

	// Remove NewCircle from source — LDD still declares it.
	stripped := strings.ReplaceAll(sampleSource, "func NewCircle(radius float64, color string) *Circle { return nil }", "")
	if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(stripped), 0644); err != nil {
		t.Fatalf("writing stripped source: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD file: %v", err)
	}

	result, err := golang.VerifyGoLDD(outPath)
	if err != nil {
		t.Fatalf("VerifyGoLDD: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if strings.Contains(f.Message, "NewCircle") {
			found = true
		}
	}
	if !found {
		t.Error("expected finding about NewCircle, got none")
	}
}

// TestVerifyGoLDD_UndocumentedExport checks that VerifyGoLDD catches an
// exported function present in source but absent from the LDD file.
func TestVerifyGoLDD_UndocumentedExport(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath, content, err := golang.GenerateLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateLDDFile: %v", err)
	}

	// Add a new exported function to source that the LDD doesn't know about.
	extended := sampleSource + "\nfunc Describe(c *Circle) string { return \"\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD file: %v", err)
	}

	result, err := golang.VerifyGoLDD(outPath)
	if err != nil {
		t.Fatalf("VerifyGoLDD: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if strings.Contains(f.Message, "Describe") {
			found = true
		}
	}
	if !found {
		t.Error("expected finding about undocumented Describe, got none")
	}
}

// TestVerifyGoLDD_FieldTypeMismatch checks that VerifyGoLDD catches a type
// mismatch for a struct field.
func TestVerifyGoLDD_FieldTypeMismatch(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath, content, err := golang.GenerateLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateLDDFile: %v", err)
	}

	// Corrupt the LDD: change Circle.Radius from float64 to int.
	corrupted := strings.Replace(content, "Radius float64", "Radius int", 1)
	if err := os.WriteFile(outPath, []byte(corrupted), 0644); err != nil {
		t.Fatalf("writing corrupted LDD: %v", err)
	}

	result, err := golang.VerifyGoLDD(outPath)
	if err != nil {
		t.Fatalf("VerifyGoLDD: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Severity == golang.SevError && strings.Contains(f.Message, "Radius") {
			found = true
		}
	}
	if !found {
		t.Error("expected error about Radius type mismatch, got none")
	}
}

// TestUpdateGoLDD_AddsNewExport verifies that UpdateGoLDD appends a declaration
// for a new exported function that was added to source after the LDD was generated.
func TestUpdateGoLDD_AddsNewExport(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath, content, err := golang.GenerateLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateLDDFile: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD: %v", err)
	}

	// Add a new exported function to source.
	extended := sampleSource + "\nfunc Describe(c *Circle) string { return \"\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	added, err := golang.UpdateGoLDD(outPath)
	if err != nil {
		t.Fatalf("UpdateGoLDD: %v", err)
	}

	// Should report adding Describe.
	found := false
	for _, name := range added {
		if strings.Contains(name, "Describe") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Describe in added list, got: %v", added)
	}

	// File should now contain Describe.
	updated, _ := os.ReadFile(outPath)
	if !strings.Contains(string(updated), "Describe") {
		t.Error("updated LDD file should contain Describe declaration")
	}
}

// TestUpdateGoLDD_AlreadyUpToDate verifies that UpdateGoLDD returns no additions
// when the LDD is already complete.
func TestUpdateGoLDD_AlreadyUpToDate(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath, content, err := golang.GenerateLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateLDDFile: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD: %v", err)
	}

	added, err := golang.UpdateGoLDD(outPath)
	if err != nil {
		t.Fatalf("UpdateGoLDD: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("expected no additions for up-to-date LDD, got: %v", added)
	}
}

// TestUpdateGoLDD_PreservesHumanWhy verifies that UpdateGoLDD preserves the
// //ldd:why annotation after update.
func TestUpdateGoLDD_PreservesHumanWhy(t *testing.T) {
	dir := writeTempSource(t, sampleSource)
	outPath, content, err := golang.GenerateLDDFile(dir)
	if err != nil {
		t.Fatalf("GenerateLDDFile: %v", err)
	}

	// Set a human-written why.
	content = strings.Replace(content, `//ldd:why ""`, `//ldd:why "geometry primitives"`, 1)
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD: %v", err)
	}

	// Add new symbol to trigger an update.
	extended := sampleSource + "\nfunc Clone(c *Circle) *Circle { return nil }\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.go"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	if _, err := golang.UpdateGoLDD(outPath); err != nil {
		t.Fatalf("UpdateGoLDD: %v", err)
	}

	updated, _ := os.ReadFile(outPath)
	if !strings.Contains(string(updated), `//ldd:why "geometry primitives"`) {
		t.Error("UpdateGoLDD should preserve the human-written //ldd:why annotation")
	}
}

func TestParseLDDMeta(t *testing.T) {
	src := `//go:build ignore

//ldd:source foo.go, bar.go
//ldd:why "handles request routing"

package mypackage
`
	meta := golang.ParseLDDMeta(src)
	if meta.Lang != "go" {
		t.Errorf("expected lang=go, got %s", meta.Lang)
	}
	if len(meta.Source) != 2 {
		t.Errorf("expected 2 source files, got %d: %v", len(meta.Source), meta.Source)
	}
	if meta.Source[0] != "foo.go" || meta.Source[1] != "bar.go" {
		t.Errorf("unexpected source files: %v", meta.Source)
	}
	if meta.Why != "handles request routing" {
		t.Errorf("unexpected why: %q", meta.Why)
	}
}
