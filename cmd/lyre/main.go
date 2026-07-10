// Command lyre is the Lyric-Driven Development (LDD) CLI tool.
//
// Usage:
//
//	lyre verify <file-or-dir> [...]  Check understanding files against source
//	lyre update <file-or-dir> [...]  Regenerate auto-generated sections
//	lyre gen <package-dir>           Scaffold a new understanding file from source
//	lyre lint <file-or-dir> [...]    Report recoverable quality issues in .lyric files
//
// verify/update/lint accept directories as well as files; a directory is walked
// recursively for *.lyric files (skipping vendor/, node_modules/, and .git/).
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/waywardgeek/lyre/pkg/cdd"
	"github.com/waywardgeek/lyre/pkg/extract"
	golang "github.com/waywardgeek/lyre/pkg/extract/golang"
	lyricext "github.com/waywardgeek/lyre/pkg/extract/lyric"
	"github.com/waywardgeek/lyre/pkg/extract/python"
	tsext "github.com/waywardgeek/lyre/pkg/extract/typescript"
	"github.com/waywardgeek/lyre/pkg/gen"
	"github.com/waywardgeek/lyre/pkg/lint"
)

const usage = `Usage: lyre <command> [arguments]

Commands:
  verify   <file-or-dir> [...]   Check understanding files against source code
  update   <file-or-dir> [...]   Regenerate auto-generated sections
  gen      <package-dir>         Scaffold a new understanding file from source
  lint     <file-or-dir> [...]   Report recoverable quality issues in .lyric files

A directory argument to verify/update/lint is walked recursively for *.lyric
files (skipping vendor/, node_modules/, and .git/).
`

var commands = []string{"verify", "update", "gen", "lint", "help"}

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

// expandLyricArgs turns each argument into one or more .lyric file paths so the
// verify/update/lint commands can accept directories as well as files. A file
// argument is passed through unchanged (even if it doesn't end in .lyric — the
// caller's language detection reports that). A directory argument is walked
// recursively and every *.lyric file under it is collected, skipping vendor/,
// node_modules/, and .git/ subtrees (matching golang.DiscoverTestFuncs). The
// .lyric files discovered under a single directory are sorted for deterministic
// output; file arguments keep their given order. It is an error for a directory
// to contain no .lyric files, so a mistyped path fails loudly instead of
// silently doing nothing.
func expandLyricArgs(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, arg)
			continue
		}
		var found []string
		walkErr := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "vendor", "node_modules", ".git":
					return fs.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(d.Name(), ".lyric") {
				found = append(found, path)
			}
			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("scanning %s: %w", arg, walkErr)
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("no .lyric files found under %s", arg)
		}
		sort.Strings(found)
		out = append(out, found...)
	}
	return out, nil
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
	case "lint":
		err = cmdLint(args)
	case "help":
		fmt.Print(usage)
		return
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// --- verify ---

func cmdVerify(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lyre verify <file-or-dir> [...]")
	}

	paths, err := expandLyricArgs(args)
	if err != nil {
		return err
	}

	totalErrors, totalWarnings := 0, 0
	for _, path := range paths {
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
			result, err := python.VerifyPy(path)
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
			result, err := lyricext.VerifyLy(path)
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
		case "typescript":
			result, err := tsext.VerifyTs(path)
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

// reportUpdate prints the added and pruned declarations from a `lyre update`.
func reportUpdate(path string, added, removed []string) {
	if len(added) == 0 && len(removed) == 0 {
		fmt.Printf("%s: up to date\n", path)
		return
	}
	if len(added) > 0 {
		fmt.Printf("%s: added %d declaration(s):\n", path, len(added))
		for _, name := range added {
			fmt.Printf("  + %s\n", name)
		}
	}
	if len(removed) > 0 {
		fmt.Printf("%s: pruned %d declaration(s):\n", path, len(removed))
		for _, name := range removed {
			fmt.Printf("  - %s\n", name)
		}
	}
}

func cmdUpdate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lyre update <file-or-dir> [...]")
	}
	files, err := expandLyricArgs(args)
	if err != nil {
		return err
	}
	for _, path := range files {
		lang := extract.DetectLanguage(path)
		switch lang {
		case "lyric":
			added, removed, err := lyricext.UpdateLy(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			reportUpdate(path, added, removed)
		case "go":
			added, removed, err := golang.UpdateGo(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			reportUpdate(path, added, removed)
		case "python":
			added, removed, err := python.UpdatePy(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			reportUpdate(path, added, removed)
		case "typescript":
			added, removed, err := tsext.UpdateTs(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			reportUpdate(path, added, removed)
		default:
			return fmt.Errorf("%s: update not yet supported for %s files", path, lang)
		}
	}
	return nil
}

// --- gen ---

func cmdGen(args []string) error {
	rich := false
	var posArgs []string
	for _, a := range args {
		if a == "--rich" {
			rich = true
		} else {
			posArgs = append(posArgs, a)
		}
	}
	if len(posArgs) != 1 {
		return fmt.Errorf("usage: lyre gen [--rich] <package-dir>")
	}
	pkgDir := posArgs[0]

	// Detect language from source files in the directory
	lang := detectDirLanguage(pkgDir)

	// Rich path: Extract → Seed → cdd.Write. Bypasses the legacy
	// language-specific GenerateXxx because the seeding step is
	// language-agnostic and lives in pkg/gen.
	if rich {
		return genRich(pkgDir, lang)
	}

	switch lang {
	case "go":
		outPath, content, err := golang.GenerateGo(pkgDir)
		if err != nil {
			return err
		}
		return writeGenerated(outPath, content)
	case "python":
		outPath, content, err := python.GeneratePy(pkgDir)
		if err != nil {
			return err
		}
		return writeGenerated(outPath, content)
	case "lyric":
		outPath, content, err := lyricext.GenerateLy(pkgDir)
		if err != nil {
			return err
		}
		return writeGenerated(outPath, content)
	case "typescript":
		outPath, content, err := tsext.GenerateTs(pkgDir)
		if err != nil {
			return err
		}
		return writeGenerated(outPath, content)
	default:
		return fmt.Errorf("unsupported language in %s (found: %s)", pkgDir, lang)
	}
}

// writeGenerated writes content to outPath only if it doesn't already exist.
// Shared by both --rich and plain gen paths.
func writeGenerated(outPath, content string) error {
	if _, err := os.Stat(outPath); err == nil {
		return fmt.Errorf("%s already exists — use lyre update instead", outPath)
	}
	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("generated %s\n", outPath)
	return nil
}

// genRich runs the --rich pipeline: extract a *PackageInfo via the
// language-specific extractor, seed TODO placeholders into every empty
// rich-doc slot, then write via cdd.Write. The seeding step (pkg/gen)
// is language-agnostic.
func genRich(pkgDir, lang string) error {
	var (
		p   *extract.PackageInfo
		err error
		ext string
	)
	switch lang {
	case "go":
		p, err = golang.ExtractGo(pkgDir)
		ext = "go.lyric"
	case "python":
		p, err = python.ExtractPy(pkgDir)
		ext = "py.lyric"
	case "lyric":
		p, err = lyricext.ExtractLy(pkgDir)
		ext = "ly.lyric"
	case "typescript":
		p, err = tsext.ExtractTs(pkgDir)
		ext = "ts.lyric"
	default:
		return fmt.Errorf("unsupported language in %s (found: %s)", pkgDir, lang)
	}
	if err != nil {
		return err
	}
	gen.SeedRichPlaceholders(p)
	absDir, err := filepath.Abs(pkgDir)
	if err != nil {
		return err
	}
	outPath := filepath.Join(absDir, p.Name+"."+ext)
	return writeGenerated(outPath, cdd.Write(p))
}

// --- lint ---

// cmdLint runs the language-agnostic linter on one or more .lyric files.
// Each file is parsed via pkg/cdd (any syntactic error is fatal), then
// pkg/lint inspects the resulting *PackageInfo for recoverable issues
// (W001-W008). Exit code is 1 if --fatal-warnings is set and any warning
// fired; otherwise 0 regardless of warning count.
//
// W007 (dangling verified-by:) is enabled for Go-source .lyric files by
// discovering the module's test-function names (golang.DiscoverTestFuncs) and
// passing them as Opts.KnownTests. For non-Go .lyric files we pass a nil set,
// leaving W007 dormant rather than risk false positives from a test-discovery
// mechanism we don't yet have for that language. Discovery is cached per
// module root so linting many files in one invocation scans the tree once.
func cmdLint(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lyre lint [--fatal-warnings] <file.lyric> [...]")
	}
	fatal := false
	var rawArgs []string
	for _, a := range args {
		if a == "--fatal-warnings" {
			fatal = true
		} else {
			rawArgs = append(rawArgs, a)
		}
	}
	if len(rawArgs) == 0 {
		return fmt.Errorf("usage: lyre lint [--fatal-warnings] <file-or-dir> [...]")
	}
	files, err := expandLyricArgs(rawArgs)
	if err != nil {
		return err
	}
	// Cache discovered Go test sets by module root so repeated Go files in
	// the same module reuse a single tree walk.
	knownTestsByRoot := map[string]map[string]bool{}
	totalWarnings := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		pkg, err := cdd.Parse(string(data), path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		known, err := knownTestsFor(path, knownTestsByRoot)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		r := lint.Lint(pkg, path, lint.Opts{KnownTests: known})
		for _, f := range r.Findings {
			fmt.Println(f)
		}
		totalWarnings += r.WarningCount()
	}
	fmt.Printf("\n%d warnings\n", totalWarnings)
	if fatal && totalWarnings > 0 {
		os.Exit(1)
	}
	return nil
}

// knownTestsFor returns the set of test names to cross-reference W007 against
// for the given .lyric file, or nil to leave W007 dormant. Only Go-source
// .lyric files get a non-nil set today (see golang.DiscoverTestFuncs). Results
// are memoized in cache keyed by the .lyric's directory so a whole-module walk
// happens at most once per directory per invocation.
func knownTestsFor(path string, cache map[string]map[string]bool) (map[string]bool, error) {
	if extract.DetectLanguage(path) != "go" {
		return nil, nil
	}
	dir := filepath.Dir(path)
	if cached, ok := cache[dir]; ok {
		return cached, nil
	}
	known, err := golang.DiscoverTestFuncs(dir)
	if err != nil {
		return nil, err
	}
	cache[dir] = known
	return known, nil
}

// --- legacy update ---

// runUpdate is the legacy plain-.lyric update path (pre-.ly.lyric, pre-v2).
//
// TODO: real implementation, or delete callers. Currently a stub. Plain
// .lyric files in the old Forge-style syntax are vanishingly rare and will
// be migrated to v2 format in Phase 6 of the rich-doc upgrade sprint.
func runUpdate(path string) error {
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
		case strings.HasSuffix(name, "_test.go"):
			// Test-only directories should still be detected as "go" so the
			// Go extractor can choose to include _test.go when no production
			// files exist. Tracked at "go-test" to keep priority semantics
			// clean; downstream we collapse it into "go".
			found["go-test"] = true
		case strings.HasSuffix(name, ".ly"):
			found["lyric"] = true
		case strings.HasSuffix(name, ".py"):
			found["python"] = true
		case strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx"):
			// .tsx is TypeScript with JSX; the TS compiler API handles both
			// natively based on the file extension.
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
	// Fallback: test-only Go directory. We return "go" so the extractor can
	// surface a useful error or (post-fix) include _test.go files.
	if found["go-test"] {
		return "go"
	}
	return "unknown"
}
