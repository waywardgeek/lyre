// Package typescript's `.ts.lyric` v2 entry points: ExtractTs, GenerateTs,
// UpdateTs, VerifyTs. The `.ts.lyric` file is the persistent CDD artifact
// for a TypeScript directory — a small declarative DSL parsed/written by
// pkg/cdd, whose payload lines (field types, method signatures, function
// signatures) are verbatim TypeScript text treated as opaque strings.
//
// Architectural principle (rich-doc upgrade plan, top): CDD documentation
// lives in the .lyric file ONLY, never as `// why:` or `/** doc */` smuggled
// directives in TS source. Extractors are signatures-only. The legacy
// //ldd:source, //ldd:why, and the parseTsLDDManual / index-marker layout
// are gone.
//
// Extraction pipeline:
//   - extract_api.js (on-disk in this package) shells out to the TypeScript
//     Compiler API to parse a single .ts file. Returns JSON.
//   - This package converts the JSON into an extract.PackageInfo with
//     SignatureText fields populated for round-trip through the .lyric v2
//     format.
//   - Field SignatureText is type-only (e.g. "number", "string[]"). Method
//     and function SignatureText is "Name(p1: t1, p2: t2): retType" — no
//     `function` keyword, no trailing semicolon, no receiver clause.
package typescript

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/waywardgeek/lyre/pkg/cdd"
	"github.com/waywardgeek/lyre/pkg/extract"
)

// packageDir is the on-disk directory of this Go package, used to locate
// extract_api.js and node_modules/ at runtime. Resolved via runtime.Caller
// at init time.
var packageDir = func() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}()

// --- VerifyResult / Finding / Severity ------------------------------------

// VerifyResult holds all findings from a verification run.
type VerifyResult struct {
	Findings []Finding
}

// Severity levels.
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

// --- JSON shape from extract_api.js ---------------------------------------

type tsPackageJSON struct {
	Name       string                   `json:"name"`
	Structs    map[string]tsClassJSON   `json:"structs"`
	Interfaces map[string]tsIfaceJSON   `json:"interfaces"`
	Functions  map[string]tsFuncJSON    `json:"functions"`
	TypeDefs   map[string]tsTypeDefJSON `json:"typedefs"`
}

type tsClassJSON struct {
	Fields  map[string]string     `json:"fields"`
	Methods map[string]tsFuncJSON `json:"methods"`
	Doc     string                `json:"doc"`
	File    string                `json:"file"`
	Line    int                   `json:"line"`
}

type tsIfaceJSON struct {
	Fields  map[string]string     `json:"fields"`
	Methods map[string]tsFuncJSON `json:"methods"`
	Doc     string                `json:"doc"`
	File    string                `json:"file"`
	Line    int                   `json:"line"`
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

// --- runExtractScript -----------------------------------------------------

// runExtractScript runs the on-disk extract_api.js against srcPath and
// returns the decoded JSON. NODE_PATH is set so that require('typescript')
// resolves the vendored module sibling to this package's node_modules.
//
// If node_modules is missing (e.g. fresh checkout), runs `npm install` once
// to populate it. This keeps the dev/test UX one-step. Production deploy
// is expected to provide node_modules out-of-band.
func runExtractScript(srcPath string) (*tsPackageJSON, error) {
	scriptPath := filepath.Join(packageDir, "extract_api.js")
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, fmt.Errorf("extract_api.js not found at %s: %w", scriptPath, err)
	}
	nodeModules := filepath.Join(packageDir, "node_modules")
	if _, err := os.Stat(nodeModules); err != nil {
		if err := ensureNodeModules(); err != nil {
			return nil, err
		}
	}
	cmd := exec.Command("node", scriptPath, srcPath)
	cmd.Env = append(os.Environ(), "NODE_PATH="+nodeModules)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("extract_api.js failed: %s",
				strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("running node: %w", err)
	}
	var raw tsPackageJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing extractor output: %w", err)
	}
	return &raw, nil
}

// ensureNodeModules runs `npm install` in the package directory. Used on
// fresh checkouts where node_modules hasn't been populated yet.
func ensureNodeModules() error {
	cmd := exec.Command("npm", "install", "--silent", "--no-progress", "--no-audit", "--no-fund")
	cmd.Dir = packageDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("npm install in %s failed: %w\n%s", packageDir, err, out)
	}
	return nil
}

// --- ExtractTs ------------------------------------------------------------

// ExtractTs parses every non-test, non-declaration .ts file in srcDir and
// returns the public API as a *PackageInfo whose SignatureText fields are
// populated for round-trip through the .lyric v2 format.
//
// Skips: .test.ts, .spec.ts, .d.ts, .ts.lyric, and files starting with `_`.
// Only exported declarations are kept (the TS extractor enforces this).
func ExtractTs(srcDir string) (*extract.PackageInfo, error) {
	absDir, err := filepath.Abs(srcDir)
	if err != nil {
		return nil, err
	}
	files, err := scanTsFiles(absDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .ts files found in %s", srcDir)
	}

	p := extract.NewPackageInfo(extract.SanitizeModuleName(filepath.Base(absDir)))
	p.ModuleSource = files

	for _, name := range files {
		raw, err := runExtractScript(filepath.Join(absDir, name))
		if err != nil {
			return nil, fmt.Errorf("extracting %s: %w", name, err)
		}
		mergeJSONInto(p, raw)
	}
	extract.SeedWhyFromDoc(p)
	return p, nil
}

// scanTsFiles returns the sorted list of non-test, non-declaration, non-
// underscore-prefixed .ts and .tsx files in dir (basenames only).
//
// .tsx is accepted because the TypeScript compiler (via extract_api.js)
// handles JSX natively when the source file uses the .tsx extension — no
// extractor changes needed beyond admitting the extension here and in
// detectDirLanguage.
func scanTsFiles(dir string) ([]string, error) {
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
		isTS := strings.HasSuffix(name, ".ts")
		isTSX := strings.HasSuffix(name, ".tsx")
		if !isTS && !isTSX {
			continue
		}
		// Test / declaration / generated artifact filters.
		if strings.HasSuffix(name, ".test.ts") ||
			strings.HasSuffix(name, ".test.tsx") ||
			strings.HasSuffix(name, ".spec.ts") ||
			strings.HasSuffix(name, ".spec.tsx") ||
			strings.HasSuffix(name, ".d.ts") ||
			strings.HasSuffix(name, ".ts.lyric") ||
			strings.HasSuffix(name, ".tsx.lyric") {
			continue
		}
		if strings.HasPrefix(name, "_") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// mergeJSONInto folds one source file's JSON into the accumulating
// PackageInfo, converting each decl to its in-memory shape with the
// canonical SignatureText forms.
func mergeJSONInto(p *extract.PackageInfo, raw *tsPackageJSON) {
	for name, cls := range raw.Structs {
		si := extract.NewStructInfo()
		si.IsClass = true
		si.Doc = cls.Doc
		si.File = cls.File
		si.Line = cls.Line
		if cls.File != "" && cls.Line > 0 {
			si.Source = fmt.Sprintf("%s:%d", cls.File, cls.Line)
		}
		for _, fname := range sortedStringMapKeys(cls.Fields) {
			si.SetField(fname, cls.Fields[fname])
		}
		for _, mname := range sortedFuncJSONKeys(cls.Methods) {
			mf := cls.Methods[mname]
			si.Methods[mname] = funcInfoFromJSON(mname, mf)
		}
		p.Structs[name] = si
	}

	for name, iface := range raw.Interfaces {
		ii := extract.NewInterfaceInfo()
		ii.Doc = iface.Doc
		ii.File = iface.File
		ii.Line = iface.Line
		if iface.File != "" && iface.Line > 0 {
			ii.Source = fmt.Sprintf("%s:%d", iface.File, iface.Line)
		}
		// Property-shape members come through as `fields` in JSON; for an
		// interface we treat them as zero-arity methods returning the
		// property's type, since the data model has no per-interface fields.
		// This mirrors the legacy behavior and keeps the round-trip clean.
		for _, fname := range sortedStringMapKeys(iface.Fields) {
			ftype := iface.Fields[fname]
			ii.Methods[fname] = &extract.FuncInfo{
				SignatureText: fmt.Sprintf("%s: %s", fname, ftype),
				File:          iface.File,
				Line:          iface.Line,
			}
			if iface.File != "" && iface.Line > 0 {
				ii.Methods[fname].Source = fmt.Sprintf("%s:%d", iface.File, iface.Line)
			}
		}
		for _, mname := range sortedFuncJSONKeys(iface.Methods) {
			mf := iface.Methods[mname]
			ii.Methods[mname] = funcInfoFromJSON(mname, mf)
		}
		p.Interfaces[name] = ii
	}

	for name, fn := range raw.Functions {
		p.Functions[name] = funcInfoFromJSON(name, fn)
	}

	for name, td := range raw.TypeDefs {
		ti := &extract.TypeDefInfo{
			Underlying: td.Underlying,
			Doc:        td.Doc,
			File:       td.File,
			Line:       td.Line,
		}
		if td.File != "" && td.Line > 0 {
			ti.Source = fmt.Sprintf("%s:%d", td.File, td.Line)
		}
		p.TypeDefs[name] = ti
	}
}

// funcInfoFromJSON builds a *FuncInfo whose SignatureText is the canonical
// TypeScript form: "Name(p1: t1, p2: t2): retType".
func funcInfoFromJSON(name string, fn tsFuncJSON) *extract.FuncInfo {
	fi := &extract.FuncInfo{
		SignatureText: tsFuncSigText(name, fn),
		Doc:           fn.Doc,
		File:          fn.File,
		Line:          fn.Line,
	}
	if fn.File != "" && fn.Line > 0 {
		fi.Source = fmt.Sprintf("%s:%d", fn.File, fn.Line)
	}
	return fi
}

// tsFuncSigText returns the canonical FuncInfo.SignatureText for a TS func
// or method: "Name(p1: t1, p2: t2): retType". No `function` keyword. No
// trailing semicolon. No receiver clause.
func tsFuncSigText(name string, fn tsFuncJSON) string {
	parts := make([]string, 0, len(fn.Params))
	for _, p := range fn.Params {
		parts = append(parts, fmt.Sprintf("%s: %s", p.Name, p.Type))
	}
	ret := "void"
	if len(fn.Returns) > 0 && fn.Returns[0] != "" {
		ret = fn.Returns[0]
	}
	return fmt.Sprintf("%s(%s): %s", name, strings.Join(parts, ", "), ret)
}

// --- GenerateTs / UpdateTs / VerifyTs -------------------------------------

// GenerateTs scaffolds a fresh <dirname>.ts.lyric file for srcDir. It does
// NOT write to disk; the caller chooses what to do with the returned content
// (the lyre CLI writes only if the target doesn't already exist).
func GenerateTs(srcDir string) (outPath, content string, err error) {
	p, err := ExtractTs(srcDir)
	if err != nil {
		return "", "", err
	}
	absDir, err := filepath.Abs(srcDir)
	if err != nil {
		return "", "", err
	}
	outPath = filepath.Join(absDir, p.Name+".ts.lyric")
	content = cdd.Write(p)
	return outPath, content, nil
}

// UpdateTs refreshes lyricPath: re-extracts signatures/positions from TS
// source, adds new exports, preserves all human prose (ModuleWhy, Docs,
// Invariants, per-decl Why, per-field Doc). Returns the human-readable list
// of additions. Source list is refreshed.
func UpdateTs(lyricPath string) (added []string, err error) {
	raw, err := os.ReadFile(lyricPath)
	if err != nil {
		return nil, err
	}
	existing, err := cdd.Parse(string(raw), lyricPath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", lyricPath, err)
	}

	srcDir := filepath.Dir(lyricPath)
	fresh, err := ExtractTs(srcDir)
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

// VerifyTs parses lyricPath as a .ts.lyric v2 file, re-extracts the TS
// source it lives next to, and reports drift via Findings.
func VerifyTs(lyricPath string) (*VerifyResult, error) {
	raw, err := os.ReadFile(lyricPath)
	if err != nil {
		return nil, err
	}
	declared, err := cdd.Parse(string(raw), lyricPath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", lyricPath, err)
	}

	srcDir := filepath.Dir(lyricPath)
	actual, err := ExtractTs(srcDir)
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
// from `fresh`, adds new exports, and refreshes the module-level source
// list. All human prose on existing decls is preserved.
//
// NOT pruned: decls present in existing but absent from fresh remain in
// the file (VerifyTs reports them as drift).
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
		es.Why = extract.PreferFresh(es.Why, fs.Why)
		preservedDoc := map[string]string{}
		for _, f := range es.Fields {
			if f.Doc != "" {
				preservedDoc[f.Name] = f.Doc
			}
		}
		es.Fields = es.Fields[:0]
		for _, f := range fs.Fields {
			ff := f
			if ff.Doc == "" {
				if doc, ok := preservedDoc[f.Name]; ok {
					ff.Doc = doc
				}
			}
			es.Fields = append(es.Fields, ff)
		}
		for mn, fm := range fs.Methods {
			if em, ok := es.Methods[mn]; ok {
				em.SignatureText = fm.SignatureText
				em.File, em.Line, em.Source = fm.File, fm.Line, fm.Source
				em.Why = extract.PreferFresh(em.Why, fm.Why)
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
		ei.Why = extract.PreferFresh(ei.Why, fi.Why)
		for mn, fm := range fi.Methods {
			if em, ok := ei.Methods[mn]; ok {
				em.SignatureText = fm.SignatureText
				em.File, em.Line, em.Source = fm.File, fm.Line, fm.Source
				em.Why = extract.PreferFresh(em.Why, fm.Why)
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
		ef.Why = extract.PreferFresh(ef.Why, ff.Why)
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
		et.Why = extract.PreferFresh(et.Why, ft.Why)
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
				result.add(SevError, file, srcStr, fmt.Sprintf("class %s: field %s not found in source", name, df.Name))
				continue
			}
			if !typesMatch(df.SignatureText, actualType) {
				result.add(SevError, file, srcStr, fmt.Sprintf("class %s: field %s type mismatch: .lyric=%s, source=%s", name, df.Name, df.SignatureText, actualType))
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
			result.add(SevWarning, file, srcStr, fmt.Sprintf("class %s: source has field %s not in .lyric", name, extra))
		}
		for mn, dm := range ds.Methods {
			am, ok := as.Methods[mn]
			if !ok {
				result.add(SevError, file, srcStr, fmt.Sprintf("class %s: method %s not found in source", name, mn))
				continue
			}
			if !sigMatch(dm.SignatureText, am.SignatureText) {
				result.add(SevError, file, srcStr, fmt.Sprintf("class %s: method %s signature mismatch: .lyric=%q, source=%q", name, mn, dm.SignatureText, am.SignatureText))
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

// sigMatch compares two signatures with spec §7 normalization: strip
// leading/trailing whitespace, collapse runs of ASCII whitespace to a
// single space, then byte-equal.
func sigMatch(a, b string) bool {
	return normalizeSig(a) == normalizeSig(b)
}

func normalizeSig(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	inSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		// Treat newlines and carriage returns as whitespace so multi-line
		// type literals (e.g. inline object types from .ts source) compare
		// equal to the writer's flattened single-line form. See
		// pkg/cdd/writer.go:flattenSig — the two sides must agree.
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

// typesMatch compares two TypeScript type strings with whitespace
// normalization. TS has no equivalent of Go's `any`↔`interface{}` quirk and
// no package-prefix complication, so this just delegates to sigMatch.
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

func sortedFuncJSONKeys(m map[string]tsFuncJSON) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
