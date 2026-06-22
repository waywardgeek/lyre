// Package python's `.py.lyric` v2 entry points: ExtractPy, GeneratePy,
// UpdatePy, VerifyPy. The `.py.lyric` file is the persistent CDD artifact
// for a Python directory — a small declarative DSL parsed/written by
// pkg/cdd, whose payload lines (field types, method signatures, function
// signatures) are verbatim Python text treated as opaque strings.
//
// Architectural principle (rich-doc upgrade plan, top): CDD documentation
// lives in the .lyric file ONLY, never as `#ldd:source`, `#ldd:why`, or
// other smuggled directives in Python source. Extractors are
// signatures-only. The legacy ParsePyLDDFile / // --- index --- layout
// and #ldd: scraping are gone.
//
// Extraction pipeline:
//   - The embedded extract_api.py script (Python 3.8+ stdlib only,
//     written to a tempfile) parses one .py file per invocation and
//     emits a single JSON blob.
//   - This package walks the directory, invokes the script per-file,
//     merges results into a *PackageInfo whose SignatureText fields are
//     populated for round-trip through the .lyric v2 format.
//   - Field SignatureText is type-only (e.g. "int", "list[str]",
//     "Optional[Token]"). Method and function SignatureText is
//     "Name(self, p: T) -> R" for methods, "Name(p: T) -> R" for
//     top-level — no `def` keyword, no decorators, no trailing colon.
//     Methods always include `self` as the leading parameter; the
//     extract_api.py script strips self/cls from the params list, so
//     we re-add it here (mirroring Phase 3c's Lyric handling).
//   - All Python "classes" become extract.StructInfo with IsClass=true
//     — Python has no value-vs-reference type distinction. Classes
//     deriving from `Protocol` are classified as interfaces.
//
// Notably KEEP the //go:embed extract_api.py + temp-file approach
// (rather than Phase 3b's runtime.Caller switch). TS needed runtime
// path because npm wants a sibling node_modules; Python's script is
// pure stdlib and has no such constraint, so the embed keeps the
// binary self-contained.
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
	"github.com/waywardgeek/lyre/pkg/cdd"
)

//go:embed extract_api.py
var extractScript []byte

// --- VerifyResult / Finding / Severity ------------------------------------

// Severity levels for verification findings.
type Severity int

const (
	SevError Severity = iota
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

// Finding is a single verification report.
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

// ErrorCount returns the number of SevError findings.
func (r *VerifyResult) ErrorCount() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == SevError {
			n++
		}
	}
	return n
}

// --- JSON shape from extract_api.py ---------------------------------------

type pyPackageJSON struct {
	Name       string                   `json:"name"`
	Structs    map[string]pyStructJSON  `json:"structs"`
	Interfaces map[string]pyIfaceJSON   `json:"interfaces"`
	Functions  map[string]pyFuncJSON    `json:"functions"`
	TypeDefs   map[string]pyTypeDefJSON `json:"typedefs"`
}

type pyStructJSON struct {
	Fields  map[string]string     `json:"fields"`
	Methods map[string]pyFuncJSON `json:"methods"`
	Doc     string                `json:"doc"` // ignored — CDD doc lives in .lyric only
	File    string                `json:"file"`
	Line    int                   `json:"line"`
}

type pyIfaceJSON struct {
	Methods map[string]pyFuncJSON `json:"methods"`
	Doc     string                `json:"doc"` // ignored
	File    string                `json:"file"`
	Line    int                   `json:"line"`
}

type pyFuncJSON struct {
	Params  []pyParamJSON `json:"params"`
	Returns []string      `json:"returns"`
	Doc     string        `json:"doc"` // ignored
	File    string        `json:"file"`
	Line    int           `json:"line"`
}

type pyParamJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type pyTypeDefJSON struct {
	Underlying string `json:"underlying"`
	File       string `json:"file"`
	Line       int    `json:"line"`
}

// --- Script invocation ----------------------------------------------------

// writeScriptTemp writes the embedded extract_api.py to a tempfile and
// returns its path. Caller removes it.
func writeScriptTemp() (string, error) {
	tmp, err := os.CreateTemp("", "lyre-extract-*.py")
	if err != nil {
		return "", fmt.Errorf("creating temp script: %w", err)
	}
	if _, err := tmp.Write(extractScript); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("writing temp script: %w", err)
	}
	tmp.Close()
	return tmp.Name(), nil
}

// runExtract invokes extract_api.py against a single .py source file and
// returns the decoded JSON. The extractor accepts ONE file per invocation
// (unlike Lyric's batch mode), so callers loop and merge.
func runExtract(scriptPath, srcPath string) (*pyPackageJSON, error) {
	out, err := exec.Command("python3", scriptPath, srcPath).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("extract_api.py failed on %s: %s", srcPath, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("running python3 on %s: %w", srcPath, err)
	}
	var raw pyPackageJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		preview := out
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, fmt.Errorf("parsing extractor output for %s: %w\nraw: %s", srcPath, err, string(preview))
	}
	return &raw, nil
}

// --- ExtractPy ------------------------------------------------------------

// ExtractPy parses every public .py file in srcDir and returns the public
// API as a *PackageInfo whose SignatureText fields are populated for
// round-trip through the .lyric v2 format.
//
// Skips: test_*.py, *_test.py, _*.py (underscore-prefixed private convention,
// which also covers __init__.py / __main__.py).
//
// Module name = dir basename, matching Phase 3a/3b/3c precedent.
func ExtractPy(srcDir string) (*extract.PackageInfo, error) {
	absDir, err := filepath.Abs(srcDir)
	if err != nil {
		return nil, err
	}
	files, err := scanPyFiles(absDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .py files found in %s", srcDir)
	}

	scriptPath, err := writeScriptTemp()
	if err != nil {
		return nil, err
	}
	defer os.Remove(scriptPath)

	p := extract.NewPackageInfo(filepath.Base(absDir))
	p.ModuleSource = files
	for _, f := range files {
		raw, err := runExtract(scriptPath, filepath.Join(absDir, f))
		if err != nil {
			return nil, err
		}
		mergeJSONInto(p, raw)
	}
	return p, nil
}

// scanPyFiles returns sorted non-test, non-private .py filenames.
func scanPyFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".py") {
			continue
		}
		// Skip private (underscore-prefixed: covers __init__.py, __main__.py, _internal.py)
		if strings.HasPrefix(name, "_") {
			continue
		}
		// Skip tests
		if strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test.py") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// mergeJSONInto folds one file's JSON into the accumulating PackageInfo.
// Per-file JSON has its own pkg "name" field (file basename without .py);
// we keep the PackageInfo.Name set by the caller (directory basename).
func mergeJSONInto(p *extract.PackageInfo, raw *pyPackageJSON) {
	for name, cls := range raw.Structs {
		si := extract.NewStructInfo()
		si.IsClass = true // Python has no value-vs-reference distinction
		si.File = filepath.Base(cls.File)
		si.Line = cls.Line
		if si.File != "" && si.Line > 0 {
			si.Source = fmt.Sprintf("%s:%d", si.File, si.Line)
		}
		for _, fname := range sortedStringMapKeys(cls.Fields) {
			si.SetField(fname, cls.Fields[fname])
		}
		for _, mname := range sortedFuncJSONKeys(cls.Methods) {
			mf := cls.Methods[mname]
			si.Methods[mname] = funcInfoFromJSON(mname, mf, true)
		}
		p.Structs[name] = si
	}

	for name, iface := range raw.Interfaces {
		ii := extract.NewInterfaceInfo()
		ii.File = filepath.Base(iface.File)
		ii.Line = iface.Line
		if ii.File != "" && ii.Line > 0 {
			ii.Source = fmt.Sprintf("%s:%d", ii.File, ii.Line)
		}
		for _, mname := range sortedFuncJSONKeys(iface.Methods) {
			mf := iface.Methods[mname]
			ii.Methods[mname] = funcInfoFromJSON(mname, mf, true)
		}
		p.Interfaces[name] = ii
	}

	for name, fn := range raw.Functions {
		p.Functions[name] = funcInfoFromJSON(name, fn, false)
	}

	for name, td := range raw.TypeDefs {
		ti := &extract.TypeDefInfo{
			Underlying: td.Underlying,
			File:       filepath.Base(td.File),
			Line:       td.Line,
		}
		if ti.File != "" && ti.Line > 0 {
			ti.Source = fmt.Sprintf("%s:%d", ti.File, ti.Line)
		}
		p.TypeDefs[name] = ti
	}
}

// funcInfoFromJSON builds a *FuncInfo with canonical Python SignatureText.
// Method form: "Name(self, p: T) -> R". Function form: "Name(p: T) -> R".
// Empty return list omits the `-> R` clause.
//
// extract_api.py strips self/cls from the params list (see its func_info()
// implementation, which starts at index 1 when the first arg is self/cls).
// We re-add `self` for methods to match the Lyric/extract_api convention.
func funcInfoFromJSON(name string, fn pyFuncJSON, isMethod bool) *extract.FuncInfo {
	fi := &extract.FuncInfo{
		SignatureText: pyFuncSigText(name, fn, isMethod),
		File:          filepath.Base(fn.File),
		Line:          fn.Line,
	}
	if fi.File != "" && fi.Line > 0 {
		fi.Source = fmt.Sprintf("%s:%d", fi.File, fi.Line)
	}
	return fi
}

func pyFuncSigText(name string, fn pyFuncJSON, isMethod bool) string {
	parts := make([]string, 0, len(fn.Params)+1)
	if isMethod {
		parts = append(parts, "self")
	}
	for _, p := range fn.Params {
		var b strings.Builder
		b.WriteString(p.Name)
		if p.Type != "" {
			b.WriteString(": ")
			b.WriteString(p.Type)
		}
		parts = append(parts, b.String())
	}
	sig := fmt.Sprintf("%s(%s)", name, strings.Join(parts, ", "))
	if len(fn.Returns) == 0 || (len(fn.Returns) == 1 && fn.Returns[0] == "") {
		return sig
	}
	if len(fn.Returns) == 1 {
		return sig + " -> " + fn.Returns[0]
	}
	return sig + " -> (" + strings.Join(fn.Returns, ", ") + ")"
}

// --- GeneratePy / UpdatePy / VerifyPy -------------------------------------

// GeneratePy scaffolds a fresh <dirname>.py.lyric file for srcDir. It does
// NOT write to disk; the caller writes outPath only if it doesn't exist.
func GeneratePy(srcDir string) (outPath, content string, err error) {
	p, err := ExtractPy(srcDir)
	if err != nil {
		return "", "", err
	}
	absDir, err := filepath.Abs(srcDir)
	if err != nil {
		return "", "", err
	}
	outPath = filepath.Join(absDir, p.Name+".py.lyric")
	content = cdd.Write(p)
	return outPath, content, nil
}

// UpdatePy refreshes lyricPath: re-extracts signatures/positions from
// Python source, adds new exports, preserves all human prose (ModuleWhy,
// Docs, Invariants, per-decl Why, per-field Doc). Source list refreshed.
func UpdatePy(lyricPath string) (added []string, err error) {
	raw, err := os.ReadFile(lyricPath)
	if err != nil {
		return nil, err
	}
	existing, err := cdd.Parse(string(raw), lyricPath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", lyricPath, err)
	}

	srcDir := filepath.Dir(lyricPath)
	fresh, err := ExtractPy(srcDir)
	if err != nil {
		return nil, err
	}

	added = mergeFreshIntoExisting(existing, fresh)

	out := cdd.Write(existing)
	if err := os.WriteFile(lyricPath, []byte(out), 0644); err != nil {
		return nil, err
	}
	return added, nil
}

// VerifyPy parses lyricPath as a .py.lyric v2 file, re-extracts the
// Python source it lives next to, and reports drift via Findings.
func VerifyPy(lyricPath string) (*VerifyResult, error) {
	raw, err := os.ReadFile(lyricPath)
	if err != nil {
		return nil, err
	}
	declared, err := cdd.Parse(string(raw), lyricPath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", lyricPath, err)
	}

	srcDir := filepath.Dir(lyricPath)
	actual, err := ExtractPy(srcDir)
	if err != nil {
		return nil, err
	}

	result := &VerifyResult{}
	srcStr := strings.Join(actual.ModuleSource, ", ")
	compareStructs(declared, actual, lyricPath, srcStr, result)
	compareInterfaces(declared, actual, lyricPath, srcStr, result)
	compareFunctions(declared, actual, lyricPath, srcStr, result)
	compareTypeDefs(declared, actual, lyricPath, srcStr, result)
	checkCompleteness(declared, actual, lyricPath, srcStr, result)
	return result, nil
}

// --- merge ----------------------------------------------------------------

// mergeFreshIntoExisting refreshes signatures/positions on existing decls
// from `fresh`, adds new exports, and refreshes the module source list.
// All human prose on existing decls is preserved.
//
// NOT pruned: decls present in existing but absent from fresh remain in
// the file (VerifyPy reports them as drift).
func mergeFreshIntoExisting(existing, fresh *extract.PackageInfo) []string {
	var added []string

	existing.ModuleSource = fresh.ModuleSource

	for _, name := range sortedKeys(fresh.Structs) {
		fs := fresh.Structs[name]
		es, ok := existing.Structs[name]
		if !ok {
			existing.Structs[name] = fs
			added = append(added, "class "+name)
			continue
		}
		es.File, es.Line, es.Source = fs.File, fs.Line, fs.Source
		es.IsClass = fs.IsClass
		preservedDoc := map[string]string{}
		for _, f := range es.Fields {
			if f.Doc != "" {
				preservedDoc[f.Name] = f.Doc
			}
		}
		es.Fields = es.Fields[:0]
		for _, f := range fs.Fields {
			ff := f
			if doc, ok := preservedDoc[f.Name]; ok {
				ff.Doc = doc
			}
			es.Fields = append(es.Fields, ff)
		}
		for mn, fm := range fs.Methods {
			if em, ok := es.Methods[mn]; ok {
				em.SignatureText = fm.SignatureText
				em.File, em.Line, em.Source = fm.File, fm.Line, fm.Source
			} else {
				es.Methods[mn] = fm
				added = append(added, fmt.Sprintf("method %s.%s", name, mn))
			}
		}
	}

	for _, name := range sortedKeys(fresh.Interfaces) {
		fi := fresh.Interfaces[name]
		ei, ok := existing.Interfaces[name]
		if !ok {
			existing.Interfaces[name] = fi
			added = append(added, "interface "+name)
			continue
		}
		ei.File, ei.Line, ei.Source = fi.File, fi.Line, fi.Source
		for mn, fm := range fi.Methods {
			if em, ok := ei.Methods[mn]; ok {
				em.SignatureText = fm.SignatureText
				em.File, em.Line, em.Source = fm.File, fm.Line, fm.Source
			} else {
				ei.Methods[mn] = fm
				added = append(added, fmt.Sprintf("interface %s.%s", name, mn))
			}
		}
	}

	for _, name := range sortedKeys(fresh.Functions) {
		ff := fresh.Functions[name]
		ef, ok := existing.Functions[name]
		if !ok {
			existing.Functions[name] = ff
			added = append(added, "function "+name)
			continue
		}
		ef.SignatureText = ff.SignatureText
		ef.File, ef.Line, ef.Source = ff.File, ff.Line, ff.Source
	}

	for _, name := range sortedKeys(fresh.TypeDefs) {
		ft := fresh.TypeDefs[name]
		et, ok := existing.TypeDefs[name]
		if !ok {
			existing.TypeDefs[name] = ft
			added = append(added, "type "+name)
			continue
		}
		et.Underlying = ft.Underlying
		et.File, et.Line, et.Source = ft.File, ft.Line, ft.Source
	}

	sort.Strings(added)
	return added
}

// --- verify comparison helpers --------------------------------------------

func compareStructs(declared, actual *extract.PackageInfo, file, srcStr string, result *VerifyResult) {
	for _, name := range sortedKeys(declared.Structs) {
		ds := declared.Structs[name]
		as, ok := actual.Structs[name]
		if !ok {
			result.add(SevError, file, srcStr, fmt.Sprintf("class %s declared in .lyric but not found in source", name))
			continue
		}
		for _, df := range ds.Fields {
			actualType, ok := as.FieldSig(df.Name)
			if !ok {
				result.add(SevError, file, srcStr, fmt.Sprintf("%s: field %s not found in source", name, df.Name))
				continue
			}
			if !typesMatch(df.SignatureText, actualType) {
				result.add(SevError, file, srcStr, fmt.Sprintf("%s: field %s type mismatch: .lyric=%s, source=%s", name, df.Name, df.SignatureText, actualType))
			}
		}
		var extras []string
		for _, af := range as.Fields {
			if !ds.HasField(af.Name) {
				extras = append(extras, af.Name)
			}
		}
		sort.Strings(extras)
		for _, extra := range extras {
			result.add(SevWarning, file, srcStr, fmt.Sprintf("%s: source has field %s not in .lyric", name, extra))
		}
		for mn, dm := range ds.Methods {
			am, ok := as.Methods[mn]
			if !ok {
				result.add(SevError, file, srcStr, fmt.Sprintf("%s: method %s not found in source", name, mn))
				continue
			}
			if !sigMatch(dm.SignatureText, am.SignatureText) {
				result.add(SevError, file, srcStr, fmt.Sprintf("%s: method %s signature mismatch: .lyric=%q, source=%q", name, mn, dm.SignatureText, am.SignatureText))
			}
		}
	}
}

func compareInterfaces(declared, actual *extract.PackageInfo, file, srcStr string, result *VerifyResult) {
	for _, name := range sortedKeys(declared.Interfaces) {
		di := declared.Interfaces[name]
		ai, ok := actual.Interfaces[name]
		if !ok {
			result.add(SevError, file, srcStr, fmt.Sprintf("interface %s declared in .lyric but not found in source", name))
			continue
		}
		for mn, dm := range di.Methods {
			am, ok := ai.Methods[mn]
			if !ok {
				result.add(SevError, file, srcStr, fmt.Sprintf("interface %s: method %s not found in source", name, mn))
				continue
			}
			if !sigMatch(dm.SignatureText, am.SignatureText) {
				result.add(SevError, file, srcStr, fmt.Sprintf("interface %s: method %s signature mismatch: .lyric=%q, source=%q", name, mn, dm.SignatureText, am.SignatureText))
			}
		}
	}
}

func compareFunctions(declared, actual *extract.PackageInfo, file, srcStr string, result *VerifyResult) {
	for _, name := range sortedKeys(declared.Functions) {
		df := declared.Functions[name]
		af, ok := actual.Functions[name]
		if !ok {
			result.add(SevError, file, srcStr, fmt.Sprintf("function %s declared in .lyric but not found in source", name))
			continue
		}
		if !sigMatch(df.SignatureText, af.SignatureText) {
			result.add(SevError, file, srcStr, fmt.Sprintf("function %s signature mismatch: .lyric=%q, source=%q", name, df.SignatureText, af.SignatureText))
		}
	}
}

func compareTypeDefs(declared, actual *extract.PackageInfo, file, srcStr string, result *VerifyResult) {
	for _, name := range sortedKeys(declared.TypeDefs) {
		dt := declared.TypeDefs[name]
		at, ok := actual.TypeDefs[name]
		if !ok {
			result.add(SevError, file, srcStr, fmt.Sprintf("type %s declared in .lyric but not found in source", name))
			continue
		}
		if !typesMatch(dt.Underlying, at.Underlying) {
			result.add(SevError, file, srcStr, fmt.Sprintf("type %s underlying mismatch: .lyric=%s, source=%s", name, dt.Underlying, at.Underlying))
		}
	}
}

func checkCompleteness(declared, actual *extract.PackageInfo, file, srcStr string, result *VerifyResult) {
	declaredNames := map[string]bool{}
	for name := range declared.Structs {
		declaredNames[name] = true
	}
	for name := range declared.Interfaces {
		declaredNames[name] = true
	}
	for name := range declared.Functions {
		declaredNames[name] = true
	}
	for name := range declared.TypeDefs {
		declaredNames[name] = true
	}

	var missing []string
	for name := range actual.Structs {
		if !declaredNames[name] {
			missing = append(missing, "class "+name)
		}
	}
	for name := range actual.Interfaces {
		if !declaredNames[name] {
			missing = append(missing, "interface "+name)
		}
	}
	for name := range actual.TypeDefs {
		if !declaredNames[name] {
			missing = append(missing, "type "+name)
		}
	}
	for name := range actual.Functions {
		if !declaredNames[name] {
			missing = append(missing, "function "+name)
		}
	}
	sort.Strings(missing)
	for _, kind := range missing {
		result.add(SevError, file, srcStr, fmt.Sprintf("exported %s not documented in .lyric", kind))
	}
}

// --- signature normalization ----------------------------------------------

func sigMatch(a, b string) bool {
	return normalizeSig(a) == normalizeSig(b)
}

func normalizeSig(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	inSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		// \n and \r included so multi-line type forms compare equal to the
		// writer's flattened form (pkg/cdd/writer.go:flattenSig).
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
		} else {
			b.WriteByte(c)
			inSpace = false
		}
	}
	return b.String()
}

func typesMatch(a, b string) bool {
	return sigMatch(a, b)
}

// --- sort helpers ---------------------------------------------------------

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedStringMapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedFuncJSONKeys(m map[string]pyFuncJSON) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
