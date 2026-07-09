// Package cdd implements the parser and writer for the `.lyric` v2 format
// — the persistence layer for Context-Driven Development.
//
// The format is a small indent-significant DSL whose payload lines (field
// types, method signatures, function signatures) are verbatim native-language
// text treated as opaque strings. See `pkg/cdd/spec.md` for the canonical
// grammar and `~/projects/lyric/cr/docs/context-driven-development.md`
// for the methodology this format implements.
//
// The package exposes two entry points:
//
//	func Write(p *extract.PackageInfo) string
//	func Parse(text string, filename string) (*extract.PackageInfo, error)
//
// `Parse(Write(p))` is structurally equal to p for any well-formed
// PackageInfo whose round-trippable fields are set. Round-trippable fields
// are: PackageInfo.{Name, ModuleWhy, ModuleSource, Docs, Invariants,
// Structs, Interfaces, Functions, TypeDefs}; StructInfo.{IsClass, Fields,
// Methods, Why, Source, File, Line}; InterfaceInfo.{Methods, Why, Source,
// File, Line}; FuncInfo.{SignatureText, Why, Source, File, Line};
// TypeDefInfo.{Underlying, Why, Source, File, Line}; FieldInfo.{Name,
// SignatureText, Doc}; Invariant.{Title, Content, Procedural, VerifiedBy};
// DocBlock.{Title, Content}.
//
// Fields not round-tripped through `.lyric`: FuncInfo.{Params, Returns,
// Doc}; StructInfo.Doc; InterfaceInfo.Doc; TypeDefInfo.{Doc, Underlying
// for typedefs where typedef block omits the underlying type}. These are
// extractor-internal forms not exposed in the `.lyric` surface.
package cdd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/waywardgeek/lyre/pkg/extract"
)

// Write serializes p to its canonical, deterministic `.lyric` v2 representation.
// The same PackageInfo produces byte-identical output every time; there is
// exactly one trailing newline and no line has trailing whitespace.
func Write(p *extract.PackageInfo) string {
	var w writer
	w.module(p)
	return w.b.String()
}

type writer struct {
	b strings.Builder
}

// indent writes 2*n spaces.
func (w *writer) indent(n int) {
	for i := 0; i < n; i++ {
		w.b.WriteString("  ")
	}
}

// line writes "  "*indent + content + "\n", trimming trailing spaces.
func (w *writer) line(ind int, content string) {
	w.indent(ind)
	w.b.WriteString(strings.TrimRight(content, " \t"))
	w.b.WriteByte('\n')
}

// blank writes a single "\n" line separator.
func (w *writer) blank() {
	w.b.WriteByte('\n')
}

// flattenSig collapses any internal newlines and runs of whitespace in a
// signature/type string into single spaces, then trims. The `.lyric` v2
// grammar is line-oriented: `field <name>: <sig>`, `method <sig>`, `func
// <sig>`, and `typedef <name>: <underlying>` MUST occupy exactly one physical
// line, because the parser interprets any deeper-indented follow-up line as a
// child key in the enclosing block. Native-language extractors (notably the
// TypeScript extractor, which copies inline-object type literals verbatim
// from source) can produce signatures containing embedded newlines; emitting
// those would write a file the parser then rejects on its very next read —
// the round-trip property Parse(Write(p)) ≡ p (spec §1) requires this
// flattening at the writer boundary. See pkg/cdd/writer_test.go:
// TestWrite_MultiLineSignatureFlattened.
func flattenSig(s string) string {
	if !strings.ContainsAny(s, "\n\r\t") {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	inSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\n' || c == '\r' || c == '\t' || c == ' ' {
			if !inSpace {
				sb.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		sb.WriteByte(c)
		inSpace = false
	}
	return strings.TrimSpace(sb.String())
}

// quote returns a double-quoted form of s with minimal escapes: only `"` and
// `\` are escaped, per spec §12 decision 5.
func quote(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// heredoc emits a heredoc at indent `ind`. The opening and closing """ lines
// are at indent ind; body lines are at indent ind (with extra indent inside
// preserved verbatim).
func (w *writer) heredoc(ind int, content string) {
	w.line(ind, `"""`)
	if content != "" {
		for _, ln := range strings.Split(content, "\n") {
			// Body lines are emitted with the heredoc's own indent as the
			// leading whitespace; the spec §5 strip-by-own-indent rule
			// inverts this on parse.
			if ln == "" {
				// Preserve blank lines without trailing whitespace.
				w.b.WriteByte('\n')
				continue
			}
			w.indent(ind)
			w.b.WriteString(ln)
			w.b.WriteByte('\n')
		}
	}
	w.line(ind, `"""`)
}

// module emits the top-level module block (indent 0 head, indent 1 body).
func (w *writer) module(p *extract.PackageInfo) {
	w.line(0, "module "+p.Name)

	// Track whether anything has been emitted at the module body level so we
	// know when to insert blank-line separators before subsequent blocks.
	any := false

	// 1. module-level source: list
	if len(p.ModuleSource) > 0 {
		raw, _ := json.Marshal(p.ModuleSource)
		w.line(1, "source: "+string(raw))
		any = true
	}
	// 2. module-level why:
	if p.ModuleWhy != "" {
		w.line(1, "why: "+quote(p.ModuleWhy))
		any = true
	}

	// 3. doc blocks (in PackageInfo.Docs order)
	for _, d := range p.Docs {
		if any {
			w.blank()
		}
		w.docBlock(1, d)
		any = true
	}

	// 4. invariant blocks (in PackageInfo.Invariants order)
	for _, inv := range p.Invariants {
		if any {
			w.blank()
		}
		w.invariantBlock(1, inv)
		any = true
	}

	// 5. all decl blocks, sorted into one stream
	for _, d := range collectDecls(p) {
		if any {
			w.blank()
		}
		w.decl(1, d)
		any = true
	}

	_ = any
}

func (w *writer) docBlock(ind int, d extract.DocBlock) {
	w.line(ind, "doc "+quote(d.Title)+":")
	w.heredoc(ind+1, d.Content)
}

func (w *writer) invariantBlock(ind int, inv extract.Invariant) {
	w.line(ind, "invariant "+quote(inv.Title)+":")
	// verified-by: lines, sorted alphabetically (per spec §9)
	if len(inv.VerifiedBy) > 0 {
		vb := append([]string(nil), inv.VerifiedBy...)
		sort.Strings(vb)
		w.line(ind+1, "verified-by: "+strings.Join(vb, ", "))
	}
	if inv.Procedural {
		w.line(ind+1, "procedural")
	}
	w.heredoc(ind+1, inv.Content)
}

// declRef is a unified handle on any decl, used for sorting.
type declRef struct {
	kind string // "class" / "struct" / "interface" / "func" / "typedef"
	name string
	file string
	line int
	// exactly one of these is set:
	s *extract.StructInfo
	i *extract.InterfaceInfo
	f *extract.FuncInfo
	t *extract.TypeDefInfo
}

// collectDecls gathers all declarations (classes/structs/interfaces/funcs/
// typedefs) into one slice sorted per spec §9: by (file, line) if every decl
// has both, else alphabetically by name.
func collectDecls(p *extract.PackageInfo) []declRef {
	var out []declRef
	for name, s := range p.Structs {
		kind := "struct"
		if s.IsClass {
			kind = "class"
		}
		out = append(out, declRef{kind: kind, name: name, file: s.File, line: s.Line, s: s})
	}
	for name, i := range p.Interfaces {
		out = append(out, declRef{kind: "interface", name: name, file: i.File, line: i.Line, i: i})
	}
	for name, f := range p.Functions {
		out = append(out, declRef{kind: "func", name: name, file: f.File, line: f.Line, f: f})
	}
	for name, t := range p.TypeDefs {
		out = append(out, declRef{kind: "typedef", name: name, file: t.File, line: t.Line, t: t})
	}

	allPositioned := true
	for _, d := range out {
		if d.file == "" || d.line == 0 {
			allPositioned = false
			break
		}
	}
	if allPositioned {
		sort.SliceStable(out, func(a, b int) bool {
			if out[a].file != out[b].file {
				return out[a].file < out[b].file
			}
			if out[a].line != out[b].line {
				return out[a].line < out[b].line
			}
			return out[a].name < out[b].name
		})
	} else {
		sort.SliceStable(out, func(a, b int) bool {
			return out[a].name < out[b].name
		})
	}
	return out
}

func (w *writer) decl(ind int, d declRef) {
	switch d.kind {
	case "class", "struct", "enum":
		w.classOrStruct(ind, d.kind, d.name, d.s)
	case "interface":
		w.iface(ind, d.name, d.i)
	case "func":
		w.funcDecl(ind, d.name, d.f)
	case "typedef":
		w.typedefDecl(ind, d.name, d.t)
	default:
		panic(fmt.Sprintf("cdd.writer: unknown decl kind %q", d.kind))
	}
}

func (w *writer) classOrStruct(ind int, kind, name string, s *extract.StructInfo) {
	w.line(ind, kind+" "+name)
	w.declMeta(ind+1, s.Source, s.Why)

	// Fields in source order (as stored in the slice).
	for _, f := range s.Fields {
		w.fieldBlock(ind+1, f)
	}

	// Methods: sort by (file,line) if all positioned, else alphabetically.
	w.methods(ind+1, s.Methods)
}

func (w *writer) iface(ind int, name string, i *extract.InterfaceInfo) {
	w.line(ind, "interface "+name)
	w.declMeta(ind+1, i.Source, i.Why)
	w.methods(ind+1, i.Methods)
}

func (w *writer) funcDecl(ind int, name string, f *extract.FuncInfo) {
	sig := flattenSig(f.SignatureText)
	if sig == "" {
		sig = name + "()"
	}
	w.line(ind, "func "+sig)
	w.declMeta(ind+1, f.Source, f.Why)
}

func (w *writer) typedefDecl(ind int, name string, t *extract.TypeDefInfo) {
	if t.Underlying != "" {
		w.line(ind, "typedef "+name+": "+flattenSig(t.Underlying))
	} else {
		w.line(ind, "typedef "+name)
	}
	w.declMeta(ind+1, t.Source, t.Why)
}

// declMeta emits source: and why: lines at the given indent in canonical order.
func (w *writer) declMeta(ind int, source, why string) {
	if source != "" {
		w.line(ind, "source: "+source)
	}
	if why != "" {
		w.line(ind, "why: "+quote(why))
	}
}

func (w *writer) fieldBlock(ind int, f extract.FieldInfo) {
	head := "field " + f.Name
	if f.SignatureText != "" {
		head += ": " + flattenSig(f.SignatureText)
	}
	w.line(ind, head)
	if f.Doc != "" {
		w.line(ind+1, "doc: "+quote(f.Doc))
	}
}

// methods emits each method block, sorted by (file,line) if all are positioned,
// else alphabetically.
func (w *writer) methods(ind int, ms map[string]*extract.FuncInfo) {
	if len(ms) == 0 {
		return
	}
	type mref struct {
		name string
		f    *extract.FuncInfo
	}
	refs := make([]mref, 0, len(ms))
	for name, f := range ms {
		refs = append(refs, mref{name, f})
	}
	allPositioned := true
	for _, r := range refs {
		if r.f.File == "" || r.f.Line == 0 {
			allPositioned = false
			break
		}
	}
	if allPositioned {
		sort.SliceStable(refs, func(a, b int) bool {
			if refs[a].f.File != refs[b].f.File {
				return refs[a].f.File < refs[b].f.File
			}
			if refs[a].f.Line != refs[b].f.Line {
				return refs[a].f.Line < refs[b].f.Line
			}
			return refs[a].name < refs[b].name
		})
	} else {
		sort.SliceStable(refs, func(a, b int) bool {
			return refs[a].name < refs[b].name
		})
	}
	for _, r := range refs {
		sig := flattenSig(r.f.SignatureText)
		if sig == "" {
			sig = r.name + "()"
		}
		w.line(ind, "method "+sig)
		w.declMeta(ind+1, r.f.Source, r.f.Why)
	}
}
