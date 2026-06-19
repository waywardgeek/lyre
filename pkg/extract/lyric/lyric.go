// Package lyric provides Lyric-syntax LDD file generation and parsing.
// A .ly.lyric file is a Lyric source file containing type declarations,
// function signatures, and LDD metadata in //ldd: comments.
//
// The extractor calls the pre-compiled extract_api binary from the Lyric
// project to parse .ly files and produce JSON matching PackageInfo.
package lyric

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/waywardgeek/lyre/pkg/extract"
)

// ExtractBinaryName is the name of the Lyric API extraction tool.
const ExtractBinaryName = "extract_api"

// Severity levels for verification findings.
type Severity int

const (
	SevError   Severity = iota
	SevWarning
	SevInfo
)

// Finding is a single verification issue.
type Finding struct {
	Severity Severity
	File     string
	Context  string
	Message  string
}

func (f Finding) String() string {
	prefix := "INFO"
	switch f.Severity {
	case SevError:
		prefix = "ERROR"
	case SevWarning:
		prefix = "WARNING"
	}
	return fmt.Sprintf("%s: %s: %s", prefix, f.File, f.Message)
}

// VerifyResult collects verification findings.
type VerifyResult struct {
	Findings []Finding
}

func (r *VerifyResult) add(sev Severity, file, ctx, msg string) {
	r.Findings = append(r.Findings, Finding{Severity: sev, File: file, Context: ctx, Message: msg})
}

// --- JSON types matching extract_api output ---

type lyPackageJSON struct {
	Name       string                    `json:"name"`
	Structs    map[string]lyStructJSON   `json:"structs"`
	Interfaces map[string]lyIfaceJSON    `json:"interfaces"`
	Functions  map[string]lyFuncJSON     `json:"functions"`
	TypeDefs   map[string]lyTypeDefJSON  `json:"typedefs"`
}

type lyStructJSON struct {
	Fields  map[string]string        `json:"fields"`
	Methods map[string]lyFuncJSON    `json:"methods"`
	File    string                   `json:"file"`
	Line    int                      `json:"line"`
	IsClass bool                     `json:"is_class"`
}

type lyIfaceJSON struct {
	Methods map[string]lyFuncJSON `json:"methods"`
	File    string                `json:"file"`
	Line    int                   `json:"line"`
}

type lyFuncJSON struct {
	Params  []lyParamJSON `json:"params"`
	Returns []string      `json:"returns"`
	File    string        `json:"file"`
	Line    int           `json:"line"`
}

type lyParamJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Mut  bool   `json:"mut,omitempty"`
}

type lyTypeDefJSON struct {
	Underlying string `json:"underlying"`
	File       string `json:"file"`
	Line       int    `json:"line"`
}

// --- Binary location ---

// findExtractBinary locates the extract_api binary.
// Search order: 1) LYRIC_HOME/tools/ 2) alongside lyric binary on PATH 3) ~/projects/lyric/tools/
func findExtractBinary() (string, error) {
	// Check LYRIC_HOME
	if home := os.Getenv("LYRIC_HOME"); home != "" {
		p := filepath.Join(home, "tools", ExtractBinaryName)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// Check alongside lyric binary on PATH
	if lyricPath, err := exec.LookPath("lyric"); err == nil {
		dir := filepath.Dir(lyricPath)
		p := filepath.Join(dir, "tools", ExtractBinaryName)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// Default: ~/projects/lyric/tools/
	home, err := os.UserHomeDir()
	if err == nil {
		p := filepath.Join(home, "projects", "lyric", "tools", ExtractBinaryName)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("cannot find %s binary; set LYRIC_HOME or ensure it's built (make tools in lyric project)", ExtractBinaryName)
}

// --- Script execution ---

// runExtract runs the extract_api binary against the given .ly files.
func runExtract(paths ...string) (*extract.PackageInfo, error) {
	bin, err := findExtractBinary()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(bin, paths...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("extract_api failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("running extract_api: %w", err)
	}

	var raw lyPackageJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing extractor output: %w\nraw: %s", err, string(out[:min(len(out), 200)]))
	}

	return convertPackageJSON(&raw), nil
}

func convertPackageJSON(raw *lyPackageJSON) *extract.PackageInfo {
	info := extract.NewPackageInfo(raw.Name)

	for name, s := range raw.Structs {
		si := extract.NewStructInfo()
		si.File = filepath.Base(s.File)
		si.Line = s.Line
		si.IsClass = s.IsClass
		for fn, ft := range s.Fields {
			si.SetField(fn, ft)
		}
		for mn, mf := range s.Methods {
			si.Methods[mn] = convertFuncJSON(mf)
		}
		info.Structs[name] = si
	}

	for name, iface := range raw.Interfaces {
		ii := extract.NewInterfaceInfo()
		ii.File = filepath.Base(iface.File)
		ii.Line = iface.Line
		for mn, mf := range iface.Methods {
			ii.Methods[mn] = convertFuncJSON(mf)
		}
		info.Interfaces[name] = ii
	}

	for name, fn := range raw.Functions {
		info.Functions[name] = convertFuncJSON(fn)
	}

	for name, td := range raw.TypeDefs {
		info.TypeDefs[name] = &extract.TypeDefInfo{
			Underlying: td.Underlying,
			File:       filepath.Base(td.File),
			Line:       td.Line,
		}
	}

	return info
}

func convertFuncJSON(fn lyFuncJSON) *extract.FuncInfo {
	fi := &extract.FuncInfo{File: filepath.Base(fn.File), Line: fn.Line}
	for _, p := range fn.Params {
		fi.Params = append(fi.Params, extract.ParamInfo{Name: p.Name, Type: p.Type, IsMut: p.Mut})
	}
	fi.Returns = fn.Returns
	return fi
}

// --- Metadata parsing ---

// ParseLyLDDMeta extracts //ldd: metadata from the text of a .ly.lyric file.
func ParseLyLDDMeta(src string) *extract.LDDMeta {
	meta := &extract.LDDMeta{Lang: "lyric"}
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//ldd:source ") {
			sources := strings.TrimPrefix(line, "//ldd:source ")
			for _, s := range strings.Split(sources, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					meta.Source = append(meta.Source, s)
				}
			}
		} else if strings.HasPrefix(line, "//ldd:why ") {
			meta.Why = strings.Trim(strings.TrimPrefix(line, "//ldd:why "), "\"")
		}
	}
	return meta
}

// --- Parse LDD file ---

// ParseLyLDDFile parses a .ly.lyric understanding file and returns both the
// declared API and the LDD metadata.
func ParseLyLDDFile(path string) (*extract.PackageInfo, *extract.LDDMeta, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	meta := ParseLyLDDMeta(string(src))

	// Run the extraction on the .ly.lyric file itself (it's valid Lyric syntax)
	info, err := runExtract(path)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return info, meta, nil
}

// --- Scan Lyric files ---

// ScanLyricFiles returns the .ly filenames in absDir (base names only),
// excluding test files.
func ScanLyricFiles(absDir string) ([]string, error) {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".ly") {
			continue
		}
		// Skip test files
		if strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test.ly") {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)
	return files, nil
}

// --- Generate LDD file ---

// writeLyLocation emits a file:line reference comment.
func writeLyLocation(b *strings.Builder, file string, line int) {
	if file != "" && line > 0 {
		b.WriteString(fmt.Sprintf("// %s:%d\n", file, line))
	}
}

// buildLyFuncSig builds a Lyric-syntax function signature.
func buildLyFuncSig(name string, hasSelf bool, fi *extract.FuncInfo) string {
	var b strings.Builder
	b.WriteString("func ")
	b.WriteString(name)
	b.WriteString("(")
	first := true
	if hasSelf {
		b.WriteString("self")
		first = false
	}
	for _, p := range fi.Params {
		if !first {
			b.WriteString(", ")
		}
		first = false
		if p.IsMut {
			b.WriteString("mut ")
		}
		b.WriteString(p.Name)
		if p.Type != "" && p.Type != "any" {
			b.WriteString(": ")
			b.WriteString(p.Type)
		}
	}
	b.WriteString(")")
	if len(fi.Returns) > 0 && fi.Returns[0] != "" {
		if len(fi.Returns) == 1 {
			b.WriteString(" -> ")
			b.WriteString(fi.Returns[0])
		} else {
			b.WriteString(" -> (")
			b.WriteString(strings.Join(fi.Returns, ", "))
			b.WriteString(")")
		}
	}
	return b.String()
}

// GenerateLyLDDFile produces a .ly.lyric understanding file for a directory of .ly files.
func GenerateLyLDDFile(pkgDir string) (outPath, content string, err error) {
	absDir, err := filepath.Abs(pkgDir)
	if err != nil {
		return "", "", err
	}

	lyFiles, err := ScanLyricFiles(absDir)
	if err != nil {
		return "", "", err
	}
	if len(lyFiles) == 0 {
		return "", "", fmt.Errorf("no .ly files found in %s", pkgDir)
	}

	// Run extractor on all .ly files
	var fullPaths []string
	for _, f := range lyFiles {
		fullPaths = append(fullPaths, filepath.Join(absDir, f))
	}
	merged, err := runExtract(fullPaths...)
	if err != nil {
		return "", "", err
	}

	// Determine module name
	moduleName := merged.Name
	if moduleName == "" {
		moduleName = filepath.Base(absDir)
	}

	// Build .ly.lyric content
	var b strings.Builder
	b.WriteString("//ldd:source ")
	b.WriteString(strings.Join(lyFiles, ", "))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("//ldd:why \"TODO: describe what %s does\"\n", moduleName))
	b.WriteString("\n")

	// Sorted keys for deterministic output
	structNames := sortedStructKeys(merged.Structs)
	ifaceNames := sortedIfaceKeys(merged.Interfaces)
	funcNames := sortedFuncKeys(merged.Functions)
	typeDefNames := sortedTypeDefKeys(merged.TypeDefs)

	// Enums (typedefs)
	for _, name := range typeDefNames {
		td := merged.TypeDefs[name]
		writeLyLocation(&b, td.File, td.Line)
		b.WriteString(fmt.Sprintf("// %s = %s\n\n", name, td.Underlying))
	}

	// Structs and classes
	for _, name := range structNames {
		si := merged.Structs[name]
		writeLyLocation(&b, si.File, si.Line)
		keyword := "struct"
		if si.IsClass {
			keyword = "class"
		}
		b.WriteString(fmt.Sprintf("%s %s {\n", keyword, name))
		for _, f := range extract.SortedFieldsByName(si.Fields) {
			b.WriteString(fmt.Sprintf("  %s: %s\n", f.Name, f.SignatureText))
		}
		b.WriteString("}\n")

		methodNames := sortedMethodKeys(si.Methods)
		for _, mn := range methodNames {
			mf := si.Methods[mn]
			writeLyLocation(&b, mf.File, mf.Line)
			b.WriteString(fmt.Sprintf("%s\n", buildLyFuncSig(fmt.Sprintf("%s.%s", name, mn), true, mf)))
		}
		b.WriteString("\n")
	}

	// Interfaces
	for _, name := range ifaceNames {
		ii := merged.Interfaces[name]
		writeLyLocation(&b, ii.File, ii.Line)
		b.WriteString(fmt.Sprintf("interface %s {\n", name))
		methodNames := sortedMethodKeys(ii.Methods)
		for _, mn := range methodNames {
			b.WriteString(fmt.Sprintf("  %s\n", buildLyFuncSig(mn, true, ii.Methods[mn])))
		}
		b.WriteString("}\n\n")
	}

	// Free functions
	for _, name := range funcNames {
		fi := merged.Functions[name]
		writeLyLocation(&b, fi.File, fi.Line)
		b.WriteString(fmt.Sprintf("%s\n", buildLyFuncSig(name, false, fi)))
	}

	b.WriteString("\n// --- index ---\n")
	b.WriteString("// DO NOT EDIT below this line — regenerated by `lyre update`.\n")

	// Determine output filename
	dirBase := filepath.Base(absDir)
	outName := dirBase + ".ly.lyric"
	outPath = filepath.Join(absDir, outName)

	return outPath, b.String(), nil
}

// --- Verify ---

// VerifyLyLDD checks a .ly.lyric file against its source files.
func VerifyLyLDD(path string) (*VerifyResult, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	srcStr := string(src)

	declared, meta, err := ParseLyLDDFile(path)
	if err != nil {
		return nil, err
	}

	result := &VerifyResult{}
	if len(meta.Source) == 0 {
		result.add(SevError, path, srcStr, "no //ldd:source directive")
		return result, nil
	}

	// Extract actual API from source files.
	lddDir := filepath.Dir(path)
	var fullPaths []string
	for _, srcFile := range meta.Source {
		fullPath := filepath.Join(lddDir, srcFile)
		if _, err := os.Stat(fullPath); err != nil {
			result.add(SevError, path, srcFile, fmt.Sprintf("source file %s does not exist", srcFile))
			continue
		}
		fullPaths = append(fullPaths, fullPath)
	}
	if len(fullPaths) == 0 {
		return result, nil
	}

	actual, err := runExtract(fullPaths...)
	if err != nil {
		return nil, fmt.Errorf("extracting source: %w", err)
	}

	// Compare declared vs actual
	compareLyStructs(declared, actual, path, srcStr, result)
	compareLyInterfaces(declared, actual, path, srcStr, result)
	compareLyFunctions(declared, actual, path, srcStr, result)
	checkLyCompleteness(declared, actual, path, srcStr, result)

	return result, nil
}

func compareLyStructs(declared, actual *extract.PackageInfo, file, src string, r *VerifyResult) {
	for name, ds := range declared.Structs {
		as, ok := actual.Structs[name]
		if !ok {
			r.add(SevError, file, src, fmt.Sprintf("struct/class %s declared in LDD but not found in source", name))
			continue
		}
		for _, f := range ds.Fields {
			if !as.HasField(f.Name) {
				r.add(SevError, file, src, fmt.Sprintf("%s.%s: field declared in LDD but not found in source", name, f.Name))
			}
		}
		for mn := range ds.Methods {
			if _, ok := as.Methods[mn]; !ok {
				r.add(SevError, file, src, fmt.Sprintf("%s.%s: method declared in LDD but not found in source", name, mn))
			}
		}
	}
}

func compareLyInterfaces(declared, actual *extract.PackageInfo, file, src string, r *VerifyResult) {
	for name, di := range declared.Interfaces {
		ai, ok := actual.Interfaces[name]
		if !ok {
			r.add(SevError, file, src, fmt.Sprintf("interface %s declared in LDD but not found in source", name))
			continue
		}
		for mn := range di.Methods {
			if _, ok := ai.Methods[mn]; !ok {
				r.add(SevError, file, src, fmt.Sprintf("%s.%s: method declared in LDD but not found in source", name, mn))
			}
		}
	}
}

func compareLyFunctions(declared, actual *extract.PackageInfo, file, src string, r *VerifyResult) {
	for name := range declared.Functions {
		if _, ok := actual.Functions[name]; !ok {
			r.add(SevError, file, src, fmt.Sprintf("function %s declared in LDD but not found in source", name))
		}
	}
}

func checkLyCompleteness(declared, actual *extract.PackageInfo, file, src string, r *VerifyResult) {
	for name := range actual.Structs {
		if _, ok := declared.Structs[name]; !ok {
			r.add(SevWarning, file, src, fmt.Sprintf("struct/class %s in source is not documented in LDD", name))
		}
	}
	for name := range actual.Interfaces {
		if _, ok := declared.Interfaces[name]; !ok {
			r.add(SevWarning, file, src, fmt.Sprintf("interface %s in source is not documented in LDD", name))
		}
	}
	for name := range actual.Functions {
		if _, ok := declared.Functions[name]; !ok {
			r.add(SevWarning, file, src, fmt.Sprintf("function %s in source is not documented in LDD", name))
		}
	}
}

// --- Update ---

// UpdateLyLDD refreshes a .ly.lyric file by adding any symbols from
// source that are not yet documented. Returns a summary of what was added.
func UpdateLyLDD(lddPath string) (added []string, err error) {
	src, err := os.ReadFile(lddPath)
	if err != nil {
		return nil, err
	}
	text := string(src)

	declared, meta, err := ParseLyLDDFile(lddPath)
	if err != nil {
		return nil, fmt.Errorf("parsing LDD file: %w", err)
	}
	if len(meta.Source) == 0 {
		return nil, fmt.Errorf("no //ldd:source directive in %s", lddPath)
	}

	// Extract actual API
	lddDir := filepath.Dir(lddPath)
	var fullPaths []string
	for _, srcFile := range meta.Source {
		fullPaths = append(fullPaths, filepath.Join(lddDir, srcFile))
	}
	actual, err := runExtract(fullPaths...)
	if err != nil {
		return nil, fmt.Errorf("extracting source: %w", err)
	}

	// Split at index marker
	const indexMarker = "\n// --- index ---\n"
	humanPart, _ := splitAtIndexMarker(text)

	var newDecls strings.Builder

	// Missing structs/classes
	var missingStructNames []string
	for name := range actual.Structs {
		if _, ok := declared.Structs[name]; !ok {
			missingStructNames = append(missingStructNames, name)
		}
	}
	sort.Strings(missingStructNames)
	for _, name := range missingStructNames {
		si := actual.Structs[name]
		keyword := "struct"
		if si.IsClass {
			keyword = "class"
		}
		newDecls.WriteString("\n")
		writeLyLocation(&newDecls, si.File, si.Line)
		newDecls.WriteString(fmt.Sprintf("%s %s {\n", keyword, name))
		for _, f := range extract.SortedFieldsByName(si.Fields) {
			newDecls.WriteString(fmt.Sprintf("  %s: %s\n", f.Name, f.SignatureText))
		}
		newDecls.WriteString("}\n")
		for _, mn := range sortedMethodKeys(si.Methods) {
			mf := si.Methods[mn]
			writeLyLocation(&newDecls, mf.File, mf.Line)
			newDecls.WriteString(fmt.Sprintf("%s\n", buildLyFuncSig(fmt.Sprintf("%s.%s", name, mn), true, mf)))
		}
		added = append(added, name)
	}

	// Missing interfaces
	var missingIfaceNames []string
	for name := range actual.Interfaces {
		if _, ok := declared.Interfaces[name]; !ok {
			missingIfaceNames = append(missingIfaceNames, name)
		}
	}
	sort.Strings(missingIfaceNames)
	for _, name := range missingIfaceNames {
		ii := actual.Interfaces[name]
		newDecls.WriteString("\n")
		writeLyLocation(&newDecls, ii.File, ii.Line)
		newDecls.WriteString(fmt.Sprintf("interface %s {\n", name))
		for _, mn := range sortedMethodKeys(ii.Methods) {
			newDecls.WriteString(fmt.Sprintf("  %s\n", buildLyFuncSig(mn, true, ii.Methods[mn])))
		}
		newDecls.WriteString("}\n")
		added = append(added, name)
	}

	// Missing functions
	var missingFuncNames []string
	for name := range actual.Functions {
		if _, ok := declared.Functions[name]; !ok {
			missingFuncNames = append(missingFuncNames, name)
		}
	}
	sort.Strings(missingFuncNames)
	for _, name := range missingFuncNames {
		fi := actual.Functions[name]
		newDecls.WriteString("\n")
		writeLyLocation(&newDecls, fi.File, fi.Line)
		newDecls.WriteString(fmt.Sprintf("%s\n", buildLyFuncSig(name, false, fi)))
		added = append(added, name)
	}

	// Missing enums/typedefs
	var missingTypeDefNames []string
	for name := range actual.TypeDefs {
		if _, ok := declared.TypeDefs[name]; !ok {
			missingTypeDefNames = append(missingTypeDefNames, name)
		}
	}
	sort.Strings(missingTypeDefNames)
	for _, name := range missingTypeDefNames {
		td := actual.TypeDefs[name]
		newDecls.WriteString("\n")
		writeLyLocation(&newDecls, td.File, td.Line)
		newDecls.WriteString(fmt.Sprintf("// %s = %s\n", name, td.Underlying))
		added = append(added, name)
	}

	if len(added) == 0 {
		return nil, nil
	}

	// Rebuild file: human part + new decls + index marker + auto-generated section
	var rebuilt strings.Builder
	rebuilt.WriteString(strings.TrimRight(humanPart, "\n"))
	rebuilt.WriteString("\n")
	rebuilt.WriteString(newDecls.String())
	rebuilt.WriteString("\n// --- index ---\n")
	rebuilt.WriteString("// DO NOT EDIT below this line — regenerated by `lyre update`.\n")

	if err := os.WriteFile(lddPath, []byte(rebuilt.String()), 0644); err != nil {
		return nil, err
	}

	return added, nil
}

func splitAtIndexMarker(text string) (human, auto string) {
	const marker = "\n// --- index ---\n"
	i := strings.Index(text, marker)
	if i < 0 {
		return text, ""
	}
	return text[:i], text[i+len(marker):]
}

// --- Helpers ---

func sortedStructKeys(m map[string]*extract.StructInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedIfaceKeys(m map[string]*extract.InterfaceInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedFuncKeys(m map[string]*extract.FuncInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedTypeDefKeys(m map[string]*extract.TypeDefInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedMethodKeys(m map[string]*extract.FuncInfo) []string {
	return sortedFuncKeys(m)
}

func sortedStringMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
