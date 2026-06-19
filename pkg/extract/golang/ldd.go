// Package golang's `.go.lyric` v2 entry points: ExtractGo, GenerateGo,
// UpdateGo, VerifyGo. The `.go.lyric` file is the persistent CDD artifact
// for a Go package — a small declarative DSL parsed/written by pkg/cdd,
// whose payload lines (field types, method signatures, function signatures)
// are verbatim Go text treated as opaque strings.
//
// Architectural principle (rich-doc upgrade plan, top): CDD documentation
// lives in the .lyric file ONLY, never as `// why:` or `// doc` comments in
// Go source. Extractors are signatures-only. The legacy //ldd:source and
// //ldd:why directives, and the //+// doc-comment scraping, are gone.
//
// File layout produced by GenerateGo:
//   - One <pkgname>.go.lyric per directory.
//   - PackageInfo.ModuleSource lists every non-test .go file in the
//     directory at extract time; refreshed on each UpdateGo.
//   - Each struct/interface/typedef/func/method carries File/Line/Source
//     ("<basename>:<line>") populated from go/parser positions.
package golang

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/waywardgeek/lyre/pkg/extract"
	"github.com/waywardgeek/lyre/pkg/cdd"
)

// docText returns the trimmed text of primary, falling back to fallback. Kept
// only for the legacy ExtractDir/ExtractFiles paths in golang.go which still
// populate the extractor-internal StructInfo.Doc / FuncInfo.Doc fields. These
// values do NOT round-trip through .lyric and are not consulted by GenerateGo
// / UpdateGo / VerifyGo — CDD prose lives in the .lyric file only.
func docText(primary, fallback *ast.CommentGroup) string {
	cg := primary
	if cg == nil {
		cg = fallback
	}
	if cg == nil {
		return ""
	}
	return strings.TrimSpace(cg.Text())
}

// --- ExtractGo --------------------------------------------------------------

// ExtractGo parses every non-test .go file in srcDir and returns the public
// API as a PackageInfo whose SignatureText fields are populated for round-
// trip through the .lyric v2 format. Only exported declarations are kept.
func ExtractGo(srcDir string) (*extract.PackageInfo, error) {
	absDir, err := filepath.Abs(srcDir)
	if err != nil {
		return nil, err
	}
	files, err := ScanSourceFiles(absDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .go files found in %s", srcDir)
	}

	fset := token.NewFileSet()
	var astFiles []*ast.File
	for _, name := range files {
		f, err := goparser.ParseFile(fset, filepath.Join(absDir, name), nil, goparser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}
		astFiles = append(astFiles, f)
	}

	p := extract.NewPackageInfo("")
	if len(astFiles) > 0 {
		p.Name = astFiles[0].Name.Name
	}
	p.ModuleSource = files

	for _, f := range astFiles {
		extractGoFile(fset, f, p)
	}
	return p, nil
}

func extractGoFile(fset *token.FileSet, file *ast.File, p *extract.PackageInfo) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !IsExported(ts.Name.Name) {
					continue
				}
				pos := fset.Position(ts.Pos())
				fb := filepath.Base(pos.Filename)
				src := fmt.Sprintf("%s:%d", fb, pos.Line)
				switch t := ts.Type.(type) {
				case *ast.StructType:
					si := extract.NewStructInfo()
					si.File, si.Line, si.Source = fb, pos.Line, src
					if t.Fields != nil {
						for _, f := range t.Fields.List {
							typ := TypeString(f.Type)
							if len(f.Names) == 0 {
								// Embedded field: name is the type itself,
								// signature is empty (per spec §4 the `field
								// <name>` form is permitted when no type).
								si.Fields = append(si.Fields, extract.FieldInfo{Name: typ})
								continue
							}
							for _, name := range f.Names {
								if !IsExported(name.Name) {
									continue
								}
								si.SetField(name.Name, typ)
							}
						}
					}
					p.Structs[ts.Name.Name] = si

				case *ast.InterfaceType:
					ii := extract.NewInterfaceInfo()
					ii.File, ii.Line, ii.Source = fb, pos.Line, src
					if t.Methods != nil {
						for _, m := range t.Methods.List {
							ft, ok := m.Type.(*ast.FuncType)
							if !ok {
								continue
							}
							for _, name := range m.Names {
								if !IsExported(name.Name) {
									continue
								}
								mp := fset.Position(name.Pos())
								mfb := filepath.Base(mp.Filename)
								ii.Methods[name.Name] = &extract.FuncInfo{
									SignatureText: goFuncSigText(name.Name, ft),
									File:          mfb,
									Line:          mp.Line,
									Source:        fmt.Sprintf("%s:%d", mfb, mp.Line),
								}
							}
						}
					}
					p.Interfaces[ts.Name.Name] = ii

				default:
					p.TypeDefs[ts.Name.Name] = &extract.TypeDefInfo{
						Underlying: TypeString(ts.Type),
						File:       fb,
						Line:       pos.Line,
						Source:     src,
					}
				}
			}

		case *ast.FuncDecl:
			if !IsExported(d.Name.Name) {
				continue
			}
			pos := fset.Position(d.Pos())
			fb := filepath.Base(pos.Filename)
			src := fmt.Sprintf("%s:%d", fb, pos.Line)
			fi := &extract.FuncInfo{
				SignatureText: goFuncSigText(d.Name.Name, d.Type),
				File:          fb,
				Line:          pos.Line,
				Source:        src,
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recvType := receiverTypeName(d.Recv.List[0].Type)
				if recvType == "" || !IsExported(recvType) {
					continue
				}
				si, ok := p.Structs[recvType]
				if !ok {
					si = extract.NewStructInfo()
					p.Structs[recvType] = si
				}
				si.Methods[d.Name.Name] = fi
			} else {
				p.Functions[d.Name.Name] = fi
			}
		}
	}
}

// goFuncSigText returns the canonical FuncInfo.SignatureText for a Go func
// or method: "<Name>[<TypeParams>](<params>) <returns>" — no `func` keyword,
// no receiver clause. The receiver is implied by the containing class block.
func goFuncSigText(name string, ft *ast.FuncType) string {
	var b strings.Builder
	b.WriteString(name)
	if ft.TypeParams != nil && len(ft.TypeParams.List) > 0 {
		b.WriteString("[")
		b.WriteString(fieldListString(ft.TypeParams))
		b.WriteString("]")
	}
	b.WriteString("(")
	if ft.Params != nil {
		b.WriteString(fieldListString(ft.Params))
	}
	b.WriteString(")")
	if ft.Results != nil {
		results := ft.Results.List
		if len(results) == 1 && len(results[0].Names) == 0 {
			b.WriteString(" ")
			b.WriteString(TypeString(results[0].Type))
		} else if len(results) > 0 {
			b.WriteString(" (")
			b.WriteString(fieldListString(ft.Results))
			b.WriteString(")")
		}
	}
	return b.String()
}

// --- GenerateGo / UpdateGo / VerifyGo --------------------------------------

// GenerateGo scaffolds a fresh <pkgname>.go.lyric file for srcDir. It does
// NOT write to disk; the caller chooses what to do with the returned content
// (the lyre CLI writes only if the target doesn't already exist).
func GenerateGo(srcDir string) (outPath, content string, err error) {
	p, err := ExtractGo(srcDir)
	if err != nil {
		return "", "", err
	}
	absDir, err := filepath.Abs(srcDir)
	if err != nil {
		return "", "", err
	}
	outPath = filepath.Join(absDir, p.Name+".go.lyric")
	content = cdd.Write(p)
	return outPath, content, nil
}

// UpdateGo refreshes lyricPath: re-extracts signatures/positions from Go
// source, adds new exports, preserves all human prose (ModuleWhy, Docs,
// Invariants, per-decl Why, per-field Doc). Returns the human-readable list
// of additions ("struct Foo", "func Bar", ...). Source list is refreshed.
func UpdateGo(lyricPath string) (added []string, err error) {
	raw, err := os.ReadFile(lyricPath)
	if err != nil {
		return nil, err
	}
	existing, err := cdd.Parse(string(raw), lyricPath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", lyricPath, err)
	}

	srcDir := filepath.Dir(lyricPath)
	fresh, err := ExtractGo(srcDir)
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

// mergeFreshIntoExisting refreshes signatures/positions on existing decls
// from `fresh`, adds new exports, and refreshes module-level source list.
// All human prose on existing decls is preserved.
//
// NOT pruned (Phase 4/6 will add a --prune option): decls present in
// existing but absent from fresh remain in the file. Verify reports them.
func mergeFreshIntoExisting(existing, fresh *extract.PackageInfo) []string {
	var added []string

	// Refresh module-level source list (always overwrite — it's mechanical).
	existing.ModuleSource = fresh.ModuleSource

	// Structs / classes.
	freshStructNames := sortedKeys(fresh.Structs)
	for _, name := range freshStructNames {
		fs := fresh.Structs[name]
		es, ok := existing.Structs[name]
		if !ok {
			existing.Structs[name] = fs
			added = append(added, "struct "+name)
			continue
		}
		// Refresh positions + source ref.
		es.File, es.Line, es.Source = fs.File, fs.Line, fs.Source
		// Refresh field signatures in source order from fresh; preserve
		// per-field Doc from existing where the field still exists.
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
		// Methods: refresh signatures/positions; add new methods; preserve
		// existing Why on retained methods.
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

	// Interfaces.
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

	// Top-level functions.
	for _, name := range sortedKeys(fresh.Functions) {
		ff := fresh.Functions[name]
		ef, ok := existing.Functions[name]
		if !ok {
			existing.Functions[name] = ff
			added = append(added, "func "+name)
			continue
		}
		ef.SignatureText = ff.SignatureText
		ef.File, ef.Line, ef.Source = ff.File, ff.Line, ff.Source
	}

	// Typedefs.
	for _, name := range sortedKeys(fresh.TypeDefs) {
		ft := fresh.TypeDefs[name]
		et, ok := existing.TypeDefs[name]
		if !ok {
			existing.TypeDefs[name] = ft
			added = append(added, "typedef "+name)
			continue
		}
		et.Underlying = ft.Underlying
		et.File, et.Line, et.Source = ft.File, ft.Line, ft.Source
	}

	sort.Strings(added)
	return added
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// VerifyGo parses lyricPath as a .go.lyric v2 file, re-extracts the Go
// source it lives next to, and reports drift via Findings.
func VerifyGo(lyricPath string) (*VerifyResult, error) {
	raw, err := os.ReadFile(lyricPath)
	if err != nil {
		return nil, err
	}
	declared, err := cdd.Parse(string(raw), lyricPath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", lyricPath, err)
	}

	srcDir := filepath.Dir(lyricPath)
	actual, err := ExtractGo(srcDir)
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

// --- VerifyResult / Finding / Severity (unchanged from v1) ------------------

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

// --- comparison helpers -----------------------------------------------------

func compareStructs(declared, actual *extract.PackageInfo, file, srcStr string, result *VerifyResult) {
	for _, name := range sortedKeys(declared.Structs) {
		ds := declared.Structs[name]
		as, ok := actual.Structs[name]
		if !ok {
			result.add(SevError, file, srcStr, fmt.Sprintf("struct %s declared in .lyric but not found in source", name))
			continue
		}
		// Fields: type match.
		for _, df := range ds.Fields {
			actualType, ok := as.FieldSig(df.Name)
			if !ok {
				result.add(SevError, file, srcStr, fmt.Sprintf("struct %s: field %s not found in source", name, df.Name))
				continue
			}
			if !typesMatch(df.SignatureText, actualType) {
				result.add(SevError, file, srcStr, fmt.Sprintf("struct %s: field %s type mismatch: .lyric=%s, source=%s", name, df.Name, df.SignatureText, actualType))
			}
		}
		// Source-only fields → warnings (extra exported fields not in .lyric).
		var extras []string
		for _, af := range as.Fields {
			if !ds.HasField(af.Name) {
				extras = append(extras, af.Name)
			}
		}
		sort.Strings(extras)
		for _, extra := range extras {
			if IsExported(extra) {
				result.add(SevWarning, file, srcStr, fmt.Sprintf("struct %s: source has field %s not in .lyric", name, extra))
			}
		}
		// Methods.
		for mn, dm := range ds.Methods {
			am, ok := as.Methods[mn]
			if !ok {
				result.add(SevError, file, srcStr, fmt.Sprintf("struct %s: method %s not found in source", name, mn))
				continue
			}
			if !sigMatch(dm.SignatureText, am.SignatureText) {
				result.add(SevError, file, srcStr, fmt.Sprintf("struct %s: method %s signature mismatch: .lyric=%q, source=%q", name, mn, dm.SignatureText, am.SignatureText))
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
			result.add(SevError, file, srcStr, fmt.Sprintf("typedef %s declared in .lyric but not found in source", name))
			continue
		}
		if !typesMatch(dt.Underlying, at.Underlying) {
			result.add(SevError, file, srcStr, fmt.Sprintf("typedef %s underlying type mismatch: .lyric=%s, source=%s", name, dt.Underlying, at.Underlying))
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
		if IsExported(name) && !declaredNames[name] {
			missing = append(missing, "struct "+name)
		}
	}
	for name := range actual.Interfaces {
		if IsExported(name) && !declaredNames[name] {
			missing = append(missing, "interface "+name)
		}
	}
	for name := range actual.TypeDefs {
		if IsExported(name) && !declaredNames[name] {
			missing = append(missing, "typedef "+name)
		}
	}
	for name := range actual.Functions {
		if IsExported(name) && !declaredNames[name] {
			missing = append(missing, "function "+name)
		}
	}
	sort.Strings(missing)
	for _, kind := range missing {
		result.add(SevError, file, srcStr, fmt.Sprintf("exported %s not documented in .lyric", kind))
	}
}

// sigMatch compares two function signatures with the spec §7 normalization:
// strip leading/trailing whitespace, collapse runs of ASCII whitespace to a
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
		if c == ' ' || c == '\t' {
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

// typesMatch compares two Go type strings, handling the special cases of
// package-prefix qualifiers and `any` ↔ `interface{}`.
func typesMatch(a, b string) bool {
	if sigMatch(a, b) {
		return true
	}
	if a == "" || b == "" {
		return true // tolerated for embedded fields
	}
	if (a == "any" && b == "interface{}") || (a == "interface{}" && b == "any") {
		return true
	}
	if stripPackagePrefix(a) == stripPackagePrefix(b) {
		return true
	}
	return false
}

func stripPackagePrefix(goType string) string {
	if strings.HasPrefix(goType, "*") {
		return "*" + stripPackagePrefix(goType[1:])
	}
	if strings.HasPrefix(goType, "[]") {
		return "[]" + stripPackagePrefix(goType[2:])
	}
	if strings.HasPrefix(goType, "map[") {
		depth := 1
		i := 4
		for i < len(goType) && depth > 0 {
			if goType[i] == '[' {
				depth++
			} else if goType[i] == ']' {
				depth--
			}
			i++
		}
		return "map[" + stripPackagePrefix(goType[4:i-1]) + "]" + stripPackagePrefix(goType[i:])
	}
	if idx := strings.LastIndex(goType, "."); idx >= 0 {
		return goType[idx+1:]
	}
	return goType
}
