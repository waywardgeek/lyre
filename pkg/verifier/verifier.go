// Package verifier compares .lyric understanding files against Go source code,
// reporting structural drift between the declared understanding and the implementation.
package verifier

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	lyricast "github.com/waywardgeek/lyre/pkg/ast"
	"github.com/waywardgeek/lyre/pkg/parser"
)

// Severity classifies how serious a drift finding is.
type Severity int

const (
	Error   Severity = iota // missing type, wrong field type
	Warning                 // extra fields not in .lyric, naming convention mismatch
	Info                    // informational (e.g., Go has more methods than .lyric declares)
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "ERROR"
	case Warning:
		return "WARNING"
	case Info:
		return "INFO"
	}
	return "UNKNOWN"
}

// Finding is a single drift report.
type Finding struct {
	Severity Severity
	LyricFile string
	GoFile   string
	Message  string
}

func (f Finding) String() string {
	loc := f.LyricFile
	if f.GoFile != "" {
		loc = fmt.Sprintf("%s ↔ %s", f.LyricFile, f.GoFile)
	}
	return fmt.Sprintf("[%s] %s: %s", f.Severity, loc, f.Message)
}

// Result holds all findings from a verification run.
type Result struct {
	Findings []Finding
}

func (r *Result) add(sev Severity, lyricFile, goFile, msg string) {
	r.Findings = append(r.Findings, Finding{
		Severity: sev,
		LyricFile: lyricFile,
		GoFile:   goFile,
		Message:  msg,
	})
}

// ErrorCount returns the number of error-level findings.
func (r *Result) ErrorCount() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == Error {
			n++
		}
	}
	return n
}

// Verify parses a .lyric file and compares it against the Go source files
// referenced in source: annotations. baseDir is the project root used to
// resolve relative source paths.
func Verify(lyricPath string) (*Result, error) {
	src, err := os.ReadFile(lyricPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", lyricPath, err)
	}

	lyricFile, err := parser.ParseFile(string(src), lyricPath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", lyricPath, err)
	}

	result := &Result{}

	lyricDir := filepath.Dir(lyricPath)

	for _, block := range lyricFile.Blocks {
		if len(block.Source) == 0 {
			result.add(Info, lyricPath, "", fmt.Sprintf("lyric block %q has no source: annotations", block.Name))
			continue
		}

		// Aggregate all Go types across all source files
		goInfo := &goTypeInfo{
			Structs:    make(map[string]*goStructInfo),
			Interfaces: make(map[string]*goInterfaceInfo),
			Functions:  make(map[string]*goFuncInfo),
			TypeDefs:   make(map[string]bool),
		}

		for _, srcPath := range block.Source {
			// Resolve source paths relative to the .lyric file's directory
			goFullPath := filepath.Join(lyricDir, srcPath)
			info, err := os.Stat(goFullPath)
			if err != nil {
				if os.IsNotExist(err) {
					result.add(Error, lyricPath, srcPath, "source file does not exist")
				} else {
					result.add(Error, lyricPath, srcPath, fmt.Sprintf("cannot stat: %v", err))
				}
				continue
			}

			var fileInfo *goTypeInfo
			if info.IsDir() {
				fileInfo, err = parseGoDir(goFullPath)
			} else {
				fileInfo, err = parseGoFile(goFullPath)
			}
			if err != nil {
				result.add(Error, lyricPath, srcPath, fmt.Sprintf("failed to parse Go file: %v", err))
				continue
			}
			mergeGoInfo(goInfo, fileInfo)
		}

		// Now compare the lyric block against the aggregated Go types
		verifyBlock(block, goInfo, lyricPath, result)
	}

	return result, nil
}

// goTypeInfo holds extracted Go type information.
type goTypeInfo struct {
	Structs    map[string]*goStructInfo
	Interfaces map[string]*goInterfaceInfo
	Functions  map[string]*goFuncInfo
	TypeDefs   map[string]bool // type names that are simple typedefs (e.g., type Foo int)
}

type goStructInfo struct {
	Fields  map[string]string // field name → type string
	Methods map[string]*goFuncInfo
}

type goInterfaceInfo struct {
	Methods map[string]*goFuncInfo
}

type goFuncInfo struct {
	Params  []goParam
	Returns []string
}

type goParam struct {
	Name string
	Type string
}

func mergeGoInfo(dst, src *goTypeInfo) {
	for k, v := range src.Structs {
		if existing, ok := dst.Structs[k]; ok {
			// Merge methods
			for mk, mv := range v.Methods {
				existing.Methods[mk] = mv
			}
			for fk, fv := range v.Fields {
				existing.Fields[fk] = fv
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

func verifyBlock(block lyricast.LyricBlock, goInfo *goTypeInfo, lyricPath string, result *Result) {
	srcStr := strings.Join(block.Source, ", ")

	for _, s := range block.Structs {
		verifyStruct(s, goInfo, lyricPath, srcStr, result)
	}
	for _, c := range block.Classes {
		verifyClass(c, goInfo, lyricPath, srcStr, result)
	}
	for _, e := range block.Enums {
		verifyEnum(e, goInfo, lyricPath, srcStr, result)
	}
	for _, i := range block.Interfaces {
		verifyInterface(i, goInfo, lyricPath, srcStr, result)
	}
	for _, f := range block.Functions {
		verifyFunction(f, goInfo, lyricPath, srcStr, result)
	}

	// Check for exported Go symbols not documented in .lyric
	verifyCompleteness(block, goInfo, lyricPath, srcStr, result)
}

// verifyCompleteness checks that all exported Go symbols in the source files
// are documented in the .lyric block. Reports errors for missing symbols.
func verifyCompleteness(block lyricast.LyricBlock, goInfo *goTypeInfo, lyricPath, goFile string, result *Result) {
	// Build set of all names declared in .lyric
	forgeNames := make(map[string]bool)
	for _, s := range block.Structs {
		forgeNames[s.Name] = true
	}
	for _, c := range block.Classes {
		forgeNames[c.Name] = true
	}
	for _, e := range block.Enums {
		forgeNames[e.Name] = true
	}
	for _, i := range block.Interfaces {
		forgeNames[i.Name] = true
	}
	for _, f := range block.Functions {
		// Try PascalCase conversion since .lyric may use snake_case
		forgeNames[f.Name] = true
		forgeNames[snakeToPascal(f.Name)] = true
	}
	for _, ta := range block.TypeAliases {
		forgeNames[ta.Name] = true
	}

	// Check exported structs (which represent both structs and classes in .lyric)
	var missingTypes []string
	for name := range goInfo.Structs {
		if isExported(name) && !forgeNames[name] {
			missingTypes = append(missingTypes, name)
		}
	}

	// Check exported interfaces
	for name := range goInfo.Interfaces {
		if isExported(name) && !forgeNames[name] {
			missingTypes = append(missingTypes, name)
		}
	}

	// Check exported typedefs
	for name := range goInfo.TypeDefs {
		if isExported(name) && !forgeNames[name] {
			missingTypes = append(missingTypes, name)
		}
	}

	sort.Strings(missingTypes)
	for _, name := range missingTypes {
		result.add(Error, lyricPath, goFile, fmt.Sprintf("exported type %s not documented in .lyric", name))
	}

	// Check exported functions (not methods — those are checked per-struct already)
	var missingFuncs []string
	for name := range goInfo.Functions {
		if isExported(name) && !forgeNames[name] {
			missingFuncs = append(missingFuncs, name)
		}
	}

	sort.Strings(missingFuncs)
	for _, name := range missingFuncs {
		result.add(Error, lyricPath, goFile, fmt.Sprintf("exported function %s not documented in .lyric", name))
	}

}

// isExported returns true if a Go name starts with an uppercase letter.
func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

func parseGoDir(dir string) (*goTypeInfo, error) {
	fset := token.NewFileSet()
	pkgs, err := goparser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		return nil, err
	}

	info := &goTypeInfo{
		Structs:    make(map[string]*goStructInfo),
		Interfaces: make(map[string]*goInterfaceInfo),
		Functions:  make(map[string]*goFuncInfo),
		TypeDefs:   make(map[string]bool),
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			extractGoTypes(file, info)
		}
	}
	return info, nil
}

func parseGoFile(path string) (*goTypeInfo, error) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	info := &goTypeInfo{
		Structs:    make(map[string]*goStructInfo),
		Interfaces: make(map[string]*goInterfaceInfo),
		Functions:  make(map[string]*goFuncInfo),
		TypeDefs:   make(map[string]bool),
	}

	extractGoTypes(file, info)
	return info, nil
}

func extractGoTypes(file *ast.File, info *goTypeInfo) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					switch t := ts.Type.(type) {
					case *ast.StructType:
						si := &goStructInfo{
							Fields:  make(map[string]string),
							Methods: make(map[string]*goFuncInfo),
						}
						if t.Fields != nil {
							for _, f := range t.Fields.List {
								typStr := typeExprString(f.Type)
								for _, name := range f.Names {
									si.Fields[name.Name] = typStr
								}
							}
						}
						info.Structs[ts.Name.Name] = si

					case *ast.InterfaceType:
						ii := &goInterfaceInfo{
							Methods: make(map[string]*goFuncInfo),
						}
						if t.Methods != nil {
							for _, m := range t.Methods.List {
								if ft, ok := m.Type.(*ast.FuncType); ok {
									for _, name := range m.Names {
										ii.Methods[name.Name] = extractFuncType(ft)
									}
								}
							}
						}
						info.Interfaces[ts.Name.Name] = ii

					default:
						// Simple typedef (type Foo int, type Bar string, etc.)
						info.TypeDefs[ts.Name.Name] = true
					}
				}
			}

		case *ast.FuncDecl:
			fi := extractFuncInfo(d)
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recvType := receiverTypeName(d.Recv.List[0].Type)
				if recvType != "" {
					si, ok := info.Structs[recvType]
					if !ok {
						si = &goStructInfo{
							Fields:  make(map[string]string),
							Methods: make(map[string]*goFuncInfo),
						}
						info.Structs[recvType] = si
					}
					si.Methods[d.Name.Name] = fi
				}
			} else {
				info.Functions[d.Name.Name] = fi
			}
		}
	}
}

func extractFuncInfo(d *ast.FuncDecl) *goFuncInfo {
	return extractFuncType(d.Type)
}

func extractFuncType(ft *ast.FuncType) *goFuncInfo {
	fi := &goFuncInfo{}
	if ft.Params != nil {
		for _, p := range ft.Params.List {
			typStr := typeExprString(p.Type)
			if len(p.Names) == 0 {
				fi.Params = append(fi.Params, goParam{Type: typStr})
			} else {
				for _, name := range p.Names {
					fi.Params = append(fi.Params, goParam{Name: name.Name, Type: typStr})
				}
			}
		}
	}
	if ft.Results != nil {
		for _, r := range ft.Results.List {
			typStr := typeExprString(r.Type)
			count := len(r.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				fi.Returns = append(fi.Returns, typStr)
			}
		}
	}
	return fi
}

func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func typeExprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeExprString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeExprString(t.Elt)
		}
		return "[...]" + typeExprString(t.Elt)
	case *ast.MapType:
		return "map[" + typeExprString(t.Key) + "]" + typeExprString(t.Value)
	case *ast.SelectorExpr:
		return typeExprString(t.X) + "." + t.Sel.Name
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(...)"
	case *ast.ChanType:
		return "chan " + typeExprString(t.Value)
	case *ast.Ellipsis:
		return "..." + typeExprString(t.Elt)
	case *ast.IndexExpr:
		return typeExprString(t.X) + "[" + typeExprString(t.Index) + "]"
	case *ast.IndexListExpr:
		var parts []string
		for _, idx := range t.Indices {
			parts = append(parts, typeExprString(idx))
		}
		return typeExprString(t.X) + "[" + strings.Join(parts, ", ") + "]"
	default:
		return "?"
	}
}

// ---- Forge type → Go type string conversion ----

// lyricTypeToGoString converts a forge TypeExpr into the string format that
// typeExprString produces from Go AST, enabling structural comparison.
func lyricTypeToGoString(t lyricast.TypeExpr) string {
	switch t.Kind {
	case lyricast.TypeNamed:
		if t.Data == nil {
			return "?"
		}
		nt := t.Data.(lyricast.NamedType)
		name := forgeNameToGo(nt.Name)
		if len(nt.Args) == 0 {
			return name
		}
		// Generic: Stack<T> → Stack[T], Map<K,V> → Map[K, V]
		var args []string
		for _, a := range nt.Args {
			args = append(args, lyricTypeToGoString(a))
		}
		return name + "[" + strings.Join(args, ", ") + "]"

	case lyricast.TypeOptional:
		if t.Data == nil {
			return "?"
		}
		ot := t.Data.(lyricast.OptionalType)
		// T? in lyric → *T in Go
		return "*" + lyricTypeToGoString(ot.Inner)

	case lyricast.TypeRef:
		if t.Data == nil {
			return "?"
		}
		rt := t.Data.(lyricast.RefType)
		// ref T in lyric → *T in Go
		return "*" + lyricTypeToGoString(rt.Inner)

	case lyricast.TypeSequence:
		if t.Data == nil {
			return "[]?"
		}
		st := t.Data.(lyricast.SequenceType)
		return "[]" + lyricTypeToGoString(st.Elem)

	case lyricast.TypeMap:
		if t.Data == nil {
			return "map[?]?"
		}
		mt := t.Data.(lyricast.MapType)
		return "map[" + lyricTypeToGoString(mt.Key) + "]" + lyricTypeToGoString(mt.Value)

	case lyricast.TypeTuple:
		// Go doesn't have tuples; skip comparison
		return "?"

	case lyricast.TypeFunc:
		return "func(...)"

	case lyricast.TypeChannel:
		if t.Data == nil {
			return "chan ?"
		}
		ct := t.Data.(lyricast.ChannelType)
		return "chan " + lyricTypeToGoString(ct.Elem)

	case lyricast.TypeLock:
		return "sync.Mutex"

	case lyricast.TypeUnit:
		return ""

	default:
		return "?"
	}
}

// forgeNameToGo maps forge primitive type names to Go equivalents.
func forgeNameToGo(name string) string {
	switch name {
	case "string":
		return "string"
	case "bool":
		return "bool"
	case "i8":
		return "int8"
	case "i16":
		return "int16"
	case "i32":
		return "int32"
	case "i64":
		return "int64"
	case "i128", "i256":
		return name // no Go equivalent, keep as-is
	case "u8":
		return "uint8"
	case "u16":
		return "uint16"
	case "u32":
		return "uint32"
	case "u64":
		return "uint64"
	case "f32":
		return "float32"
	case "f64":
		return "float64"
	case "f128":
		return name
	case "int":
		return "int"
	case "any":
		return "any"
	case "error":
		return "error"
	default:
		return name // user-defined types pass through
	}
}

// typesMatch compares a forge type string against a Go type string.
// Returns true if they match or if the forge type is "?" (unknown/unconvertible).
func typesMatch(forgeStr, goStr string) bool {
	if forgeStr == "?" || forgeStr == "" {
		return true // can't compare, don't report false positive
	}
	if forgeStr == goStr {
		return true
	}
	// Go 1.18+: "any" is an alias for "interface{}"
	if (forgeStr == "any" && goStr == "interface{}") || (forgeStr == "interface{}" && goStr == "any") {
		return true
	}
	// Also handle in composite types: map[string]any == map[string]interface{}, etc.
	if strings.ReplaceAll(forgeStr, "any", "interface{}") == goStr || forgeStr == strings.ReplaceAll(goStr, "interface{}", "any") {
		return true
	}
	// Strip Go package prefix: "ast.Span" → "Span", "*ast.File" → "*File"
	// since .lyric files use unqualified type names
	stripped := stripPackagePrefix(goStr)
	if forgeStr == stripped {
		return true
	}
	return false
}

// stripPackagePrefix removes Go package qualifiers from a type string.
// Handles nested cases like "*ast.File", "[]ast.Node", "map[string]*ast.File".
func stripPackagePrefix(goType string) string {
	// Handle pointer prefix
	if strings.HasPrefix(goType, "*") {
		return "*" + stripPackagePrefix(goType[1:])
	}
	// Handle slice prefix
	if strings.HasPrefix(goType, "[]") {
		return "[]" + stripPackagePrefix(goType[2:])
	}
	// Handle map
	if strings.HasPrefix(goType, "map[") {
		// Find the ] that closes the key
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
	// Strip package prefix from qualified name: "ast.File" → "File"
	if idx := strings.LastIndex(goType, "."); idx >= 0 {
		return goType[idx+1:]
	}
	return goType
}

// ---- Verification helpers ----

// findGoName checks if a name exists in a map, trying both PascalCase and camelCase.
// Returns the found name and whether it was found.
func findGoName(name string, names map[string]*goFuncInfo) (string, bool) {
	pascal := snakeToPascal(name)
	if _, ok := names[pascal]; ok {
		return pascal, true
	}
	camel := snakeToCamel(name)
	if _, ok := names[camel]; ok {
		return camel, true
	}
	// Try exact match
	if _, ok := names[name]; ok {
		return name, true
	}
	return pascal, false
}

func verifyStruct(s lyricast.StructDecl, goInfo *goTypeInfo, lyricFile, goFile string, result *Result) {
	goStruct, ok := goInfo.Structs[s.Name]
	if !ok {
		result.add(Error, lyricFile, goFile, fmt.Sprintf("struct %s declared in .lyric but not found in Go", s.Name))
		return
	}

	for _, forgeField := range s.Fields {
		goType, found := findGoFieldType(forgeField.Name, goStruct.Fields)
		if !found {
			result.add(Error, lyricFile, goFile, fmt.Sprintf("struct %s: field %s not found in Go", s.Name, forgeField.Name))
			continue
		}
		lyricType := lyricTypeToGoString(forgeField.Type)
		if !typesMatch(lyricType, goType) {
			result.add(Error, lyricFile, goFile, fmt.Sprintf("struct %s: field %s type mismatch: .lyric=%s, Go=%s", s.Name, forgeField.Name, lyricType, goType))
		}
	}

	forgeFieldSet := make(map[string]bool)
	for _, f := range s.Fields {
		forgeFieldSet[snakeToPascal(f.Name)] = true
		forgeFieldSet[snakeToCamel(f.Name)] = true
		forgeFieldSet[f.Name] = true
	}
	var extras []string
	for goField := range goStruct.Fields {
		if !forgeFieldSet[goField] {
			extras = append(extras, goField)
		}
	}
	sort.Strings(extras)
	for _, extra := range extras {
		result.add(Warning, lyricFile, goFile, fmt.Sprintf("struct %s: Go has field %s not in .lyric", s.Name, extra))
	}
}

func verifyClass(c lyricast.ClassDecl, goInfo *goTypeInfo, lyricFile, goFile string, result *Result) {
	goStruct, ok := goInfo.Structs[c.Name]
	if !ok {
		result.add(Error, lyricFile, goFile, fmt.Sprintf("class %s declared in .lyric but not found as Go struct", c.Name))
		return
	}

	for _, forgeField := range c.Fields {
		goType, found := findGoFieldType(forgeField.Name, goStruct.Fields)
		if !found {
			result.add(Error, lyricFile, goFile, fmt.Sprintf("class %s: field %s not found in Go", c.Name, forgeField.Name))
			continue
		}
		lyricType := lyricTypeToGoString(forgeField.Type)
		if !typesMatch(lyricType, goType) {
			result.add(Error, lyricFile, goFile, fmt.Sprintf("class %s: field %s type mismatch: .lyric=%s, Go=%s", c.Name, forgeField.Name, lyricType, goType))
		}
	}

	for _, forgeMethod := range c.Methods {
		goName, found := findGoName(forgeMethod.Name, goStruct.Methods)
		if !found {
			pascal := snakeToPascal(forgeMethod.Name)
			camel := snakeToCamel(forgeMethod.Name)
			result.add(Error, lyricFile, goFile, fmt.Sprintf("class %s: method %s (tried Go: %s, %s) not found", c.Name, forgeMethod.Name, pascal, camel))
			continue
		}
		goFunc := goStruct.Methods[goName]
		verifyFuncSignature(fmt.Sprintf("class %s method %s", c.Name, forgeMethod.Name), forgeMethod, goFunc, lyricFile, goFile, result)
	}

	forgeMethodSet := make(map[string]bool)
	for _, m := range c.Methods {
		forgeMethodSet[snakeToPascal(m.Name)] = true
		forgeMethodSet[snakeToCamel(m.Name)] = true
		forgeMethodSet[m.Name] = true
	}
	var extras []string
	for goMethod := range goStruct.Methods {
		if !forgeMethodSet[goMethod] {
			extras = append(extras, goMethod)
		}
	}
	sort.Strings(extras)
	for _, extra := range extras {
		if isExported(extra) {
			result.add(Error, lyricFile, goFile, fmt.Sprintf("class %s: exported method %s not documented in .lyric", c.Name, extra))
		} else {
			result.add(Info, lyricFile, goFile, fmt.Sprintf("class %s: Go has method %s not in .lyric", c.Name, extra))
		}
	}
}

func verifyEnum(e lyricast.EnumDecl, goInfo *goTypeInfo, lyricFile, goFile string, result *Result) {
	// Go enums are typically: type FooKind int (typedef) or type Foo int
	// Check for the type as a typedef, struct, or interface
	_, hasStruct := goInfo.Structs[e.Name]
	_, hasIface := goInfo.Interfaces[e.Name]
	_, hasTypedef := goInfo.TypeDefs[e.Name]

	if hasStruct || hasIface || hasTypedef {
		return
	}

	// Also check XxxKind pattern
	kindName := e.Name + "Kind"
	if goInfo.TypeDefs[kindName] {
		return
	}

	result.add(Warning, lyricFile, goFile, fmt.Sprintf("enum %s: no matching Go type found (looked for %s, %s as typedef/struct/interface)", e.Name, e.Name, kindName))
}

func verifyInterface(i lyricast.InterfaceDecl, goInfo *goTypeInfo, lyricFile, goFile string, result *Result) {
	goIface, ok := goInfo.Interfaces[i.Name]
	if !ok {
		result.add(Error, lyricFile, goFile, fmt.Sprintf("interface %s declared in .lyric but not found in Go", i.Name))
		return
	}

	for _, forgeMethod := range i.Methods {
		goName := toGoMethodName(forgeMethod.Name)
		goFunc, ok := goIface.Methods[goName]
		if !ok {
			result.add(Error, lyricFile, goFile, fmt.Sprintf("interface %s: method %s (Go: %s) not found", i.Name, forgeMethod.Name, goName))
			continue
		}
		verifyFuncSignature(fmt.Sprintf("interface %s method %s", i.Name, forgeMethod.Name), forgeMethod, goFunc, lyricFile, goFile, result)
	}
}

func verifyFunction(f lyricast.FuncDecl, goInfo *goTypeInfo, lyricFile, goFile string, result *Result) {
	pascal := snakeToPascal(f.Name)
	camel := snakeToCamel(f.Name)

	var goFunc *goFuncInfo
	if fn, ok := goInfo.Functions[pascal]; ok {
		goFunc = fn
	} else if fn, ok := goInfo.Functions[camel]; ok {
		goFunc = fn
	} else if fn, ok := goInfo.Functions[f.Name]; ok {
		goFunc = fn
	}

	if goFunc == nil {
		result.add(Error, lyricFile, goFile, fmt.Sprintf("function %s (tried Go: %s, %s) not found", f.Name, pascal, camel))
		return
	}

	verifyFuncSignature(fmt.Sprintf("function %s", f.Name), f, goFunc, lyricFile, goFile, result)
}

// ---- Naming convention helpers ----

// toGoFieldName tries PascalCase first (exported), then the original name (unexported).
func toGoFieldName(name string) string {
	return snakeToPascal(name)
}

// findGoField checks if a field exists, trying PascalCase, camelCase, and exact match.
func findGoField(name string, fields map[string]string) bool {
	_, found := findGoFieldType(name, fields)
	return found
}

// findGoFieldType returns the Go type string for a field, trying PascalCase, camelCase, and exact match.
func findGoFieldType(name string, fields map[string]string) (string, bool) {
	pascal := snakeToPascal(name)
	if typ, ok := fields[pascal]; ok {
		return typ, true
	}
	camel := snakeToCamel(name)
	if typ, ok := fields[camel]; ok {
		return typ, true
	}
	if typ, ok := fields[name]; ok {
		return typ, true
	}
	return "", false
}

// verifyFuncSignature checks parameter count and return type of a forge function against Go.
func verifyFuncSignature(context string, forgeFunc lyricast.FuncDecl, goFunc *goFuncInfo, lyricFile, goFile string, result *Result) {
	// Count forge params excluding self
	forgeParamCount := 0
	for _, p := range forgeFunc.Params {
		if !p.IsSelf {
			forgeParamCount++
		}
	}
	goParamCount := len(goFunc.Params)

	if forgeParamCount != goParamCount {
		result.add(Error, lyricFile, goFile, fmt.Sprintf("%s: param count mismatch: .lyric=%d, Go=%d", context, forgeParamCount, goParamCount))
	} else {
		// Check param types positionally
		gi := 0
		for _, forgeParam := range forgeFunc.Params {
			if forgeParam.IsSelf {
				continue
			}
			if gi < len(goFunc.Params) {
				lyricType := lyricTypeToGoString(forgeParam.Type)
				goType := goFunc.Params[gi].Type
				if !typesMatch(lyricType, goType) {
					paramName := forgeParam.Name
					if paramName == "" {
						paramName = fmt.Sprintf("#%d", gi+1)
					}
					result.add(Error, lyricFile, goFile, fmt.Sprintf("%s: param %s type mismatch: .lyric=%s, Go=%s", context, paramName, lyricType, goType))
				}
			}
			gi++
		}
	}

	// Check return types
	forgeReturnCount := 0
	var forgeReturnStr string
	if forgeFunc.ReturnType != nil {
		forgeReturnStr = lyricTypeToGoString(*forgeFunc.ReturnType)
		if forgeFunc.ReturnType.Kind == lyricast.TypeTuple {
			if forgeFunc.ReturnType.Data != nil {
				tt := forgeFunc.ReturnType.Data.(lyricast.TupleType)
				forgeReturnCount = len(tt.Fields)
			}
		} else if forgeFunc.ReturnType.Kind == lyricast.TypeUnit {
			forgeReturnCount = 0
		} else {
			forgeReturnCount = 1
		}
	}
	goReturnCount := len(goFunc.Returns)

	if forgeReturnCount != goReturnCount {
		result.add(Error, lyricFile, goFile, fmt.Sprintf("%s: return count mismatch: .lyric=%d, Go=%d", context, forgeReturnCount, goReturnCount))
	} else if forgeReturnCount == 1 && goReturnCount == 1 {
		if !typesMatch(forgeReturnStr, goFunc.Returns[0]) {
			result.add(Error, lyricFile, goFile, fmt.Sprintf("%s: return type mismatch: .lyric=%s, Go=%s", context, forgeReturnStr, goFunc.Returns[0]))
		}
	} else if forgeReturnCount > 1 && forgeFunc.ReturnType != nil && forgeFunc.ReturnType.Kind == lyricast.TypeTuple {
		// Element-wise comparison for tuple returns
		tt := forgeFunc.ReturnType.Data.(lyricast.TupleType)
		for i, field := range tt.Fields {
			if i < len(goFunc.Returns) {
				forgeElemStr := lyricTypeToGoString(field.Type)
				if !typesMatch(forgeElemStr, goFunc.Returns[i]) {
					result.add(Error, lyricFile, goFile, fmt.Sprintf("%s: return #%d type mismatch: .lyric=%s, Go=%s", context, i+1, forgeElemStr, goFunc.Returns[i]))
				}
			}
		}
	}
}

func toGoMethodName(name string) string {
	return snakeToPascal(name)
}

// snakeToPascal converts snake_case to PascalCase.
func snakeToPascal(s string) string {
	parts := strings.Split(s, "_")
	var result strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		result.WriteString(strings.ToUpper(p[:1]))
		result.WriteString(p[1:])
	}
	return result.String()
}

// snakeToCamel converts snake_case to camelCase.
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	var result strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i == 0 {
			result.WriteString(p)
		} else {
			result.WriteString(strings.ToUpper(p[:1]))
			result.WriteString(p[1:])
		}
	}
	return result.String()
}
