// Package golang extracts declarations from Go source files using go/parser.
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

// ExtractDir parses all Go files in a directory and returns the exported API.
func ExtractDir(dir string) (*extract.PackageInfo, error) {
	fset := token.NewFileSet()
	pkgs, err := goparser.ParseDir(fset, dir, func(info os.FileInfo) bool {
		name := info.Name()
		return !strings.HasSuffix(name, "_test.go") && strings.HasSuffix(name, ".go")
	}, goparser.ParseComments)
	if err != nil {
		return nil, err
	}

	info := extract.NewPackageInfo("")
	for pkgName, pkg := range pkgs {
		if info.Name == "" {
			info.Name = pkgName
		}
		for _, file := range pkg.Files {
			extractFromFile(fset, file, info)
		}
	}
	return info, nil
}

// ExtractFiles parses specific Go files and returns the exported API.
func ExtractFiles(paths []string) (*extract.PackageInfo, error) {
	fset := token.NewFileSet()
	info := extract.NewPackageInfo("")

	for _, path := range paths {
		file, err := goparser.ParseFile(fset, path, nil, goparser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		if info.Name == "" {
			info.Name = file.Name.Name
		}
		extractFromFile(fset, file, info)
	}
	return info, nil
}

// ExtractSource parses Go source from a string and returns declarations.
// This is used for parsing .go.lyric understanding files.
func ExtractSource(src string, filename string) (*extract.PackageInfo, error) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, filename, src, goparser.ParseComments)
	if err != nil {
		return nil, err
	}
	info := extract.NewPackageInfo(file.Name.Name)
	extractFromFile(fset, file, info)
	return info, nil
}

func extractFromFile(fset *token.FileSet, file *ast.File, info *extract.PackageInfo) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					doc := docText(ts.Doc, d.Doc)
					pos := fset.Position(ts.Pos())
					switch t := ts.Type.(type) {
					case *ast.StructType:
						si := extract.NewStructInfo()
						si.Doc = doc
						si.File = filepath.Base(pos.Filename)
						si.Line = pos.Line
						if t.Fields != nil {
							for _, f := range t.Fields.List {
								typStr := TypeString(f.Type)
								for _, name := range f.Names {
									si.SetField(name.Name, typStr)
								}
							}
						}
						info.Structs[ts.Name.Name] = si

					case *ast.InterfaceType:
						ii := extract.NewInterfaceInfo()
						ii.Doc = doc
						ii.File = filepath.Base(pos.Filename)
						ii.Line = pos.Line
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
						info.TypeDefs[ts.Name.Name] = &extract.TypeDefInfo{
							Underlying: TypeString(ts.Type),
							Doc:        doc,
							File:       filepath.Base(pos.Filename),
							Line:       pos.Line,
						}
					}
				}
			}

		case *ast.FuncDecl:
			fi := extractFuncInfo(fset, d)
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recvType := receiverTypeName(d.Recv.List[0].Type)
				if recvType != "" {
					si, ok := info.Structs[recvType]
					if !ok {
						si = extract.NewStructInfo()
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

func extractFuncInfo(fset *token.FileSet, d *ast.FuncDecl) *extract.FuncInfo {
	fi := extractFuncType(d.Type)
	fi.Doc = docText(d.Doc, nil)
	pos := fset.Position(d.Pos())
	fi.File = filepath.Base(pos.Filename)
	fi.Line = pos.Line
	return fi
}

func extractFuncType(ft *ast.FuncType) *extract.FuncInfo {
	fi := &extract.FuncInfo{}
	if ft.Params != nil {
		for _, p := range ft.Params.List {
			typStr := TypeString(p.Type)
			if len(p.Names) == 0 {
				fi.Params = append(fi.Params, extract.ParamInfo{Type: typStr})
			} else {
				for _, name := range p.Names {
					fi.Params = append(fi.Params, extract.ParamInfo{Name: name.Name, Type: typStr})
				}
			}
		}
	}
	if ft.Results != nil {
		for _, r := range ft.Results.List {
			typStr := TypeString(r.Type)
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

// TypeString converts a Go AST type expression to a string representation.
func TypeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + TypeString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + TypeString(t.Elt)
		}
		return "[...]" + TypeString(t.Elt)
	case *ast.MapType:
		return "map[" + TypeString(t.Key) + "]" + TypeString(t.Value)
	case *ast.SelectorExpr:
		return TypeString(t.X) + "." + t.Sel.Name
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return funcTypeString(t)
	case *ast.ChanType:
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + TypeString(t.Value)
		case ast.RECV:
			return "<-chan " + TypeString(t.Value)
		default:
			return "chan " + TypeString(t.Value)
		}
	case *ast.Ellipsis:
		return "..." + TypeString(t.Elt)
	case *ast.IndexExpr:
		return TypeString(t.X) + "[" + TypeString(t.Index) + "]"
	case *ast.IndexListExpr:
		var parts []string
		for _, idx := range t.Indices {
			parts = append(parts, TypeString(idx))
		}
		return TypeString(t.X) + "[" + strings.Join(parts, ", ") + "]"
	case *ast.StructType:
		return "struct{...}"
	case *ast.ParenExpr:
		return "(" + TypeString(t.X) + ")"
	default:
		return fmt.Sprintf("<%T>", expr)
	}
}

func funcTypeString(ft *ast.FuncType) string {
	var b strings.Builder
	b.WriteString("func(")
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

func fieldListString(fl *ast.FieldList) string {
	var parts []string
	for _, f := range fl.List {
		ts := TypeString(f.Type)
		if len(f.Names) == 0 {
			parts = append(parts, ts)
		} else {
			var names []string
			for _, n := range f.Names {
				names = append(names, n.Name)
			}
			parts = append(parts, strings.Join(names, ", ")+" "+ts)
		}
	}
	return strings.Join(parts, ", ")
}

// ScanSourceFiles returns all non-test .go files in a directory, sorted.
//
// Fallback: if the directory contains only *_test.go files (e.g. an
// integration-test directory), those are returned instead so the extractor
// can produce a useful .lyric. The production-vs-test distinction is
// preserved in normal mixed dirs — _test.go files are only included when
// there are no production files. See TODO.md ("Test-only directories
// rejected by Go extractor").
func ScanSourceFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files, testOnly []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			testOnly = append(testOnly, name)
			continue
		}
		files = append(files, name)
	}
	if len(files) == 0 && len(testOnly) > 0 {
		files = testOnly
	}
	sort.Strings(files)
	return files, nil
}

// BuildSignature produces a Go function signature string from a FuncDecl.
func BuildSignature(fn *ast.FuncDecl, fset *token.FileSet) string {
	var b strings.Builder
	b.WriteString("func ")
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recv := fn.Recv.List[0]
		b.WriteString("(")
		if len(recv.Names) > 0 {
			b.WriteString(recv.Names[0].Name)
			b.WriteString(" ")
		}
		b.WriteString(TypeString(recv.Type))
		b.WriteString(") ")
	}
	b.WriteString(fn.Name.Name)
	if fn.Type.TypeParams != nil && len(fn.Type.TypeParams.List) > 0 {
		b.WriteString("[")
		b.WriteString(fieldListString(fn.Type.TypeParams))
		b.WriteString("]")
	}
	b.WriteString("(")
	if fn.Type.Params != nil {
		b.WriteString(fieldListString(fn.Type.Params))
	}
	b.WriteString(")")
	if fn.Type.Results != nil {
		results := fn.Type.Results.List
		if len(results) == 1 && len(results[0].Names) == 0 {
			b.WriteString(" ")
			b.WriteString(TypeString(results[0].Type))
		} else if len(results) > 0 {
			b.WriteString(" (")
			b.WriteString(fieldListString(fn.Type.Results))
			b.WriteString(")")
		}
	}
	return b.String()
}

// IsExported returns true if a Go name starts with an uppercase letter.
func IsExported(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

// FindModulePath walks up from dir looking for go.mod and returns the module path.
func FindModulePath(dir string) string {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		data, err := os.ReadFile(filepath.Join(absDir, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					return strings.TrimSpace(strings.TrimPrefix(line, "module "))
				}
			}
		}
		parent := filepath.Dir(absDir)
		if parent == absDir {
			return ""
		}
		absDir = parent
	}
}
