// Command lyre is the Lyric-Driven Development (LDD) CLI tool.
//
// Usage:
//
//	lyre verify <file.go.lyric|file.lyric> [...]  Check understanding files against source
//	lyre update <file.go.lyric|file.lyric> [...]  Regenerate auto-generated sections
//	lyre gen <package-dir>                         Scaffold a new understanding file from source
//	lyre fmt <file.lyric> [...]                    Format .lyric files
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/waywardgeek/lyre/pkg/extract"
	golang "github.com/waywardgeek/lyre/pkg/extract/golang"
	lyricext "github.com/waywardgeek/lyre/pkg/extract/lyric"
	"github.com/waywardgeek/lyre/pkg/extract/python"
	tsext "github.com/waywardgeek/lyre/pkg/extract/typescript"
	"github.com/waywardgeek/lyre/pkg/verifier"
)

const usage = `Usage: lyre <command> [arguments]

Commands:
  verify   <file> [...]          Check understanding files against source code
  update   <file> [...]          Regenerate auto-generated sections
  gen      <package-dir>         Scaffold a new understanding file from source
  fmt      <file.lyric> [...]    Format .lyric files (Lyric syntax only)
`

var commands = []string{"verify", "update", "gen", "fmt", "help"}

// resolveCommand matches a unique prefix of a command name.
func resolveCommand(prefix string) (string, error) {
	if prefix == "-h" || prefix == "--help" {
		return "help", nil
	}
	var matches []string
	for _, c := range commands {
		if c == prefix {
			return c, nil
		}
		if strings.HasPrefix(c, prefix) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("unknown command: %s", prefix)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous command %q: matches %s", prefix, strings.Join(matches, ", "))
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	cmd, cmdErr := resolveCommand(os.Args[1])
	if cmdErr != nil {
		fmt.Fprintf(os.Stderr, "%v\n\n%s", cmdErr, usage)
		os.Exit(1)
	}
	args := os.Args[2:]

	var err error
	switch cmd {
	case "verify":
		err = cmdVerify(args)
	case "update":
		err = cmdUpdate(args)
	case "gen":
		err = cmdGen(args)
	case "fmt":
		err = cmdFmt(args)
	case "help":
		fmt.Print(usage)
		return
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// isLyLyric returns true if the path is a .ly.lyric file (Lyric-native LDD)
// as opposed to a plain .lyric file (legacy Lyric declaration syntax).
func isLyLyric(path string) bool {
	return strings.HasSuffix(path, ".ly.lyric")
}

// --- verify ---

func cmdVerify(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lyre verify <file> [...]")
	}

	totalErrors, totalWarnings := 0, 0
	for _, path := range args {
		lang := extract.DetectLanguage(path)
		switch lang {
		case "go":
			result, err := golang.VerifyGo(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			for _, f := range result.Findings {
				fmt.Println(f)
				switch f.Severity {
				case golang.SevError:
					totalErrors++
				case golang.SevWarning:
					totalWarnings++
				}
			}
		case "python":
			result, err := python.VerifyPyLDD(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			for _, f := range result.Findings {
				fmt.Println(f)
				switch f.Severity {
				case python.SevError:
					totalErrors++
				case python.SevWarning:
					totalWarnings++
				}
			}
		case "lyric":
			if isLyLyric(path) {
				result, err := lyricext.VerifyLyLDD(path)
				if err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				for _, f := range result.Findings {
					fmt.Println(f)
					switch f.Severity {
					case lyricext.SevError:
						totalErrors++
					case lyricext.SevWarning:
						totalWarnings++
					}
				}
			} else {
				// Fall back to legacy Lyric-syntax verifier
				result, err := verifier.Verify(path)
				if err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				for _, f := range result.Findings {
					fmt.Println(f)
					switch f.Severity {
					case verifier.Error:
						totalErrors++
					case verifier.Warning:
						totalWarnings++
					}
				}
			}
		case "typescript":
			result, err := tsext.VerifyTsLDD(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			for _, f := range result.Findings {
				fmt.Println(f)
				switch f.Severity {
				case tsext.SevError:
					totalErrors++
				case tsext.SevWarning:
					totalWarnings++
				}
			}
		default:
			return fmt.Errorf("%s: unsupported language %q (supported: go, python, typescript, lyric)", path, lang)
		}
	}

	fmt.Printf("\n%d errors, %d warnings\n", totalErrors, totalWarnings)
	if totalErrors > 0 {
		os.Exit(1)
	}
	return nil
}

// --- update ---

func cmdUpdate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lyre update [--prune] <file> [...]")
	}
	prune := false
	var files []string
	for _, a := range args {
		if a == "--prune" {
			prune = true
		} else {
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("usage: lyre update [--prune] <file> [...]")
	}
	for _, path := range files {
		lang := extract.DetectLanguage(path)
		switch lang {
		case "lyric":
			if isLyLyric(path) {
				added, err := lyricext.UpdateLyLDD(path)
				if err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				if len(added) == 0 {
					fmt.Printf("%s: up to date\n", path)
				} else {
					fmt.Printf("%s: added %d declaration(s):\n", path, len(added))
					for _, name := range added {
						fmt.Printf("  + %s\n", name)
					}
				}
			} else {
				if err := runUpdate(path, prune); err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				fmt.Printf("updated %s\n", path)
			}
		case "go":
			added, err := golang.UpdateGo(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if len(added) == 0 {
				fmt.Printf("%s: up to date\n", path)
			} else {
				fmt.Printf("%s: added %d declaration(s):\n", path, len(added))
				for _, name := range added {
					fmt.Printf("  + %s\n", name)
				}
			}
		case "python":
			added, err := python.UpdatePyLDD(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if len(added) == 0 {
				fmt.Printf("%s: up to date\n", path)
			} else {
				fmt.Printf("%s: added %d declaration(s):\n", path, len(added))
				for _, name := range added {
					fmt.Printf("  + %s\n", name)
				}
			}
		case "typescript":
			added, err := tsext.UpdateTsLDD(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if len(added) == 0 {
				fmt.Printf("%s: up to date\n", path)
			} else {
				fmt.Printf("%s: added %d declaration(s):\n", path, len(added))
				for _, name := range added {
					fmt.Printf("  + %s\n", name)
				}
			}
		default:
			return fmt.Errorf("%s: update not yet supported for %s files", path, lang)
		}
	}
	return nil
}

// --- gen ---

func cmdGen(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: lyre gen <package-dir>")
	}
	pkgDir := args[0]

	// Detect language from source files in the directory
	lang := detectDirLanguage(pkgDir)
	switch lang {
	case "go":
		outPath, content, err := golang.GenerateGo(pkgDir)
		if err != nil {
			return err
		}
		if _, err := os.Stat(outPath); err == nil {
			return fmt.Errorf("%s already exists — use lyre update instead", outPath)
		}
		if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
			return err
		}
		fmt.Printf("generated %s\n", outPath)
	case "python":
		outPath, content, err := python.GeneratePyLDDFile(pkgDir)
		if err != nil {
			return err
		}
		if _, err := os.Stat(outPath); err == nil {
			return fmt.Errorf("%s already exists — use lyre update instead", outPath)
		}
		if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
			return err
		}
		fmt.Printf("generated %s\n", outPath)
	case "lyric":
		outPath, content, err := lyricext.GenerateLyLDDFile(pkgDir)
		if err != nil {
			return err
		}
		if _, err := os.Stat(outPath); err == nil {
			return fmt.Errorf("%s already exists — use lyre update instead", outPath)
		}
		if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
			return err
		}
		fmt.Printf("generated %s\n", outPath)
	case "typescript":
		outPath, content, err := tsext.GenerateTsLDDFile(pkgDir)
		if err != nil {
			return err
		}
		if _, err := os.Stat(outPath); err == nil {
			return fmt.Errorf("%s already exists — use lyre update instead", outPath)
		}
		if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
			return err
		}
		fmt.Printf("generated %s\n", outPath)
	default:
		return fmt.Errorf("unsupported language in %s (found: %s)", pkgDir, lang)
	}
	return nil
}

// --- fmt ---

// cmdFmt formats .lyric files in-place.
//
// TODO: real implementation. Currently a stub that errors clearly so the
// build passes. The v2 .lyric format (see cr/docs/rich-doc-upgrade-plan.md)
// will need a dedicated formatter in pkg/ldd/; this stub will be replaced
// when that lands.
func cmdFmt(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lyre fmt <file.lyric> [...]")
	}
	return fmt.Errorf("lyre fmt: not yet implemented (see cr/docs/rich-doc-upgrade-plan.md)")
}

// --- legacy update ---

// runUpdate is the legacy plain-.lyric update path (pre-.ly.lyric, pre-v2).
//
// TODO: real implementation, or delete callers. Currently a stub. Plain
// .lyric files in the old Forge-style syntax are vanishingly rare and will
// be migrated to v2 format in Phase 6 of the rich-doc upgrade sprint.
func runUpdate(path string, prune bool) error {
	return fmt.Errorf("lyre update: legacy plain-.lyric update is not implemented for %s (use .ly.lyric files, or wait for v2 format migration)", path)
}

// detectDirLanguage checks what source files are in a directory.
// It scans all entries and returns the highest-priority language found.
// Priority: go > lyric > python > typescript > rust (Go wins in mixed dirs).
func detectDirLanguage(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "unknown"
	}
	found := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
			found["go"] = true
		case strings.HasSuffix(name, ".ly"):
			found["lyric"] = true
		case strings.HasSuffix(name, ".py"):
			found["python"] = true
		case strings.HasSuffix(name, ".ts"):
			found["typescript"] = true
		case strings.HasSuffix(name, ".rs"):
			found["rust"] = true
		}
	}
	for _, lang := range []string{"go", "lyric", "python", "typescript", "rust"} {
		if found[lang] {
			return lang
		}
	}
	return "unknown"
}
