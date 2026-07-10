// Package extract defines the common interface for language-specific extractors.
// Each extractor parses source files in a target language and produces a
// PackageInfo describing the exported declarations.
//
// As of Phase 1 of the rich-doc upgrade (see cr/docs/rich-doc-upgrade-plan.md),
// PackageInfo carries the eight rich-doc sections needed for CDD parity with
// the legacy .forge format: module-level why, named doc blocks, named
// invariant blocks (with procedural/verified-by metadata), per-decl why,
// per-field doc, and source location bindings. Signatures themselves are
// verbatim native-language text held as opaque strings (FieldInfo.SignatureText
// for fields; FuncInfo for methods/functions still holds structured params).
package extract

import (
	"fmt"
	"sort"
	"strings"
)

// PackageInfo is the language-agnostic representation of a package's exported
// API — the "narrow waist" every extractor produces and every consumer reads.
type PackageInfo struct {
	Name       string
	Structs    map[string]*StructInfo
	Interfaces map[string]*InterfaceInfo
	Functions  map[string]*FuncInfo
	TypeDefs   map[string]*TypeDefInfo

	// Rich-doc additions (Phase 1).
	ModuleWhy    string      // module-level "why" prose
	ModuleSource []string    // module-level `source: ["a.go", "b.go"]` file list
	Docs         []DocBlock  // named doc "..." blocks (e.g. "Architecture")
	Invariants   []Invariant // named invariant "..." blocks
}

// DocBlock is a titled prose section (e.g. doc "Architecture":).
type DocBlock struct {
	Title   string
	Content string
}

// Invariant is a titled invariant block with optional verification metadata.
type Invariant struct {
	Title      string
	Content    string
	Procedural bool     // marked `procedural` — cannot be mechanically tested
	VerifiedBy []string // test function names that verify this invariant
}

// StructInfo describes a struct/class with fields and methods.
type StructInfo struct {
	Fields  []FieldInfo // ordered list of fields; was map[string]string before Phase 1
	Methods map[string]*FuncInfo
	Doc     string // leading doc comment, if any (legacy; for richer docs use Why)
	File    string // source file name (basename)
	Line    int    // line number in source file
	IsClass bool   // true if this is a class (reference type), false for struct (value type)

	// Rich-doc additions (Phase 1).
	Why    string // per-decl "why" prose
	Source string // canonical "file:line" reference; refreshed by `lyre update`
}

// FieldInfo describes a single struct/class field.
// SignatureText is the verbatim native-language type text (e.g. "[]error" for
// Go, "string[]" for TypeScript, "[Sym]" for Lyric); Lyre treats it as an
// opaque string and only string-compares it (modulo whitespace) against the
// extractor's output.
type FieldInfo struct {
	Name          string
	SignatureText string
	Doc           string // per-field doc (rich-doc addition)
}

// InterfaceInfo describes an interface with methods.
type InterfaceInfo struct {
	Methods map[string]*FuncInfo
	Doc     string
	File    string
	Line    int

	// Rich-doc additions (Phase 1).
	Why    string // per-decl "why" prose
	Source string // canonical "file:line" reference; refreshed by `lyre update`
}

// FuncInfo describes a function or method signature.
//
// SignatureText is the verbatim native-language signature text including the
// function/method name (e.g. "Foo(x int) error" for Go, "Foo(x: int) -> error"
// for Lyric). It is the canonical form for `.lyric` v2 round-trip — writers
// emit it as the `func`/`method` block head's rest-of-line and parsers store
// the rest-of-line back into it. Params/Returns are extractor-internal
// structured forms that do NOT round-trip through `.lyric` (per spec §1:
// signature payloads are opaque verbatim text).
type FuncInfo struct {
	SignatureText string      // verbatim native signature incl. name (Phase 2 round-trip)
	Params        []ParamInfo // extractor-internal; not round-tripped through .lyric
	Returns       []string    // extractor-internal; not round-tripped through .lyric
	Doc           string
	File          string
	Line          int

	// Rich-doc additions (Phase 1).
	Why    string // per-decl "why" prose
	Source string
}

// ParamInfo describes a function parameter.
type ParamInfo struct {
	Name  string
	Type  string
	IsMut bool
}

// TypeDefInfo describes a type alias or newtype and its underlying type.
type TypeDefInfo struct {
	Underlying string // the underlying type string, if simple
	Doc        string
	File       string
	Line       int

	// Methods declared on this named type (e.g. Go's `func (s Severity)
	// String() string` on `type Severity int`). Keyed by method name. A
	// typedef with methods is the common Go "stringer" pattern; representing
	// them here avoids synthesizing a phantom struct to hold the method.
	Methods map[string]*FuncInfo

	// Rich-doc additions (Phase 1).
	Why    string // per-decl "why" prose
	Source string // canonical "file:line" reference; round-trips through .lyric
}

// LDDMeta holds the LDD-specific metadata parsed from structured comments.
type LDDMeta struct {
	Source []string // source files this understanding file covers
	Why    string   // human explanation of what this package does
	Lang   string   // detected language (go, python, typescript, lyric)
}

// --- StructInfo field helpers ----------------------------------------------
//
// These exist to keep callers minimally changed while Fields transitions from
// map[string]string to []FieldInfo. Phase 3 (per-language extractor rewrites)
// will delete most of the callers that still use these helpers.

// FieldSig returns the SignatureText of the named field, if present.
func (s *StructInfo) FieldSig(name string) (string, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f.SignatureText, true
		}
	}
	return "", false
}

// HasField reports whether the struct has a field with the given name.
func (s *StructInfo) HasField(name string) bool {
	_, ok := s.FieldSig(name)
	return ok
}

// SetField appends or updates a field by name. Order is preserved on update;
// new fields are appended in call order. Use SetFieldDoc to add the per-field
// doc string; SetField on its own leaves Doc untouched.
func (s *StructInfo) SetField(name, sig string) {
	for i := range s.Fields {
		if s.Fields[i].Name == name {
			s.Fields[i].SignatureText = sig
			return
		}
	}
	s.Fields = append(s.Fields, FieldInfo{Name: name, SignatureText: sig})
}

// SetFieldDoc sets the per-field doc for the named field. Creates the field
// (with empty SignatureText) if it doesn't exist.
func (s *StructInfo) SetFieldDoc(name, doc string) {
	for i := range s.Fields {
		if s.Fields[i].Name == name {
			s.Fields[i].Doc = doc
			return
		}
	}
	s.Fields = append(s.Fields, FieldInfo{Name: name, Doc: doc})
}

// FieldNames returns the field names in source order.
func (s *StructInfo) FieldNames() []string {
	names := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		names = append(names, f.Name)
	}
	return names
}

// SortedFieldsByName returns a copy of the field slice sorted alphabetically
// by name. Used by legacy emit paths (Phase 3 will delete most callers).
func SortedFieldsByName(fields []FieldInfo) []FieldInfo {
	out := make([]FieldInfo, len(fields))
	copy(out, fields)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
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

// SanitizeModuleName converts a directory base name into a valid Lyric
// identifier matching the parser's `leadingIdentifier` grammar
// ([A-Za-z_][A-Za-z0-9_]*). Any rune that is not an ASCII letter, digit, or
// underscore is mapped to `_` (so "foo-bar" → "foo_bar", "foo.bar" →
// "foo_bar"). A leading digit gets a leading `_` prefix ("123abc" →
// "_123abc"). An empty input (or input that sanitizes to "") returns
// "_module" so the result is always a valid identifier.
//
// Used by language extractors (TypeScript, Python, Lyric) to derive the
// `module` identifier from `filepath.Base(absDir)`. Go does not need this
// because it derives the module name from the source's `package` declaration,
// which Go itself already constrains to a valid identifier.
func SanitizeModuleName(name string) string {
	if name == "" {
		return "_module"
	}
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			out = append(out, c)
		case c >= '0' && c <= '9':
			if len(out) == 0 {
				out = append(out, '_')
			}
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "_module"
	}
	return string(out)
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

// NewStructInfo creates an initialized StructInfo. Fields starts empty; use
// SetField or append directly.
func NewStructInfo() *StructInfo {
	return &StructInfo{
		Methods: make(map[string]*FuncInfo),
	}
}

// NewInterfaceInfo creates an initialized InterfaceInfo.
func NewInterfaceInfo() *InterfaceInfo {
	return &InterfaceInfo{
		Methods: make(map[string]*FuncInfo),
	}
}

// CleanDocLine extracts a one-line summary from a multi-line native-source
// doc comment. It picks the first non-empty line, strips common comment
// markers (`// `, `# `, `* `, `/** `), trims whitespace, and collapses
// embedded whitespace. Returns "" if no usable content is found.
//
// The one-line reduction is deliberate: the `.lyric` `why:` slot is a single
// quoted string (the writer does not escape newlines), and the file is meant
// to be a compact review aid — the full prose lives in the source comment,
// which is the source of truth.
func CleanDocLine(doc string) string {
	for _, raw := range strings.Split(doc, "\n") {
		line := strings.TrimSpace(raw)
		// Strip leading comment markers.
		for {
			trimmed := line
			for _, prefix := range []string{"// ", "//", "# ", "#", "/**", "/*", "*/", "* ", "*"} {
				if strings.HasPrefix(trimmed, prefix) {
					trimmed = strings.TrimSpace(trimmed[len(prefix):])
				}
			}
			if trimmed == line {
				break
			}
			line = trimmed
		}
		if line == "" {
			continue
		}
		// Collapse internal whitespace.
		return strings.Join(strings.Fields(line), " ")
	}
	return ""
}

// PreferFresh returns fresh when it is non-empty, else keeps existing. Source
// is the source of truth for per-decl prose, so `lyre update`'s merge uses this
// to refresh `why:` from the current source comment — while still preserving
// hand-written prose on decls that have no source comment (fresh == "").
func PreferFresh(existing, fresh string) string {
	if strings.TrimSpace(fresh) != "" {
		return fresh
	}
	return existing
}

// PruneOrphans removes declarations present in `existing` but absent from
// `fresh` (the just-extracted source view), returning the removed declaration
// labels sorted for deterministic output.
//
// It is the destructive half of `lyre update`: mergeFreshIntoExisting refreshes
// and ADDS decls from source, PruneOrphans DELETES decls that source no longer
// exports, so `update` keeps the .lyric in exact sync with the source's
// exported surface (prune-by-default). Struct/interface fields are already
// reconciled in place by mergeFreshIntoExisting (it rebuilds Fields from
// fresh); PruneOrphans handles top-level decls (structs/classes, interfaces,
// functions, typedefs) and the methods within surviving structs and interfaces.
//
// Human prose on a pruned decl is discarded with it: the decl is gone from
// source, so its documentation is stale by definition. Because a failed
// extraction returns an error before merge/prune run, this never sees a
// spuriously empty `fresh`.
func PruneOrphans(existing, fresh *PackageInfo) []string {
	if existing == nil || fresh == nil {
		return nil
	}
	var removed []string

	for name, es := range existing.Structs {
		fs, ok := fresh.Structs[name]
		if !ok {
			kind := "struct"
			if es.IsClass {
				kind = "class"
			}
			removed = append(removed, kind+" "+name)
			delete(existing.Structs, name)
			continue
		}
		for mn := range es.Methods {
			if _, ok := fs.Methods[mn]; !ok {
				removed = append(removed, fmt.Sprintf("method %s.%s", name, mn))
				delete(es.Methods, mn)
			}
		}
	}

	for name, ei := range existing.Interfaces {
		fi, ok := fresh.Interfaces[name]
		if !ok {
			removed = append(removed, "interface "+name)
			delete(existing.Interfaces, name)
			continue
		}
		for mn := range ei.Methods {
			if _, ok := fi.Methods[mn]; !ok {
				removed = append(removed, fmt.Sprintf("interface %s.%s", name, mn))
				delete(ei.Methods, mn)
			}
		}
	}

	for name := range existing.Functions {
		if _, ok := fresh.Functions[name]; !ok {
			removed = append(removed, "func "+name)
			delete(existing.Functions, name)
		}
	}

	for name := range existing.TypeDefs {
		ft, ok := fresh.TypeDefs[name]
		if !ok {
			removed = append(removed, "typedef "+name)
			delete(existing.TypeDefs, name)
			continue
		}
		et := existing.TypeDefs[name]
		for mn := range et.Methods {
			if _, ok := ft.Methods[mn]; !ok {
				removed = append(removed, fmt.Sprintf("method %s.%s", name, mn))
				delete(et.Methods, mn)
			}
		}
	}

	sort.Strings(removed)
	return removed
}

// SeedWhyFromDoc populates each declaration's one-line `Why` from its scraped
// native-source `Doc` comment, and collapses per-field `Doc` to a single line.
// It is called at the end of every language extractor so that the source code
// is the source of truth for per-decl `why:` prose: teammates read the native
// `.go`/`.py`/`.ts` comments; the `.lyric` mirrors their first line.
//
// It only fills empty `Why` slots (never clobbers prose already present on the
// passed-in PackageInfo). Freshly extracted PackageInfos always have empty
// `Why`, so this seeds from the source comment; `lyre update` reconciles
// against an existing file separately (see mergeFreshIntoExisting), where the
// fresh source comment wins. Module-level `why`, `doc` blocks, and invariants
// are NOT touched here — those stay human/design-doc curated.
func SeedWhyFromDoc(p *PackageInfo) {
	if p == nil {
		return
	}
	seed := func(why *string, doc string) {
		if strings.TrimSpace(*why) == "" {
			if line := CleanDocLine(doc); line != "" {
				*why = line
			}
		}
	}
	for _, s := range p.Structs {
		seed(&s.Why, s.Doc)
		for _, m := range s.Methods {
			seed(&m.Why, m.Doc)
		}
		for i := range s.Fields {
			if s.Fields[i].Doc != "" {
				s.Fields[i].Doc = CleanDocLine(s.Fields[i].Doc)
			}
		}
	}
	for _, ifc := range p.Interfaces {
		seed(&ifc.Why, ifc.Doc)
		for _, m := range ifc.Methods {
			seed(&m.Why, m.Doc)
		}
	}
	for _, fn := range p.Functions {
		seed(&fn.Why, fn.Doc)
	}
	for _, td := range p.TypeDefs {
		seed(&td.Why, td.Doc)
		for _, m := range td.Methods {
			seed(&m.Why, m.Doc)
		}
	}
}
