// Package golang provides Go-syntax LDD file generation and parsing.
// A .go.lyric file is a valid Go source file (minus the build tag)
// containing type declarations, function signatures, and LDD metadata
// in structured comments.
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
)

// ParseLDDMeta extracts LDD metadata from //ldd: comments in a source string.
func ParseLDDMeta(src string) *extract.LDDMeta {
	meta := &extract.LDDMeta{Lang: "go"}
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

// ParseLDDFile parses a .go.lyric understanding file and returns both
// the declared API and the LDD metadata.
func ParseLDDFile(path string) (*extract.PackageInfo, *extract.LDDMeta, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	meta := ParseLDDMeta(string(src))
	info, err := ExtractSource(string(src), path)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return info, meta, nil
}

// GenerateLDDFile produces a .go.lyric understanding file for a Go package.
func GenerateLDDFile(pkgDir string) (string, string, error) {
	absDir, err := filepath.Abs(pkgDir)
	if err != nil {
		return "", "", err
	}

	goFiles, err := ScanSourceFiles(absDir)
	if err != nil {
		return "", "", err
	}
	if len(goFiles) == 0 {
		return "", "", fmt.Errorf("no .go files found in %s", pkgDir)
	}

	// Parse all Go files
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range goFiles {
		f, err := goparser.ParseFile(fset, filepath.Join(absDir, name), nil, 0)
		if err != nil {
			return "", "", fmt.Errorf("parsing %s: %w", name, err)
		}
		files = append(files, f)
	}

	pkgName := ""
	if len(files) > 0 {
		pkgName = files[0].Name.Name
	}

	// Collect exported types and functions
	var (
		structs    []structDecl
		interfaces []ifaceDecl
		typedefs   []typedefDecl
		functions  []funcDecl
	)

	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || !IsExported(ts.Name.Name) {
						continue
					}
					switch t := ts.Type.(type) {
					case *ast.StructType:
						structs = append(structs, collectStruct(ts.Name.Name, ts, t))
					case *ast.InterfaceType:
						interfaces = append(interfaces, collectInterface(ts.Name.Name, ts, t))
					default:
						typedefs = append(typedefs, typedefDecl{
							Name:       ts.Name.Name,
							TypeParams: typeParamString(ts),
							Underlying: TypeString(ts.Type),
						})
					}
				}
			case *ast.FuncDecl:
				if !IsExported(d.Name.Name) {
					continue
				}
				if d.Recv != nil {
					continue // methods collected separately
				}
				functions = append(functions, funcDecl{
					Name: d.Name.Name,
					Sig:  BuildSignature(d, fset),
				})
			}
		}
	}

	// Collect exported methods per struct
	methodMap := make(map[string][]funcDecl)
	for _, file := range files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || !IsExported(fd.Name.Name) {
				continue
			}
			recvName := receiverTypeName(fd.Recv.List[0].Type)
			if recvName != "" && IsExported(recvName) {
				methodMap[recvName] = append(methodMap[recvName], funcDecl{
					Name: fd.Name.Name,
					Sig:  BuildSignature(fd, fset),
				})
			}
		}
	}

	// Generate output
	var b strings.Builder
	b.WriteString("//go:build ignore\n\n")
	b.WriteString(fmt.Sprintf("//ldd:source %s\n", strings.Join(goFiles, ", ")))
	b.WriteString("//ldd:why \"\"\n\n")
	b.WriteString(fmt.Sprintf("package %s\n", pkgName))

	// Structs
	for _, s := range structs {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("type %s%s struct {\n", s.Name, s.TypeParams))
		for _, f := range s.Fields {
			b.WriteString(fmt.Sprintf("\t%s %s\n", f.Name, f.Type))
		}
		b.WriteString("}\n")

		// Methods for this struct
		if methods, ok := methodMap[s.Name]; ok {
			for _, m := range methods {
				b.WriteString(fmt.Sprintf("\n%s\n", m.Sig))
			}
		}
	}

	// Interfaces
	for _, iface := range interfaces {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("type %s%s interface {\n", iface.Name, iface.TypeParams))
		for _, m := range iface.Methods {
			b.WriteString(fmt.Sprintf("\t%s\n", m))
		}
		b.WriteString("}\n")
	}

	// Typedefs
	for _, td := range typedefs {
		b.WriteString(fmt.Sprintf("\ntype %s%s %s\n", td.Name, td.TypeParams, td.Underlying))
	}

	// Standalone functions
	for _, fn := range functions {
		b.WriteString(fmt.Sprintf("\n%s\n", fn.Sig))
	}

	// Auto-generated index
	b.WriteString("\n// --- index ---\n")
	b.WriteString("// Auto-generated function/method index.\n")
	b.WriteString("// DO NOT EDIT below this line — regenerated by `lyre update`.\n")

	// Determine output filename
	outName := pkgName + ".go.lyric"
	outPath := filepath.Join(absDir, outName)

	return outPath, b.String(), nil
}

// Helper types for generation

type structDecl struct {
	Name       string
	TypeParams string
	Fields     []fieldDecl
}

type fieldDecl struct {
	Name string
	Type string
}

type ifaceDecl struct {
	Name       string
	TypeParams string
	Methods    []string // method signatures
}

type typedefDecl struct {
	Name       string
	TypeParams string
	Underlying string
}

type funcDecl struct {
	Name string
	Sig  string
}

func collectStruct(name string, ts *ast.TypeSpec, st *ast.StructType) structDecl {
	s := structDecl{
		Name:       name,
		TypeParams: typeParamString(ts),
	}
	if st.Fields != nil {
		for _, f := range st.Fields.List {
			typStr := TypeString(f.Type)
			if len(f.Names) == 0 {
				// Embedded field
				s.Fields = append(s.Fields, fieldDecl{Name: typStr, Type: ""})
			} else {
				for _, n := range f.Names {
					if IsExported(n.Name) {
						s.Fields = append(s.Fields, fieldDecl{Name: n.Name, Type: typStr})
					}
				}
			}
		}
	}
	return s
}

func collectInterface(name string, ts *ast.TypeSpec, it *ast.InterfaceType) ifaceDecl {
	iface := ifaceDecl{
		Name:       name,
		TypeParams: typeParamString(ts),
	}
	if it.Methods != nil {
		for _, m := range it.Methods.List {
			if ft, ok := m.Type.(*ast.FuncType); ok {
				for _, n := range m.Names {
					sig := n.Name + "(" + fieldListString(ft.Params) + ")"
					if ft.Results != nil {
						results := ft.Results.List
						if len(results) == 1 && len(results[0].Names) == 0 {
							sig += " " + TypeString(results[0].Type)
						} else if len(results) > 0 {
							sig += " (" + fieldListString(ft.Results) + ")"
						}
					}
					iface.Methods = append(iface.Methods, sig)
				}
			} else if id, ok := m.Type.(*ast.Ident); ok {
				// Embedded interface
				iface.Methods = append(iface.Methods, id.Name)
			}
		}
	}
	return iface
}

func typeParamString(ts *ast.TypeSpec) string {
	if ts.TypeParams == nil || len(ts.TypeParams.List) == 0 {
		return ""
	}
	return "[" + fieldListString(ts.TypeParams) + "]"
}

// VerifyGoLDD compares a .go.lyric understanding file against the actual Go source.
func VerifyGoLDD(lddPath string) (*VerifyResult, error) {
	// Parse the LDD file
	declared, meta, err := ParseLDDFile(lddPath)
	if err != nil {
		return nil, err
	}

	if len(meta.Source) == 0 {
		return nil, fmt.Errorf("no //ldd:source directive found in %s", lddPath)
	}

	// Parse the actual source files
	lddDir := filepath.Dir(lddPath)
	actual := extract.NewPackageInfo("")

	for _, srcFile := range meta.Source {
		fullPath := filepath.Join(lddDir, srcFile)
		info, err := os.Stat(fullPath)
		if err != nil {
			return &VerifyResult{
				Findings: []Finding{{
					Severity: SevError,
					File:     lddPath,
					Source:   srcFile,
					Message:  "source file does not exist",
				}},
			}, nil
		}

		var extracted *extract.PackageInfo
		if info.IsDir() {
			extracted, err = ExtractDir(fullPath)
		} else {
			extracted, err = ExtractFiles([]string{fullPath})
		}
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", srcFile, err)
		}
		mergePackageInfo(actual, extracted)
	}

	// Compare declared vs actual
	result := &VerifyResult{}
	srcStr := strings.Join(meta.Source, ", ")

	compareStructs(declared, actual, lddPath, srcStr, result)
	compareInterfaces(declared, actual, lddPath, srcStr, result)
	compareFunctions(declared, actual, lddPath, srcStr, result)
	checkCompleteness(declared, actual, lddPath, srcStr, result)

	return result, nil
}

// VerifyResult holds all findings from a verification run.
type VerifyResult struct {
	Findings []Finding
}

// Severity levels
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

func (r *VerifyResult) ErrorCount() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == SevError {
			n++
		}
	}
	return n
}

func mergePackageInfo(dst, src *extract.PackageInfo) {
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

func compareStructs(declared, actual *extract.PackageInfo, file, srcStr string, result *VerifyResult) {
	for name, ds := range declared.Structs {
		as, ok := actual.Structs[name]
		if !ok {
			result.add(SevError, file, srcStr, fmt.Sprintf("struct %s declared in LDD but not found in source", name))
			continue
		}
		// Check fields
		for fieldName, declType := range ds.Fields {
			actualType, ok := as.Fields[fieldName]
			if !ok {
				result.add(SevError, file, srcStr, fmt.Sprintf("struct %s: field %s not found in source", name, fieldName))
				continue
			}
			if !typesMatch(declType, actualType) {
				result.add(SevError, file, srcStr, fmt.Sprintf("struct %s: field %s type mismatch: LDD=%s, source=%s", name, fieldName, declType, actualType))
			}
		}
		// Check for extra fields in source
		var extras []string
		for fieldName := range as.Fields {
			if _, ok := ds.Fields[fieldName]; !ok {
				extras = append(extras, fieldName)
			}
		}
		sort.Strings(extras)
		for _, extra := range extras {
			if IsExported(extra) {
				result.add(SevWarning, file, srcStr, fmt.Sprintf("struct %s: source has field %s not in LDD", name, extra))
			}
		}

		// Check methods
		for methodName, dm := range ds.Methods {
			am, ok := as.Methods[methodName]
			if !ok {
				result.add(SevError, file, srcStr, fmt.Sprintf("struct %s: method %s not found in source", name, methodName))
				continue
			}
			compareFuncInfo(fmt.Sprintf("struct %s method %s", name, methodName), dm, am, file, srcStr, result)
		}
	}
}

func compareInterfaces(declared, actual *extract.PackageInfo, file, srcStr string, result *VerifyResult) {
	for name, di := range declared.Interfaces {
		ai, ok := actual.Interfaces[name]
		if !ok {
			result.add(SevError, file, srcStr, fmt.Sprintf("interface %s declared in LDD but not found in source", name))
			continue
		}
		for methodName, dm := range di.Methods {
			am, ok := ai.Methods[methodName]
			if !ok {
				result.add(SevError, file, srcStr, fmt.Sprintf("interface %s: method %s not found in source", name, methodName))
				continue
			}
			compareFuncInfo(fmt.Sprintf("interface %s method %s", name, methodName), dm, am, file, srcStr, result)
		}
	}
}

func compareFunctions(declared, actual *extract.PackageInfo, file, srcStr string, result *VerifyResult) {
	for name, df := range declared.Functions {
		af, ok := actual.Functions[name]
		if !ok {
			result.add(SevError, file, srcStr, fmt.Sprintf("function %s not found in source", name))
			continue
		}
		compareFuncInfo(fmt.Sprintf("function %s", name), df, af, file, srcStr, result)
	}
}

func compareFuncInfo(context string, declared, actual *extract.FuncInfo, file, srcStr string, result *VerifyResult) {
	if len(declared.Params) != len(actual.Params) {
		result.add(SevError, file, srcStr, fmt.Sprintf("%s: param count mismatch: LDD=%d, source=%d", context, len(declared.Params), len(actual.Params)))
	} else {
		for i, dp := range declared.Params {
			ap := actual.Params[i]
			if !typesMatch(dp.Type, ap.Type) {
				result.add(SevError, file, srcStr, fmt.Sprintf("%s: param %d type mismatch: LDD=%s, source=%s", context, i+1, dp.Type, ap.Type))
			}
		}
	}
	if len(declared.Returns) != len(actual.Returns) {
		result.add(SevError, file, srcStr, fmt.Sprintf("%s: return count mismatch: LDD=%d, source=%d", context, len(declared.Returns), len(actual.Returns)))
	} else {
		for i, dr := range declared.Returns {
			ar := actual.Returns[i]
			if !typesMatch(dr, ar) {
				result.add(SevError, file, srcStr, fmt.Sprintf("%s: return %d type mismatch: LDD=%s, source=%s", context, i+1, dr, ar))
			}
		}
	}
}

func checkCompleteness(declared, actual *extract.PackageInfo, file, srcStr string, result *VerifyResult) {
	// Build set of declared names
	declaredNames := make(map[string]bool)
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

	// Check exported types
	var missingTypes []string
	for name := range actual.Structs {
		if IsExported(name) && !declaredNames[name] {
			missingTypes = append(missingTypes, name)
		}
	}
	for name := range actual.Interfaces {
		if IsExported(name) && !declaredNames[name] {
			missingTypes = append(missingTypes, name)
		}
	}
	for name := range actual.TypeDefs {
		if IsExported(name) && !declaredNames[name] {
			missingTypes = append(missingTypes, name)
		}
	}
	sort.Strings(missingTypes)
	for _, name := range missingTypes {
		result.add(SevError, file, srcStr, fmt.Sprintf("exported type %s not documented in LDD", name))
	}

	// Check exported functions
	var missingFuncs []string
	for name := range actual.Functions {
		if IsExported(name) && !declaredNames[name] {
			missingFuncs = append(missingFuncs, name)
		}
	}
	sort.Strings(missingFuncs)
	for _, name := range missingFuncs {
		result.add(SevError, file, srcStr, fmt.Sprintf("exported function %s not documented in LDD", name))
	}
}

// typesMatch compares two Go type strings, handling package prefix stripping.
func typesMatch(a, b string) bool {
	if a == b {
		return true
	}
	if a == "" || b == "" {
		return true // can't compare embedded fields
	}
	// any == interface{}
	if (a == "any" && b == "interface{}") || (a == "interface{}" && b == "any") {
		return true
	}
	// Strip package prefixes for comparison
	if stripPackagePrefix(a) == stripPackagePrefix(b) {
		return true
	}
	return false
}

// stripPackagePrefix removes Go package qualifiers from a type string.
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
