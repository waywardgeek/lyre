// Package lint implements `lyre lint`: a language-agnostic linter that
// inspects a parsed *extract.PackageInfo produced by pkg/udd and reports
// recoverable quality issues distinct from the fatal syntactic errors
// raised by udd.Parse (spec §10).
//
// Lint is intentionally separate from pkg/verifier and the per-language
// VerifyXxx functions: those compare a .lyric file against native source
// (drift detection); lint inspects the .lyric file's own content
// (completeness / TODO-hygiene / test-name validity).
//
// Warning codes (rich-doc-upgrade-plan.md Phase 4):
//
//	W001  empty module-level why:
//	W002  no doc "Architecture" block
//	W003  no invariant blocks on a module with ≥1 class/struct/interface
//	      having ≥3 methods
//	W004  class/struct with ≥4 methods and no per-method why:
//	W005  struct with ≥3 fields and ≥1 enum-typed field, no per-field doc
//	W006  invariant without verified-by: AND without procedural marker
//	W007  verified-by: references test that doesn't exist (requires
//	      Opts.KnownTests to be non-nil; otherwise dormant)
//	W008  unfilled TODO placeholder anywhere in prose
package lint

import (
	"fmt"
	"sort"
	"strings"

	"github.com/waywardgeek/lyre/pkg/extract"
)

// Severity classifies lint findings.
type Severity int

const (
	SevWarning Severity = iota
	SevInfo
)

func (s Severity) String() string {
	switch s {
	case SevWarning:
		return "WARNING"
	case SevInfo:
		return "INFO"
	}
	return "UNKNOWN"
}

// Finding is a single lint report. Code is the W001-W008 token.
// Where is a human-readable scope hint (e.g. "module", "class Foo",
// `invariant "Three-Phase Ordering"`, `field bits`).
type Finding struct {
	Code     string
	Severity Severity
	File     string
	Where    string
	Message  string
}

func (f Finding) String() string {
	scope := f.File
	if f.Where != "" {
		scope = fmt.Sprintf("%s: %s", f.File, f.Where)
	}
	return fmt.Sprintf("[%s %s] %s: %s", f.Severity, f.Code, scope, f.Message)
}

// Result holds all findings from a lint run. Findings are sorted by
// (Code, Where) for deterministic output.
type Result struct {
	Findings []Finding
}

// WarningCount returns the number of SevWarning findings.
func (r *Result) WarningCount() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == SevWarning {
			n++
		}
	}
	return n
}

// Opts configures a lint run.
type Opts struct {
	// KnownTests is the set of test function names that exist in the project.
	// When non-nil, W007 fires for any verified-by: name not present.
	// When nil (the default for the CLI in v1), W007 is dormant.
	KnownTests map[string]bool
}

// Lint runs all enabled checks against p. lyricPath is recorded on each
// finding's File field (Lint does not re-read the file).
func Lint(p *extract.PackageInfo, lyricPath string, opts Opts) *Result {
	r := &Result{}
	if p == nil {
		return r
	}
	w001(r, p, lyricPath)
	w002(r, p, lyricPath)
	w003(r, p, lyricPath)
	w004(r, p, lyricPath)
	w005(r, p, lyricPath)
	w006(r, p, lyricPath)
	w007(r, p, lyricPath, opts)
	w008(r, p, lyricPath)
	sortFindings(r)
	return r
}

func (r *Result) add(code string, sev Severity, file, where, msg string) {
	r.Findings = append(r.Findings, Finding{
		Code: code, Severity: sev, File: file, Where: where, Message: msg,
	})
}

// --- checks ---------------------------------------------------------------

// W001: empty module-level why:
func w001(r *Result, p *extract.PackageInfo, file string) {
	if strings.TrimSpace(p.ModuleWhy) == "" {
		r.add("W001", SevWarning, file, "module",
			"module-level why: is empty — add a one-line purpose statement")
	}
}

// W002: no doc "Architecture" block.
func w002(r *Result, p *extract.PackageInfo, file string) {
	for _, d := range p.Docs {
		if strings.EqualFold(d.Title, "Architecture") {
			return
		}
	}
	r.add("W002", SevWarning, file, "module",
		`no doc "Architecture" block — every module should explain its high-level structure`)
}

// W003: ≥1 class/struct/interface with ≥3 methods, but module has no
// invariant blocks. Substantial state machines should document their
// invariants.
func w003(r *Result, p *extract.PackageInfo, file string) {
	if len(p.Invariants) > 0 {
		return
	}
	heavy := ""
	for _, name := range sortedKeysStruct(p.Structs) {
		if len(p.Structs[name].Methods) >= 3 {
			heavy = "class/struct " + name
			break
		}
	}
	if heavy == "" {
		for _, name := range sortedKeysIface(p.Interfaces) {
			if len(p.Interfaces[name].Methods) >= 3 {
				heavy = "interface " + name
				break
			}
		}
	}
	if heavy == "" {
		return
	}
	r.add("W003", SevWarning, file, "module",
		fmt.Sprintf("%s has ≥3 methods but module has no invariant blocks — document the invariants it maintains", heavy))
}

// W004: class/struct with ≥4 methods and no per-method why:.
func w004(r *Result, p *extract.PackageInfo, file string) {
	for _, name := range sortedKeysStruct(p.Structs) {
		s := p.Structs[name]
		if len(s.Methods) < 4 {
			continue
		}
		hasAny := false
		for _, m := range s.Methods {
			if strings.TrimSpace(m.Why) != "" {
				hasAny = true
				break
			}
		}
		if !hasAny {
			kind := "struct"
			if s.IsClass {
				kind = "class"
			}
			r.add("W004", SevWarning, file, fmt.Sprintf("%s %s", kind, name),
				fmt.Sprintf("%d methods and no per-method why: — explain at least the entry-point methods", len(s.Methods)))
		}
	}
}

// W005: struct with ≥3 fields and ≥1 enum-typed field has no per-field
// doc. Heuristic for "enum-typed": field SignatureText (trimmed)
// matches a key in p.TypeDefs.
func w005(r *Result, p *extract.PackageInfo, file string) {
	for _, name := range sortedKeysStruct(p.Structs) {
		s := p.Structs[name]
		if len(s.Fields) < 3 {
			continue
		}
		hasEnum := false
		for _, f := range s.Fields {
			if _, ok := p.TypeDefs[strings.TrimSpace(f.SignatureText)]; ok {
				hasEnum = true
				break
			}
		}
		if !hasEnum {
			continue
		}
		hasAnyDoc := false
		for _, f := range s.Fields {
			if strings.TrimSpace(f.Doc) != "" {
				hasAnyDoc = true
				break
			}
		}
		if !hasAnyDoc {
			kind := "struct"
			if s.IsClass {
				kind = "class"
			}
			r.add("W005", SevWarning, file, fmt.Sprintf("%s %s", kind, name),
				"has ≥3 fields including an enum-typed field but no per-field doc: — clarify semantic context")
		}
	}
}

// W006: invariant without verified-by: AND without procedural marker.
func w006(r *Result, p *extract.PackageInfo, file string) {
	for _, inv := range p.Invariants {
		if inv.Procedural {
			continue
		}
		if len(inv.VerifiedBy) > 0 {
			continue
		}
		r.add("W006", SevWarning, file, fmt.Sprintf("invariant %q", inv.Title),
			"has no verified-by: and is not marked procedural — invariants without tests are hypotheses, not facts")
	}
}

// W007: verified-by: references test that doesn't exist. Dormant when
// opts.KnownTests is nil.
func w007(r *Result, p *extract.PackageInfo, file string, opts Opts) {
	if opts.KnownTests == nil {
		return
	}
	for _, inv := range p.Invariants {
		for _, tn := range inv.VerifiedBy {
			tn = strings.TrimSpace(tn)
			if tn == "" {
				continue
			}
			if !opts.KnownTests[tn] {
				r.add("W007", SevWarning, file, fmt.Sprintf("invariant %q", inv.Title),
					fmt.Sprintf("verified-by: %s — no such test in the project", tn))
			}
		}
	}
}

// W008: unfilled TODO placeholder anywhere in prose. Case-sensitive
// substring match for "TODO" to keep the trigger strict and obvious.
func w008(r *Result, p *extract.PackageInfo, file string) {
	check := func(where, text string) {
		if strings.Contains(text, "TODO") {
			r.add("W008", SevWarning, file, where,
				"unfilled TODO placeholder")
		}
	}
	check("module why:", p.ModuleWhy)
	for _, d := range p.Docs {
		check(fmt.Sprintf("doc %q", d.Title), d.Content)
	}
	for _, inv := range p.Invariants {
		check(fmt.Sprintf("invariant %q", inv.Title), inv.Content)
	}
	for _, name := range sortedKeysStruct(p.Structs) {
		s := p.Structs[name]
		kind := "struct"
		if s.IsClass {
			kind = "class"
		}
		check(fmt.Sprintf("%s %s why:", kind, name), s.Why)
		for _, f := range s.Fields {
			check(fmt.Sprintf("%s %s.%s doc:", kind, name, f.Name), f.Doc)
		}
		for _, mname := range sortedKeysFunc(s.Methods) {
			check(fmt.Sprintf("%s %s.%s why:", kind, name, mname), s.Methods[mname].Why)
		}
	}
	for _, name := range sortedKeysIface(p.Interfaces) {
		ifc := p.Interfaces[name]
		check(fmt.Sprintf("interface %s why:", name), ifc.Why)
		for _, mname := range sortedKeysFunc(ifc.Methods) {
			check(fmt.Sprintf("interface %s.%s why:", name, mname), ifc.Methods[mname].Why)
		}
	}
	for _, name := range sortedKeysFunc(p.Functions) {
		check(fmt.Sprintf("func %s why:", name), p.Functions[name].Why)
	}
	for _, name := range sortedKeysTypeDef(p.TypeDefs) {
		check(fmt.Sprintf("typedef %s why:", name), p.TypeDefs[name].Why)
	}
}

// --- helpers --------------------------------------------------------------

func sortFindings(r *Result) {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Where < b.Where
	})
}

func sortedKeysStruct(m map[string]*extract.StructInfo) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysIface(m map[string]*extract.InterfaceInfo) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysFunc(m map[string]*extract.FuncInfo) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysTypeDef(m map[string]*extract.TypeDefInfo) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
