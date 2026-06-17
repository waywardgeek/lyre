// Package typescript provides TypeScript-syntax LDD file generation and parsing.
// A .ts.lyric file uses TypeScript declaration syntax (.d.ts style) with
// //ldd: metadata comments, serving as a living understanding document for a
// TypeScript module.
package typescript

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

//go:embed extract_api.js
var extractScript []byte

// --- Severity / Finding / VerifyResult ---

type Severity int

const (
	SevError   Severity = iota
	SevWarning
	SevInfo
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

type Finding struct {
	Severity Severity
	Message  string
}

func (f Finding) String() string {
	return fmt.Sprintf("[%s] %s", f.Severity, f.Message)
}

type VerifyResult struct {
	Findings []Finding
}

// --- JSON types for extractor output ---

type tsPackageJSON struct {
	Name       string                    `json:"name"`
	Structs    map[string]tsClassJSON    `json:"structs"`
	Interfaces map[string]tsIfaceJSON    `json:"interfaces"`
	Functions  map[string]tsFuncJSON     `json:"functions"`
	TypeDefs   map[string]tsTypeDefJSON  `json:"typedefs"`
}

type tsClassJSON struct {
	Fields  map[string]string         `json:"fields"`
	Methods map[string]tsFuncJSON     `json:"methods"`
	Doc     string                    `json:"doc"`
	File    string                    `json:"file"`
	Line    int                       `json:"line"`
}

type tsIfaceJSON struct {
	Fields  map[string]string         `json:"fields"`
	Methods map[string]tsFuncJSON     `json:"methods"`
	Doc     string                    `json:"doc"`
	File    string                    `json:"file"`
	Line    int                       `json:"line"`
}

type tsFuncJSON struct {
	Params  []tsParamJSON `json:"params"`
	Returns []string      `json:"returns"`
	Doc     string        `json:"doc"`
	File    string        `json:"file"`
	Line    int           `json:"line"`
}

type tsParamJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type tsTypeDefJSON struct {
	Underlying string `json:"underlying"`
	Doc        string `json:"doc"`
	File       string `json:"file"`
	Line       int    `json:"line"`
}

// --- Script execution ---

func runExtractScript(srcPath string) (*extract.PackageInfo, error) {
	tmp, err := os.CreateTemp("", "lyre-extract-*.js")
	if err != nil {
		return nil, fmt.Errorf("creating temp script: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(extractScript); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("writing temp script: %w", err)
	}
	tmp.Close()

	out, err := exec.Command("node", tmp.Name(), srcPath).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("extract_api.js failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("running node: %w", err)
	}

	var raw tsPackageJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing extractor output: %w", err)
	}

	return convertPackageJSON(&raw), nil
}

func convertPackageJSON(raw *tsPackageJSON) *extract.PackageInfo {
	info := extract.NewPackageInfo(raw.Name)

	for name, s := range raw.Structs {
		si := extract.NewStructInfo()
		si.Doc = s.Doc
		si.File = s.File
		si.Line = s.Line
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
		ii.Doc = iface.Doc
		ii.File = iface.File
		ii.Line = iface.Line
		for fn, ft := range iface.Fields {
			ii.Methods[fn] = &extract.FuncInfo{
				Params:  nil,
				Returns: []string{ft},
			}
		}
		for mn, mf := range iface.Methods {
			ii.Methods[mn] = convertFuncJSON(mf)
		}
		info.Interfaces[name] = ii
	}

	for name, fn := range raw.Functions {
		info.Functions[name] = convertFuncJSON(fn)
	}

	for name, td := range raw.TypeDefs {
		info.TypeDefs[name] = &extract.TypeDefInfo{Underlying: td.Underlying, Doc: td.Doc, File: td.File, Line: td.Line}
	}

	return info
}

func convertFuncJSON(f tsFuncJSON) *extract.FuncInfo {
	params := make([]extract.ParamInfo, len(f.Params))
	for i, p := range f.Params {
		params[i] = extract.ParamInfo{Name: p.Name, Type: p.Type}
	}
	returns := f.Returns
	if returns == nil {
		returns = []string{"void"}
	}
	return &extract.FuncInfo{Params: params, Returns: returns, Doc: f.Doc, File: f.File, Line: f.Line}
}

// --- LDD Metadata ---

type TsLDDMeta struct {
	Source []string
	Why    string
	Lang   string
}

func ParseTsLDDMeta(content string) TsLDDMeta {
	meta := TsLDDMeta{Lang: "typescript"}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//ldd:source ") {
			parts := strings.Split(strings.TrimPrefix(line, "//ldd:source "), ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					meta.Source = append(meta.Source, p)
				}
			}
		} else if strings.HasPrefix(line, "//ldd:why ") {
			w := strings.TrimPrefix(line, "//ldd:why ")
			w = strings.Trim(w, `"`)
			meta.Why = w
		}
	}
	return meta
}

// --- Generation ---

// GenerateTsLDDFile creates a new .ts.lyric file from the TypeScript source
// files in the given directory.
func GenerateTsLDDFile(dir string) (outPath string, content string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", fmt.Errorf("reading directory: %w", err)
	}

	merged := extract.NewPackageInfo("")
	var sourceFiles []string

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".ts") {
			continue
		}
		// Skip test files, declaration files, and .ts.lyric files
		if strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".spec.ts") ||
			strings.HasSuffix(name, ".d.ts") || strings.HasSuffix(name, ".ts.lyric") {
			continue
		}

		srcPath := filepath.Join(dir, name)
		info, err := runExtractScript(srcPath)
		if err != nil {
			return "", "", fmt.Errorf("extracting %s: %w", name, err)
		}

		if merged.Name == "" {
			merged.Name = info.Name
		}
		mergeTsInfo(merged, info)
		sourceFiles = append(sourceFiles, name)
	}

	if len(sourceFiles) == 0 {
		return "", "", fmt.Errorf("no .ts source files found in %s", dir)
	}
	sort.Strings(sourceFiles)

	// Use directory name as package name
	pkgName := filepath.Base(dir)
	merged.Name = pkgName

	outPath = filepath.Join(dir, pkgName+".ts.lyric")
	content = renderTsLDD(merged, sourceFiles)
	return outPath, content, nil
}

func renderTsLDD(info *extract.PackageInfo, sourceFiles []string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("//ldd:source %s\n", strings.Join(sourceFiles, ", ")))
	b.WriteString(`//ldd:why ""` + "\n\n")

	// Interfaces
	for _, name := range sortedInterfaceKeys(info.Interfaces) {
		iface := info.Interfaces[name]
		writeTsDoc(&b, iface.Doc)
		writeTsLocation(&b, iface.File, iface.Line)
		b.WriteString(fmt.Sprintf("interface %s {\n", name))
		for _, mn := range sortedFuncKeys(iface.Methods) {
			mf := iface.Methods[mn]
			b.WriteString(fmt.Sprintf("  %s;\n", buildTsFuncSig(mn, "", mf)))
		}
		b.WriteString("}\n\n")
	}

	// Classes
	for _, name := range sortedKeys(info.Structs) {
		cls := info.Structs[name]
		writeTsDoc(&b, cls.Doc)
		writeTsLocation(&b, cls.File, cls.Line)
		b.WriteString(fmt.Sprintf("class %s {\n", name))
		for _, fn := range sortedStringMapKeys(cls.Fields) {
			b.WriteString(fmt.Sprintf("  %s: %s;\n", fn, cls.Fields[fn]))
		}
		for _, mn := range sortedFuncKeys(cls.Methods) {
			mf := cls.Methods[mn]
			b.WriteString(fmt.Sprintf("  %s;\n", buildTsFuncSig(mn, "", mf)))
		}
		b.WriteString("}\n\n")
	}

	// Type aliases
	for _, name := range sortedTypeDefKeys(info.TypeDefs) {
		td := info.TypeDefs[name]
		writeTsDoc(&b, td.Doc)
		writeTsLocation(&b, td.File, td.Line)
		b.WriteString(fmt.Sprintf("type %s = %s;\n", name, td.Underlying))
	}
	if len(info.TypeDefs) > 0 {
		b.WriteString("\n")
	}

	// Functions
	for _, name := range sortedFuncKeys(info.Functions) {
		fn := info.Functions[name]
		writeTsDoc(&b, fn.Doc)
		writeTsLocation(&b, fn.File, fn.Line)
		b.WriteString(fmt.Sprintf("function %s;\n", buildTsFuncSig(name, "", fn)))
	}

	b.WriteString("\n// --- index ---\n")
	b.WriteString("// Auto-generated function/method index.\n")
	b.WriteString("// DO NOT EDIT below this line — regenerated by `lyre update`.\n")

	return b.String()
}

func buildTsFuncSig(name string, _ string, fi *extract.FuncInfo) string {
	var parts []string
	for _, p := range fi.Params {
		parts = append(parts, fmt.Sprintf("%s: %s", p.Name, p.Type))
	}
	ret := "void"
	if len(fi.Returns) > 0 && fi.Returns[0] != "" {
		ret = fi.Returns[0]
	}
	return fmt.Sprintf("%s(%s): %s", name, strings.Join(parts, ", "), ret)
}

// writeTsDoc emits a doc comment using // prefix (first line only for brevity).
func writeTsDoc(b *strings.Builder, doc string) {
	if doc == "" {
		return
	}
	first := strings.SplitN(doc, "\n", 2)[0]
	b.WriteString("// " + first + "\n")
}

// writeTsLocation emits a file:line reference comment.
func writeTsLocation(b *strings.Builder, file string, line int) {
	if file != "" && line > 0 {
		b.WriteString(fmt.Sprintf("// %s:%d\n", file, line))
	}
}

// --- Parsing ---

// ParseTsLDDFile parses a .ts.lyric file and returns the declared API and
// the metadata.
func ParseTsLDDFile(lddPath string) (*extract.PackageInfo, *TsLDDMeta, error) {
	data, err := os.ReadFile(lddPath)
	if err != nil {
		return nil, nil, err
	}
	content := string(data)
	meta := ParseTsLDDMeta(content)

	// Run the extractor on the .ts.lyric file itself (valid TS declarations)
	info, err := runExtractScript(lddPath)
	if err != nil {
		// Fall back: parse manually from the text
		info = parseTsLDDManual(content)
	}
	return info, &meta, nil
}

// parseTsLDDManual is a simple line-based parser for .ts.lyric files that
// extracts names of declared symbols. Used as a fallback when the TS compiler
// cannot parse the stub file.
func parseTsLDDManual(content string) *extract.PackageInfo {
	info := extract.NewPackageInfo("")
	humanPart, _ := splitAtIndexMarkerTs(content)

	for _, line := range strings.Split(humanPart, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") || line == "" || line == "{" || line == "}" {
			continue
		}
		// class Foo {
		if strings.HasPrefix(line, "class ") {
			name := strings.Fields(line)[1]
			name = strings.TrimSuffix(name, "{")
			name = strings.TrimSpace(name)
			if _, ok := info.Structs[name]; !ok {
				info.Structs[name] = extract.NewStructInfo()
			}
		}
		// interface Foo {
		if strings.HasPrefix(line, "interface ") {
			name := strings.Fields(line)[1]
			name = strings.TrimSuffix(name, "{")
			name = strings.TrimSpace(name)
			if _, ok := info.Interfaces[name]; !ok {
				info.Interfaces[name] = extract.NewInterfaceInfo()
			}
		}
		// function foo(...): T;
		if strings.HasPrefix(line, "function ") {
			rest := strings.TrimPrefix(line, "function ")
			if idx := strings.Index(rest, "("); idx > 0 {
				name := rest[:idx]
				info.Functions[name] = &extract.FuncInfo{}
			}
		}
		// type Foo = ...;
		if strings.HasPrefix(line, "type ") {
			rest := strings.TrimPrefix(line, "type ")
			if idx := strings.Index(rest, "="); idx > 0 {
				name := strings.TrimSpace(rest[:idx])
				underlying := strings.TrimSpace(rest[idx+1:])
				underlying = strings.TrimSuffix(underlying, ";")
				info.TypeDefs[name] = &extract.TypeDefInfo{Underlying: underlying}
			}
		}
	}
	return info
}

// --- Verification ---

func VerifyTsLDD(lddPath string) (*VerifyResult, error) {
	result := &VerifyResult{}

	declared, meta, err := ParseTsLDDFile(lddPath)
	if err != nil {
		return nil, fmt.Errorf("parsing LDD file: %w", err)
	}

	if len(meta.Source) == 0 {
		result.Findings = append(result.Findings, Finding{SevError, fmt.Sprintf("%s: missing //ldd:source directive", lddPath)})
		return result, nil
	}

	dir := filepath.Dir(lddPath)
	actual := extract.NewPackageInfo("")
	for _, src := range meta.Source {
		srcPath := filepath.Join(dir, src)
		info, err := runExtractScript(srcPath)
		if err != nil {
			result.Findings = append(result.Findings, Finding{SevError, fmt.Sprintf("%s: %v", src, err)})
			continue
		}
		mergeTsInfo(actual, info)
	}

	// Check for undocumented symbols
	for name := range actual.Structs {
		if _, ok := declared.Structs[name]; !ok {
			result.Findings = append(result.Findings, Finding{SevWarning, fmt.Sprintf("undocumented class: %s", name)})
		}
	}
	for name := range actual.Interfaces {
		if _, ok := declared.Interfaces[name]; !ok {
			result.Findings = append(result.Findings, Finding{SevWarning, fmt.Sprintf("undocumented interface: %s", name)})
		}
	}
	for name := range actual.Functions {
		if _, ok := declared.Functions[name]; !ok {
			result.Findings = append(result.Findings, Finding{SevWarning, fmt.Sprintf("undocumented function: %s", name)})
		}
	}
	for name := range actual.TypeDefs {
		if _, ok := declared.TypeDefs[name]; !ok {
			if _, ok := declared.Structs[name]; !ok {
				if _, ok := declared.Interfaces[name]; !ok {
					result.Findings = append(result.Findings, Finding{SevWarning, fmt.Sprintf("undocumented type: %s", name)})
				}
			}
		}
	}

	// Check for stale symbols
	for name := range declared.Structs {
		if _, ok := actual.Structs[name]; !ok {
			result.Findings = append(result.Findings, Finding{SevWarning, fmt.Sprintf("stale class (removed from source): %s", name)})
		}
	}
	for name := range declared.Interfaces {
		if _, ok := actual.Interfaces[name]; !ok {
			result.Findings = append(result.Findings, Finding{SevWarning, fmt.Sprintf("stale interface (removed from source): %s", name)})
		}
	}
	for name := range declared.Functions {
		if _, ok := actual.Functions[name]; !ok {
			result.Findings = append(result.Findings, Finding{SevWarning, fmt.Sprintf("stale function (removed from source): %s", name)})
		}
	}
	for name := range declared.TypeDefs {
		if _, ok := actual.TypeDefs[name]; !ok {
			result.Findings = append(result.Findings, Finding{SevWarning, fmt.Sprintf("stale type (removed from source): %s", name)})
		}
	}

	return result, nil
}

// --- Update ---

func UpdateTsLDD(lddPath string) (added []string, err error) {
	_, meta, err := ParseTsLDDFile(lddPath)
	if err != nil {
		return nil, err
	}

	if len(meta.Source) == 0 {
		return nil, fmt.Errorf("no //ldd:source directive found")
	}

	dir := filepath.Dir(lddPath)
	actual := extract.NewPackageInfo("")
	for _, src := range meta.Source {
		srcPath := filepath.Join(dir, src)
		info, err := runExtractScript(srcPath)
		if err != nil {
			return nil, fmt.Errorf("extracting %s: %w", src, err)
		}
		mergeTsInfo(actual, info)
	}

	data, err := os.ReadFile(lddPath)
	if err != nil {
		return nil, err
	}
	content := string(data)
	humanPart, _ := splitAtIndexMarkerTs(content)

	declared := parseTsLDDManual(content)

	var newDecls strings.Builder

	// Missing interfaces
	var missingIfaceNames []string
	for name := range actual.Interfaces {
		if _, ok := declared.Interfaces[name]; !ok {
			missingIfaceNames = append(missingIfaceNames, name)
		}
	}
	sort.Strings(missingIfaceNames)
	for _, name := range missingIfaceNames {
		iface := actual.Interfaces[name]
		newDecls.WriteString(fmt.Sprintf("\ninterface %s {\n", name))
		for _, mn := range sortedFuncKeys(iface.Methods) {
			mf := iface.Methods[mn]
			newDecls.WriteString(fmt.Sprintf("  %s;\n", buildTsFuncSig(mn, "", mf)))
		}
		newDecls.WriteString("}\n")
		added = append(added, "interface "+name)
	}

	// Missing classes
	var missingClassNames []string
	for name := range actual.Structs {
		if _, ok := declared.Structs[name]; !ok {
			missingClassNames = append(missingClassNames, name)
		}
	}
	sort.Strings(missingClassNames)
	for _, name := range missingClassNames {
		cls := actual.Structs[name]
		newDecls.WriteString(fmt.Sprintf("\nclass %s {\n", name))
		for _, fn := range sortedStringMapKeys(cls.Fields) {
			newDecls.WriteString(fmt.Sprintf("  %s: %s;\n", fn, cls.Fields[fn]))
		}
		for _, mn := range sortedFuncKeys(cls.Methods) {
			mf := cls.Methods[mn]
			newDecls.WriteString(fmt.Sprintf("  %s;\n", buildTsFuncSig(mn, "", mf)))
		}
		newDecls.WriteString("}\n")
		added = append(added, "class "+name)
	}

	// Missing type defs
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
		newDecls.WriteString(fmt.Sprintf("\ntype %s = %s;\n", name, td.Underlying))
		added = append(added, "type "+name)
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
		newDecls.WriteString(fmt.Sprintf("\nfunction %s;\n", buildTsFuncSig(name, "", fi)))
		added = append(added, "func "+name)
	}

	// Reconstruct the file
	resultStr := strings.TrimRight(humanPart, "\n")
	if newDecls.Len() > 0 {
		resultStr += "\n" + newDecls.String()
	}
	resultStr += "\n// --- index ---\n"
	resultStr += "// Auto-generated function/method index.\n"
	resultStr += "// DO NOT EDIT below this line — regenerated by `lyre update`.\n"

	return added, os.WriteFile(lddPath, []byte(resultStr), 0644)
}

func splitAtIndexMarkerTs(text string) (human string, rest string) {
	const marker = "\n// --- index ---\n"
	if idx := strings.Index(text, marker); idx >= 0 {
		return text[:idx], text[idx:]
	}
	return text, ""
}

// --- Merge helper ---

func mergeTsInfo(dst, src *extract.PackageInfo) {
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

func sortedStringMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
