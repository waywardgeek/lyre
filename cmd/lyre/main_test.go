package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExpandLyricArgs covers the directory-expansion helper that lets
// verify/update/lint accept directories as well as files.
func TestExpandLyricArgs(t *testing.T) {
	// Build a temp tree:
	//   root/a.go.lyric
	//   root/sub/b.py.lyric
	//   root/sub/notes.txt        (ignored: not .lyric)
	//   root/vendor/c.go.lyric    (skipped: vendor/)
	//   root/node_modules/d.lyric (skipped: node_modules/)
	root := t.TempDir()
	mustWrite := func(rel string) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		return p
	}
	aLyric := mustWrite("a.go.lyric")
	bLyric := mustWrite("sub/b.py.lyric")
	mustWrite("sub/notes.txt")
	mustWrite("vendor/c.go.lyric")
	mustWrite("node_modules/d.lyric")

	t.Run("directory expands recursively, sorted, skipping vendor/node_modules", func(t *testing.T) {
		got, err := expandLyricArgs([]string{root})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{aLyric, bLyric} // sorted: a.go.lyric < sub/b.py.lyric
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("got[%d]=%q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("file argument passes through unchanged", func(t *testing.T) {
		got, err := expandLyricArgs([]string{aLyric})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != aLyric {
			t.Fatalf("got %v, want [%q]", got, aLyric)
		}
	})

	t.Run("mixed file and directory args preserve arg order, dir results sorted", func(t *testing.T) {
		got, err := expandLyricArgs([]string{aLyric, filepath.Join(root, "sub")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{aLyric, bLyric}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("got[%d]=%q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("directory with no .lyric files errors loudly", func(t *testing.T) {
		empty := t.TempDir()
		if _, err := expandLyricArgs([]string{empty}); err == nil {
			t.Fatalf("expected error for directory with no .lyric files, got nil")
		}
	})

	t.Run("nonexistent path errors", func(t *testing.T) {
		if _, err := expandLyricArgs([]string{filepath.Join(root, "nope")}); err == nil {
			t.Fatalf("expected error for nonexistent path, got nil")
		}
	})
}
