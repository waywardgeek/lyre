package python_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/waywardgeek/lyre/pkg/extract/python"
)

// sampleSource is a simple Python package used as test input.
const sampleSource = `"""shapes — basic geometric primitives."""

from typing import Protocol


class Circle:
    """A round shape."""

    def __init__(self, radius: float, color: str) -> None:
        self.radius = radius
        self.color = color

    def area(self) -> float:
        return 3.14159 * self.radius ** 2

    def perimeter(self) -> float:
        return 2 * 3.14159 * self.radius

    def _private(self) -> None:
        pass


class Sizer(Protocol):
    """Something that can compute area and perimeter."""

    def area(self) -> float: ...
    def perimeter(self) -> float: ...


Scale = int


def new_circle(radius: float, color: str) -> Circle:
    """Create a circle."""
    return Circle(radius, color)


def _hidden() -> None:
    pass
`

// writeTempPySource writes the Python source into a temp dir.
func writeTempPySource(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shapes.py"), []byte(src), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	return dir
}

// TestGeneratePyLDDFile_OutputFormat checks that GeneratePyLDDFile produces a
// file with the expected structure and key declarations.
func TestGeneratePyLDDFile_OutputFormat(t *testing.T) {
	dir := writeTempPySource(t, sampleSource)
	outPath, content, err := python.GeneratePyLDDFile(dir)
	if err != nil {
		t.Fatalf("GeneratePyLDDFile: %v", err)
	}

	// Should be named after the package.
	if !strings.HasSuffix(outPath, "shapes.py.lyric") {
		t.Errorf("expected output path ending in shapes.py.lyric, got %s", outPath)
	}

	// Must have ldd directives.
	if !strings.Contains(content, "#ldd:source") {
		t.Error("missing #ldd:source")
	}
	if !strings.Contains(content, "#ldd:why") {
		t.Error("missing #ldd:why")
	}

	// Must have class, protocol, type alias, function.
	for _, want := range []string{"class Circle:", "class Sizer(Protocol):", "new_circle"} {
		if !strings.Contains(content, want) {
			t.Errorf("content missing %q", want)
		}
	}

	// Must NOT contain private names.
	if strings.Contains(content, "_hidden") {
		t.Error("content should not contain _hidden function")
	}
	if strings.Contains(content, "_private") {
		t.Error("content should not contain _private method")
	}

	// Must have index marker.
	if !strings.Contains(content, "# --- index ---") {
		t.Error("missing # --- index --- marker")
	}
}

// TestRoundTrip generates a .py.lyric file and immediately parses it back;
// the parsed PackageInfo should contain all the exported symbols.
func TestRoundTrip(t *testing.T) {
	dir := writeTempPySource(t, sampleSource)
	outPath, content, err := python.GeneratePyLDDFile(dir)
	if err != nil {
		t.Fatalf("GeneratePyLDDFile: %v", err)
	}

	// Write the generated file to disk so ParsePyLDDFile can read it.
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD file: %v", err)
	}

	info, meta, err := python.ParsePyLDDFile(outPath)
	if err != nil {
		t.Fatalf("ParsePyLDDFile: %v", err)
	}

	// Meta should list source files.
	if len(meta.Source) == 0 {
		t.Error("ParsePyLDDFile: no source files in meta")
	}

	// Struct Circle must be present.
	if _, ok := info.Structs["Circle"]; !ok {
		t.Error("struct Circle not found in parsed LDD")
	}

	// Interface Sizer must be present.
	if _, ok := info.Interfaces["Sizer"]; !ok {
		t.Error("interface Sizer not found in parsed LDD")
	}

	// Function new_circle must be present.
	if _, ok := info.Functions["new_circle"]; !ok {
		t.Error("function new_circle not found in parsed LDD")
	}
}

// TestVerifyPyLDD_Clean verifies that a freshly generated LDD reports no errors
// against the same source it was generated from.
func TestVerifyPyLDD_Clean(t *testing.T) {
	dir := writeTempPySource(t, sampleSource)
	outPath, content, err := python.GeneratePyLDDFile(dir)
	if err != nil {
		t.Fatalf("GeneratePyLDDFile: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD file: %v", err)
	}

	result, err := python.VerifyPyLDD(outPath)
	if err != nil {
		t.Fatalf("VerifyPyLDD: %v", err)
	}

	if result.ErrorCount() > 0 {
		for _, f := range result.Findings {
			if f.Severity == python.SevError {
				t.Errorf("unexpected error: %s", f.Message)
			}
		}
	}
}

// TestVerifyPyLDD_MissingFunction checks that VerifyPyLDD catches a function
// declared in the LDD that no longer exists in source.
func TestVerifyPyLDD_MissingFunction(t *testing.T) {
	dir := writeTempPySource(t, sampleSource)
	outPath, content, err := python.GeneratePyLDDFile(dir)
	if err != nil {
		t.Fatalf("GeneratePyLDDFile: %v", err)
	}

	// Remove new_circle from source — LDD still declares it.
	stripped := strings.ReplaceAll(sampleSource,
		"\ndef new_circle(radius: float, color: str) -> Circle:\n    \"\"\"Create a circle.\"\"\"\n    return Circle(radius, color)\n", "")
	if err := os.WriteFile(filepath.Join(dir, "shapes.py"), []byte(stripped), 0644); err != nil {
		t.Fatalf("writing stripped source: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD file: %v", err)
	}

	result, err := python.VerifyPyLDD(outPath)
	if err != nil {
		t.Fatalf("VerifyPyLDD: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if strings.Contains(f.Message, "new_circle") {
			found = true
		}
	}
	if !found {
		t.Error("expected finding about new_circle, got none")
	}
}

// TestVerifyPyLDD_UndocumentedExport checks that VerifyPyLDD catches an
// exported function present in source but absent from the LDD file.
func TestVerifyPyLDD_UndocumentedExport(t *testing.T) {
	dir := writeTempPySource(t, sampleSource)
	outPath, content, err := python.GeneratePyLDDFile(dir)
	if err != nil {
		t.Fatalf("GeneratePyLDDFile: %v", err)
	}

	// Add a new exported function to source that the LDD doesn't know about.
	extended := sampleSource + "\ndef describe(c: 'Circle') -> str:\n    return str(c)\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.py"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD file: %v", err)
	}

	result, err := python.VerifyPyLDD(outPath)
	if err != nil {
		t.Fatalf("VerifyPyLDD: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if strings.Contains(f.Message, "describe") {
			found = true
		}
	}
	if !found {
		t.Error("expected finding about undocumented describe, got none")
	}
}

// TestUpdatePyLDD_AddsNewExport verifies that UpdatePyLDD appends a declaration
// for a new exported function added to source after the LDD was generated.
func TestUpdatePyLDD_AddsNewExport(t *testing.T) {
	dir := writeTempPySource(t, sampleSource)
	outPath, content, err := python.GeneratePyLDDFile(dir)
	if err != nil {
		t.Fatalf("GeneratePyLDDFile: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD: %v", err)
	}

	// Add a new exported function to source.
	extended := sampleSource + "\ndef describe(c: 'Circle') -> str:\n    return str(c)\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.py"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	added, err := python.UpdatePyLDD(outPath)
	if err != nil {
		t.Fatalf("UpdatePyLDD: %v", err)
	}

	// Should report adding describe.
	found := false
	for _, name := range added {
		if strings.Contains(name, "describe") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected describe in added list, got: %v", added)
	}

	// File should now contain describe.
	updated, _ := os.ReadFile(outPath)
	if !strings.Contains(string(updated), "describe") {
		t.Error("updated LDD file should contain describe declaration")
	}
}

// TestUpdatePyLDD_AlreadyUpToDate verifies that UpdatePyLDD returns no additions
// when the LDD is already complete.
func TestUpdatePyLDD_AlreadyUpToDate(t *testing.T) {
	dir := writeTempPySource(t, sampleSource)
	outPath, content, err := python.GeneratePyLDDFile(dir)
	if err != nil {
		t.Fatalf("GeneratePyLDDFile: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD: %v", err)
	}

	added, err := python.UpdatePyLDD(outPath)
	if err != nil {
		t.Fatalf("UpdatePyLDD: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("expected no additions for up-to-date LDD, got: %v", added)
	}
}

// TestUpdatePyLDD_PreservesHumanWhy verifies that UpdatePyLDD preserves the
// #ldd:why annotation after update.
func TestUpdatePyLDD_PreservesHumanWhy(t *testing.T) {
	dir := writeTempPySource(t, sampleSource)
	outPath, content, err := python.GeneratePyLDDFile(dir)
	if err != nil {
		t.Fatalf("GeneratePyLDDFile: %v", err)
	}

	// Set a human-written why.
	content = strings.Replace(content, `#ldd:why ""`, `#ldd:why "geometry primitives"`, 1)
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD: %v", err)
	}

	// Add new symbol to trigger an update.
	extended := sampleSource + "\ndef clone(c: 'Circle') -> 'Circle':\n    return c\n"
	if err := os.WriteFile(filepath.Join(dir, "shapes.py"), []byte(extended), 0644); err != nil {
		t.Fatalf("writing extended source: %v", err)
	}

	if _, err := python.UpdatePyLDD(outPath); err != nil {
		t.Fatalf("UpdatePyLDD: %v", err)
	}

	updated, _ := os.ReadFile(outPath)
	if !strings.Contains(string(updated), `#ldd:why "geometry primitives"`) {
		t.Error("UpdatePyLDD should preserve the human-written #ldd:why annotation")
	}
}

// TestParsePyLDDMeta checks that metadata is correctly extracted from a stub file.
func TestParsePyLDDMeta(t *testing.T) {
	src := `#ldd:source foo.py, bar.py
#ldd:why "handles request routing"

class Router:
    def route(self, path: str) -> None: ...
`
	meta := python.ParsePyLDDMeta(src)
	if meta.Lang != "python" {
		t.Errorf("expected lang=python, got %s", meta.Lang)
	}
	if len(meta.Source) != 2 {
		t.Errorf("expected 2 source files, got %d: %v", len(meta.Source), meta.Source)
	}
	if meta.Source[0] != "foo.py" || meta.Source[1] != "bar.py" {
		t.Errorf("unexpected source files: %v", meta.Source)
	}
	if meta.Why != "handles request routing" {
		t.Errorf("unexpected why: %q", meta.Why)
	}
}

// TestExtractInit checks that fields declared in __init__ are extracted as class fields.
func TestExtractInit(t *testing.T) {
	dir := writeTempPySource(t, sampleSource)
	outPath, content, err := python.GeneratePyLDDFile(dir)
	if err != nil {
		t.Fatalf("GeneratePyLDDFile: %v", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing LDD: %v", err)
	}

	info, _, err := python.ParsePyLDDFile(outPath)
	if err != nil {
		t.Fatalf("ParsePyLDDFile: %v", err)
	}

	circle, ok := info.Structs["Circle"]
	if !ok {
		t.Fatal("Circle not found")
	}

	// Both radius and color come from __init__ typed params.
	for _, field := range []string{"radius", "color"} {
		if _, ok := circle.Fields[field]; !ok {
			t.Errorf("Circle should have field %s (from __init__)", field)
		}
	}
}
