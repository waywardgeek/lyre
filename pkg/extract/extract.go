// Package extract defines the common interface for language-specific extractors.
// Each extractor parses source files in a target language and produces a
// PackageInfo describing the exported declarations.
package extract

// PackageInfo is the language-agnostic representation of a package's exported API.
type PackageInfo struct {
	Name       string
	Structs    map[string]*StructInfo
	Interfaces map[string]*InterfaceInfo
	Functions  map[string]*FuncInfo
	TypeDefs   map[string]*TypeDefInfo
}

// StructInfo describes a struct/class with fields and methods.
type StructInfo struct {
	Fields  map[string]string          // field name → type string (in source language syntax)
	Methods map[string]*FuncInfo
}

// InterfaceInfo describes an interface with methods.
type InterfaceInfo struct {
	Methods map[string]*FuncInfo
}

// FuncInfo describes a function or method signature.
type FuncInfo struct {
	Params  []ParamInfo
	Returns []string // type strings in source language syntax
}

// ParamInfo describes a function parameter.
type ParamInfo struct {
	Name string
	Type string
}

// TypeDefInfo describes a type alias or newtype.
type TypeDefInfo struct {
	Underlying string // the underlying type string, if simple
}

// LDDMeta holds the LDD-specific metadata parsed from structured comments.
type LDDMeta struct {
	Source []string // source files this understanding file covers
	Why    string   // human explanation of what this package does
	Lang   string   // detected language (go, python, typescript, lyric)
}

// DetectLanguage returns the source language from a compound .lyric extension.
// "agent.go.lyric" → "go", "parser.py.lyric" → "python", "checker.ly.lyric" → "lyric"
// Plain ".lyric" → "lyric" (default).
func DetectLanguage(filename string) string {
	// Strip the .lyric suffix to get the inner extension
	name := filename
	if len(name) > 6 && name[len(name)-6:] == ".lyric" {
		name = name[:len(name)-6]
	} else {
		return "lyric"
	}

	// Find the inner extension
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			ext := name[i+1:]
			switch ext {
			case "go":
				return "go"
			case "py", "pyi":
				return "python"
			case "ts", "d.ts":
				return "typescript"
			case "rs":
				return "rust"
			case "ly":
				return "lyric"
			default:
				return ext
			}
		}
		if name[i] == '/' || name[i] == '\\' {
			break
		}
	}
	return "lyric"
}

// NewPackageInfo creates an initialized PackageInfo.
func NewPackageInfo(name string) *PackageInfo {
	return &PackageInfo{
		Name:       name,
		Structs:    make(map[string]*StructInfo),
		Interfaces: make(map[string]*InterfaceInfo),
		Functions:  make(map[string]*FuncInfo),
		TypeDefs:   make(map[string]*TypeDefInfo),
	}
}

// NewStructInfo creates an initialized StructInfo.
func NewStructInfo() *StructInfo {
	return &StructInfo{
		Fields:  make(map[string]string),
		Methods: make(map[string]*FuncInfo),
	}
}

// NewInterfaceInfo creates an initialized InterfaceInfo.
func NewInterfaceInfo() *InterfaceInfo {
	return &InterfaceInfo{
		Methods: make(map[string]*FuncInfo),
	}
}
