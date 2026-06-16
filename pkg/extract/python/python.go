// Package python provides Python-syntax LDD file generation and parsing.
// A .py.lyric file is a Python stub file (.pyi style) with #ldd: metadata
// comments at the top, used as a living understanding document for a Python
// package.
package python

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/waywardgeek/lyre/pkg/extract"
)

//go:embed extract_api.py
var extractScript []byte

// --- Severity / Finding / VerifyResult ---

// Severity levels for verification findings.
type Severity int

const (
	SevError   Severity = iota
	SevWarning          // nolint:deadcode
	SevInfo             // nolint:deadcode
)

func (s Severity) String() string {
	switch s {
	case SevError:
		return "ERROR"
	case SevWarning:
		return "WARNING"
	case SevInfo:
		return "INFO"
	}
	return "UNKNOWN"
}

// Finding is a single verification result.
type Finding struct {
	Severity Severity
	File     string
	Source   string
	Message  string
}

func (f Finding) String() string {
	loc := f.File
	if f.Source != "" {
		loc = fmt.Sprintf("%s ↔ %s", f.File, f.Source)
	}
	return fmt.Sprintf("[%s] %s: %s", f.Severity, loc, f.Message)
}

// VerifyResult holds all findings from a verification run.
type VerifyResult struct {
	Findings []Finding
}

func (r *VerifyResult) add(sev Severity, file, source, msg string) {
	r.Findings = append(r.Findings, Finding{
		Severity: sev,
		File:     file,
		Source:   source,
		Message:  msg,
	})
}

// ErrorCount returns the number of error-level findings.
func (r *VerifyResult) ErrorCount() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == SevError {
			n++
		}
	}
	return n
}

// --- JSON schema for Python extractor output ---

type pyPackageJSON struct {
	Name       string                    `json:"name"`
	Structs    map[string]pyStructJSON   `json:"structs"`
	Interfaces map[string]pyIfaceJSON    `json:"interfaces"`
	Functions  map[string]pyFuncJSON     `json:"functions"`
	TypeDefs   map[string]string         `json:"typedefs"`
}

type pyStructJSON struct {
	Fields  map[string]string         `json:"fields"`
	Methods map[string]pyFuncJSON     `json:"methods"`
}

type pyIfaceJSON struct {
	Methods map[string]pyFuncJSON `json:"methods"`
}

type pyFuncJSON struct {
	Params  []pyParamJSON `json:"params"`
	Returns []string      `json:"returns"`
}

type pyParamJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// --- Script execution ---

// runExtractScript runs extract_api.py against the given file path and returns
// the parsed JSON output as a PackageInfo.
func runExtractScript(srcPath string) (*extract.PackageInfo, error) {
	// Write the embedded script to a temp file.
	tmp, err := os.CreateTemp("", "lyre-extract-*.py")
	if err != nil {
		return nil, fmt.Errorf("creating temp script: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(extractScript); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("writing temp script: %w", err)
	}
	tmp.Close()

	out, err := exec.Command("python3", tmp.Name(), srcPath).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("extract_api.py failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("running python3: %w", err)
	}

	var raw pyPackageJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing extractor output: %w", err)
	}

	return convertPackageJSON(&raw), nil
}

// convertPackageJSON converts the raw JSON output into a PackageInfo.
func convertPackageJSON(raw *pyPackageJSON) *extract.PackageInfo {
	info := extract.NewPackageInfo(raw.Name)

	for name, s := range raw.Structs {
		si := extract.NewStructInfo()
		for fn, ft := range s.Fields {
			si.Fields[fn] = ft
		}
		for mn, mf := range s.Methods {
			si.Methods[mn] = convertFuncJSON(mf)
		}
		info.Structs[name] = si
	}

	for name, iface := range raw.Interfaces {
		ii := extract.NewInterfaceInfo()
		for mn, mf := range iface.Methods {
			ii.Methods[mn] = convertFuncJSON(mf)
		}
		info.Interfaces[name] = ii
	}

	for name, fn := range raw.Functions {
		info.Functions[name] = convertFuncJSON(fn)
	}

	for name, underlying := range raw.TypeDefs {
		info.TypeDefs[name] = &extract.TypeDefInfo{Underlying: underlying}
	}

	return info
}

func convertFuncJSON(fn pyFuncJSON) *extract.FuncInfo {
	fi := &extract.FuncInfo{}
	for _, p := range fn.Params {
		fi.Params = append(fi.Params, extract.ParamInfo{Name: p.Name, Type: p.Type})
	}
	fi.Returns = fn.Returns
	return fi
}

// --- Metadata parsing ---

// ParsePyLDDMeta extracts #ldd: metadata from the text of a .py.lyric file.
func ParsePyLDDMeta(src string) *extract.LDDMeta {
	meta := &extract.LDDMeta{Lang: "python"}
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#ldd:source ") {
			sources := strings.TrimPrefix(line, "#ldd:source ")
			for _, s := range strings.Split(sources, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					meta.Source = append(meta.Source, s)
				}
			}
		} else if strings.HasPrefix(line, "#ldd:why ") {
			meta.Why = strings.Trim(strings.TrimPrefix(line, "#ldd:why "), "\"")
		}
	}
	return meta
}

// --- Parse LDD file ---

// ParsePyLDDFile parses a .py.lyric understanding file and returns both the
// declared API and the LDD metadata.
func ParsePyLDDFile(path string) (*extract.PackageInfo, *extract.LDDMeta, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	meta := ParsePyLDDMeta(string(src))

	// Run the extraction script on the stub file itself — stub files are valid
	// Python stub syntax and ast.parse handles them fine.
	info, err := runExtractScript(path)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return info, meta, nil
}

// --- Generate LDD file ---

// ScanPythonFiles returns the .py filenames in absDir (base names only),
// excluding test files and __init__.py.
func ScanPythonFiles(absDir string) ([]string, error) {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".py") {
			continue
		}
		// Skip test files and __init__.py
		if strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test.py") || name == "__init__.py" {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)
	return files, nil
}

// buildPyFuncSig builds a Python-stub-syntax method signature.
// If recv is non-empty it's the receiver name (e.g. "self") prepended.
func buildPyFuncSig(name string, recv string, fi *extract.FuncInfo) string {
	var b strings.Builder
	b.WriteString("def ")
	b.WriteString(name)
	b.WriteString("(")
	first := true
	if recv != "" {
		b.WriteString(recv)
		first = false
	}
	for _, p := range fi.Params {
		if !first {
			b.WriteString(", ")
		}
		first = false
		b.WriteString(p.Name)
		if p.Type != "" && p.Type != "Any" {
			b.WriteString(": ")
			b.WriteString(p.Type)
		}
	}
	b.WriteString(")")
	if len(fi.Returns) > 0 {
		b.WriteString(" -> ")
		b.WriteString(strings.Join(fi.Returns, ", "))
	}
	b.WriteString(": ...")
	return b.String()
}

// GeneratePyLDDFile produces a .py.lyric understanding file for a Python package.
// Returns the output path and the file content.
func GeneratePyLDDFile(pkgDir string) (outPath, content string, err error) {
	absDir, err := filepath.Abs(pkgDir)
	if err != nil {
		return "", "", err
	}

	pyFiles, err := ScanPythonFiles(absDir)
	if err != nil {
		return "", "", err
	}
	if len(pyFiles) == 0 {
		return "", "", fmt.Errorf("no .py files found in %s", pkgDir)
	}

	// Extract API from all source files, merged.
	merged := extract.NewPackageInfo("")
	for _, f := range pyFiles {
		info, err := runExtractScript(filepath.Join(absDir, f))
		if err != nil {
			return "", "", fmt.Errorf("extracting %s: %w", f, err)
		}
		if merged.Name == "" && info.Name != "" {
			merged.Name = info.Name
		}
		mergePyInfo(merged, info)
	}

	pkgName := merged.Name
	if pkgName == "" {
		pkgName = filepath.Base(absDir)
	}

	// Generate stub content.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("#ldd:source %s\n", strings.Join(pyFiles, ", ")))
	b.WriteString("#ldd:why \"\"\n")

	// Sort names for deterministic output.
	structNames := sortedKeys(merged.Structs)
	ifaceNames := sortedInterfaceKeys(merged.Interfaces)
	funcNames := sortedFuncKeys(merged.Functions)
	typeDefNames := sortedTypeDefKeys(merged.TypeDefs)

	for _, name := range structNames {
		si := merged.Structs[name]
		b.WriteString(fmt.Sprintf("\nclass %s:\n", name))
		fieldNames := sortedStringMapKeys(si.Fields)
		for _, fn := range fieldNames {
			b.WriteString(fmt.Sprintf("    %s: %s\n", fn, si.Fields[fn]))
		}
		methodNames := sortedMethodKeys(si.Methods)
		if len(fieldNames) > 0 && len(methodNames) > 0 {
			b.WriteString("\n")
		}
		for _, mn := range methodNames {
			b.WriteString(fmt.Sprintf("    %s\n", buildPyFuncSig(mn, "self", si.Methods[mn])))
		}
		if len(fieldNames) == 0 && len(methodNames) == 0 {
			b.WriteString("    ...\n")
		}
	}

	for _, name := range ifaceNames {
		ii := merged.Interfaces[name]
		b.WriteString(fmt.Sprintf("\nclass %s(Protocol):\n", name))
		methodNames := sortedMethodKeys(ii.Methods)
		for _, mn := range methodNames {
			b.WriteString(fmt.Sprintf("    %s\n", buildPyFuncSig(mn, "self", ii.Methods[mn])))
		}
		if len(methodNames) == 0 {
			b.WriteString("    ...\n")
		}
	}

	for _, name := range typeDefNames {
		td := merged.TypeDefs[name]
		b.WriteString(fmt.Sprintf("\n%s = %s\n", name, td.Underlying))
	}

	for _, name := range funcNames {
		fi := merged.Functions[name]
		b.WriteString(fmt.Sprintf("\n%s\n", buildPyFuncSig(name, "", fi)))
	}

	b.WriteString("\n# --- index ---\n")
	b.WriteString("# Auto-generated function/method index.\n")
	b.WriteString("# DO NOT EDIT below this line — regenerated by `lyre update`.\n")

	outName := pkgName + ".py.lyric"
	outPath = filepath.Join(absDir, outName)
	return outPath, b.String(), nil
}

// --- Verify ---

// VerifyPyLDD compares a .py.lyric understanding file against the actual Python source.
func VerifyPyLDD(lddPath string) (*VerifyResult, error) {
	declared, meta, err := ParsePyLDDFile(lddPath)
	if err != nil {
		return nil, err
	}
	if len(meta.Source) == 0 {
		return nil, fmt.Errorf("no #ldd:source directive found in %s", lddPath)
	}

	lddDir := filepath.Dir(lddPath)
	actual := extract.NewPackageInfo("")

	result := &VerifyResult{}
	srcStr := strings.Join(meta.Source, ", ")

	for _, srcFile := range meta.Source {
		fullPath := filepath.Join(lddDir, srcFile)
		if _, err := os.Stat(fullPath); err != nil {
			result.add(SevError, lddPath, srcFile, "source file does not exist")
			continue
		}
		info, err := runExtractScript(fullPath)
		if err != nil {
			return nil, fmt.Errorf("extracting %s: %w", srcFile, err)
		}
		mergePyInfo(actual, info)
	}

	// Compare declared vs actual.
	comparePyStructs(declared, actual, lddPath, srcStr, result)
	comparePyInterfaces(declared, actual, lddPath, srcStr, result)
	comparePyFunctions(declared, actual, lddPath, srcStr, result)
	checkPyCompleteness(declared, actual, lddPath, srcStr, result)

	return result, nil
}

func comparePyStructs(declared, actual *extract.PackageInfo, file, src string, r *VerifyResult) {
	for name, ds := range declared.Structs {
		as, ok := actual.Structs[name]
		if !ok {
			r.add(SevError, file, src, fmt.Sprintf("class %s declared in LDD but not found in source", name))
			continue
		}
		// Check fields
		for fn, dt := range ds.Fields {
			at, ok := as.Fields[fn]
			if !ok {
				r.add(SevError, file, src, fmt.Sprintf("class %s: field %s declared in LDD but not in source", name, fn))
				continue
			}
			if normalizeType(dt) != normalizeType(at) {
				r.add(SevError, file, src, fmt.Sprintf("class %s: field %s type mismatch: LDD=%s, source=%s", name, fn, dt, at))
			}
		}
		// Check methods
		for mn := range ds.Methods {
			if _, ok := as.Methods[mn]; !ok {
				r.add(SevError, file, src, fmt.Sprintf("class %s: method %s declared in LDD but not in source", name, mn))
			}
		}
	}
}

func comparePyInterfaces(declared, actual *extract.PackageInfo, file, src string, r *VerifyResult) {
	for name, di := range declared.Interfaces {
		ai, ok := actual.Interfaces[name]
		if !ok {
			r.add(SevError, file, src, fmt.Sprintf("Protocol %s declared in LDD but not found in source", name))
			continue
		}
		for mn := range di.Methods {
			if _, ok := ai.Methods[mn]; !ok {
				r.add(SevError, file, src, fmt.Sprintf("Protocol %s: method %s declared in LDD but not in source", name, mn))
			}
		}
	}
}

func comparePyFunctions(declared, actual *extract.PackageInfo, file, src string, r *VerifyResult) {
	for name := range declared.Functions {
		if _, ok := actual.Functions[name]; !ok {
			r.add(SevError, file, src, fmt.Sprintf("function %s declared in LDD but not found in source", name))
		}
	}
}

func checkPyCompleteness(declared, actual *extract.PackageInfo, file, src string, r *VerifyResult) {
	for name := range actual.Structs {
		if _, ok := declared.Structs[name]; !ok {
			if _, ok := declared.Interfaces[name]; !ok {
				r.add(SevWarning, file, src, fmt.Sprintf("class %s in source is not documented in LDD", name))
			}
		}
	}
	for name := range actual.Interfaces {
		if _, ok := declared.Interfaces[name]; !ok {
			if _, ok := declared.Structs[name]; !ok {
				r.add(SevWarning, file, src, fmt.Sprintf("Protocol %s in source is not documented in LDD", name))
			}
		}
	}
	for name := range actual.Functions {
		if _, ok := declared.Functions[name]; !ok {
			r.add(SevWarning, file, src, fmt.Sprintf("function %s in source is not documented in LDD", name))
		}
	}
}

// normalizeType strips whitespace for comparison.
func normalizeType(t string) string {
	return strings.ReplaceAll(strings.TrimSpace(t), " ", "")
}

// --- Update ---

// UpdatePyLDD refreshes a .py.lyric file by adding any exported symbols from
// source that are not yet documented. Existing declarations are left unchanged.
// Returns a summary of what was added.
func UpdatePyLDD(lddPath string) (added []string, err error) {
	src, err := os.ReadFile(lddPath)
	if err != nil {
		return nil, err
	}
	text := string(src)

	declared, meta, err := ParsePyLDDFile(lddPath)
	if err != nil {
		return nil, fmt.Errorf("parsing LDD file: %w", err)
	}
	if len(meta.Source) == 0 {
		return nil, fmt.Errorf("no #ldd:source directive in %s", lddPath)
	}

	lddDir := filepath.Dir(lddPath)
	actual := extract.NewPackageInfo("")
	for _, srcFile := range meta.Source {
		fullPath := filepath.Join(lddDir, srcFile)
		info, err := runExtractScript(fullPath)
		if err != nil {
			return nil, fmt.Errorf("extracting %s: %w", srcFile, err)
		}
		mergePyInfo(actual, info)
	}

	// Split at index marker.
	humanPart, _ := splitAtIndexMarkerPy(text)

	var newDecls strings.Builder

	// Missing structs.
	var missingStructNames []string
	for name := range actual.Structs {
		if _, ok := declared.Structs[name]; !ok {
			if _, ok := declared.Interfaces[name]; !ok {
				missingStructNames = append(missingStructNames, name)
			}
		}
	}
	sort.Strings(missingStructNames)
	for _, name := range missingStructNames {
		as := actual.Structs[name]
		newDecls.WriteString(fmt.Sprintf("\nclass %s:\n", name))
		fieldNames := sortedStringMapKeys(as.Fields)
		for _, fn := range fieldNames {
			newDecls.WriteString(fmt.Sprintf("    %s: %s\n", fn, as.Fields[fn]))
		}
		methodNames := sortedMethodKeys(as.Methods)
		if len(fieldNames) > 0 && len(methodNames) > 0 {
			newDecls.WriteString("\n")
		}
		for _, mn := range methodNames {
			newDecls.WriteString(fmt.Sprintf("    %s\n", buildPyFuncSig(mn, "self", as.Methods[mn])))
		}
		if len(fieldNames) == 0 && len(methodNames) == 0 {
			newDecls.WriteString("    ...\n")
		}
		added = append(added, "class "+name)
	}

	// Missing interfaces.
	var missingIfaceNames []string
	for name := range actual.Interfaces {
		if _, ok := declared.Interfaces[name]; !ok {
			if _, ok := declared.Structs[name]; !ok {
				missingIfaceNames = append(missingIfaceNames, name)
			}
		}
	}
	sort.Strings(missingIfaceNames)
	for _, name := range missingIfaceNames {
		ai := actual.Interfaces[name]
		newDecls.WriteString(fmt.Sprintf("\nclass %s(Protocol):\n", name))
		methodNames := sortedMethodKeys(ai.Methods)
		for _, mn := range methodNames {
			newDecls.WriteString(fmt.Sprintf("    %s\n", buildPyFuncSig(mn, "self", ai.Methods[mn])))
		}
		if len(methodNames) == 0 {
			newDecls.WriteString("    ...\n")
		}
		added = append(added, "Protocol "+name)
	}

	// Missing type defs.
	var missingTypeNames []string
	for name := range actual.TypeDefs {
		if _, ok := declared.TypeDefs[name]; !ok {
			if _, ok := declared.Structs[name]; !ok {
				if _, ok := declared.Interfaces[name]; !ok {
					missingTypeNames = append(missingTypeNames, name)
				}
			}
		}
	}
	sort.Strings(missingTypeNames)
	for _, name := range missingTypeNames {
		td := actual.TypeDefs[name]
		newDecls.WriteString(fmt.Sprintf("\n%s = %s\n", name, td.Underlying))
		added = append(added, "type "+name)
	}

	// Missing functions.
	var missingFuncNames []string
	for name := range actual.Functions {
		if _, ok := declared.Functions[name]; !ok {
			missingFuncNames = append(missingFuncNames, name)
		}
	}
	sort.Strings(missingFuncNames)
	for _, name := range missingFuncNames {
		fi := actual.Functions[name]
		newDecls.WriteString(fmt.Sprintf("\n%s\n", buildPyFuncSig(name, "", fi)))
		added = append(added, "func "+name)
	}

	// Reconstruct the file.
	result := strings.TrimRight(humanPart, "\n")
	if newDecls.Len() > 0 {
		result += "\n" + newDecls.String()
	}
	result += "\n# --- index ---\n"
	result += "# Auto-generated function/method index.\n"
	result += "# DO NOT EDIT below this line — regenerated by `lyre update`.\n"

	return added, os.WriteFile(lddPath, []byte(result), 0644)
}

// splitAtIndexMarkerPy splits the file content at "# --- index ---".
func splitAtIndexMarkerPy(text string) (human string, rest string) {
	const marker = "\n# --- index ---\n"
	if idx := strings.Index(text, marker); idx >= 0 {
		return text[:idx], text[idx:]
	}
	return text, ""
}

// --- Merge helper ---

func mergePyInfo(dst, src *extract.PackageInfo) {
	for k, v := range src.Structs {
		if existing, ok := dst.Structs[k]; ok {
			for fk, fv := range v.Fields {
				existing.Fields[fk] = fv
			}
			for mk, mv := range v.Methods {
				existing.Methods[mk] = mv
			}
		} else {
			dst.Structs[k] = v
		}
	}
	for k, v := range src.Interfaces {
		dst.Interfaces[k] = v
	}
	for k, v := range src.Functions {
		dst.Functions[k] = v
	}
	for k, v := range src.TypeDefs {
		dst.TypeDefs[k] = v
	}
}

// --- Sorting helpers ---

func sortedKeys(m map[string]*extract.StructInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedInterfaceKeys(m map[string]*extract.InterfaceInfo) []string {
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
