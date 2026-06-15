package verifier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)






func TestTypeDriftDetected(t *testing.T) {
	// Create a temporary Go file and .lyric file with deliberate type mismatches
	dir := t.TempDir()

	// Write a Go source file
	goSrc := `package example

type Widget struct {
	Name    string
	Count   int
	Tags    []string
	Options map[string]bool
}

func NewWidget(name string, count int) *Widget {
	return &Widget{Name: name, Count: count}
}
`
	goFile := filepath.Join(dir, "widget.go")
	if err := os.WriteFile(goFile, []byte(goSrc), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a .lyric file with deliberate drift
	forgeSrc := `lyric Example {
  struct Widget {
    name:    string
    count:   string
    tags:    [int]
    options: map[string]string
    missing: bool
  }

  func new_widget(name: string) -> Widget

  source: ["widget.go"]
}
`
	forgeFile := filepath.Join(dir, "example.lyric")
	if err := os.WriteFile(forgeFile, []byte(forgeSrc), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Verify(forgeFile)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	// Collect error messages
	var errors []string
	for _, f := range result.Findings {
		if f.Severity == Error {
			errors = append(errors, f.Message)
		}
	}

	// Expected errors:
	// 1. count type mismatch (string vs int)
	// 2. tags type mismatch ([]int vs []string)
	// 3. options type mismatch (map[string]string vs map[string]bool)
	// 4. missing field not in Go
	// 5. new_widget param count mismatch (1 vs 2)
	if len(errors) < 5 {
		t.Errorf("expected at least 5 errors, got %d:", len(errors))
		for _, e := range errors {
			t.Logf("  %s", e)
		}
	}

	// Verify specific drift was caught
	assertContains := func(substr string) {
		for _, e := range errors {
			if strings.Contains(e, substr) {
				return
			}
		}
		t.Errorf("expected error containing %q, not found in: %v", substr, errors)
	}

	assertContains("count")
	assertContains("type mismatch")
	assertContains("missing")
	assertContains("param count mismatch")
}

func TestSnakeToPascal(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"name", "Name"},
		{"type_params", "TypeParams"},
		{"is_many", "IsMany"},
		{"return_type", "ReturnType"},
		{"parse_forge_block", "ParseForgeBlock"},
	}
	for _, tt := range tests {
		got := snakeToPascal(tt.in)
		if got != tt.want {
			t.Errorf("snakeToPascal(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}


func TestCompletenessCheck(t *testing.T) {
	dir := t.TempDir()

	// Go file with several exported symbols
	goSrc := `package example

type Widget struct {
	Name string
}

type Config struct {
	Debug bool
}

type unexported struct {
	x int
}

func NewWidget() *Widget { return nil }
func ParseConfig() *Config { return nil }
func helper() {}
`
	if err := os.WriteFile(filepath.Join(dir, "example.go"), []byte(goSrc), 0644); err != nil {
		t.Fatal(err)
	}

	// .lyric only documents Widget — Config and ParseConfig are missing
	forgeSrc := `lyric Example {
  struct Widget {
    Name: string
  }

  func NewWidget() -> Widget?

  source: ["example.go"]
}
`
	forgeFile := filepath.Join(dir, "example.lyric")
	if err := os.WriteFile(forgeFile, []byte(forgeSrc), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Verify(forgeFile)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	var errors []string
	for _, f := range result.Findings {
		if f.Severity == Error {
			errors = append(errors, f.Message)
		}
	}

	// Should error about Config (exported type) and ParseConfig (exported func)
	// Should NOT error about unexported or helper
	assertError := func(substr string) {
		for _, e := range errors {
			if strings.Contains(e, substr) {
				return
			}
		}
		t.Errorf("expected error containing %q, got: %v", substr, errors)
	}
	assertNoError := func(substr string) {
		for _, e := range errors {
			if strings.Contains(e, substr) {
				t.Errorf("unexpected error containing %q: %s", substr, e)
			}
		}
	}

	assertError("Config")
	assertError("ParseConfig")
	assertNoError("unexported")
	assertNoError("helper")
}
