// Package gen implements `lyre gen --rich` post-processing: it takes a
// freshly-extracted *extract.PackageInfo and seeds TODO placeholders into
// every rich-doc slot that lint would otherwise flag (W001/W002/W004 and,
// where the legacy per-decl Doc field has content, W005 too).
//
// The contract: after SeedRichPlaceholders runs on a clean (un-seeded)
// PackageInfo, a subsequent `lyre lint` run should report only W008
// (unfilled TODO placeholders) plus — for modules with ≥3 methods on any
// class/struct/interface — W003 (which seeding intentionally does NOT
// satisfy, because invariants are caught-bug records, not auto-generated
// boilerplate).
//
// Seeding is idempotent: a field that already has prose is never
// overwritten. Where the legacy per-decl Doc field (populated by extractors
// from native source comments) is non-empty, it is used as the seed for
// Why; otherwise a generic "TODO: explain X." placeholder is used.
package gen

import (
	"strings"

	"github.com/waywardgeek/lyre/pkg/extract"
)

// SeedRichPlaceholders mutates p in place, filling every empty rich-doc slot with a standardized placeholder or a migrated native-source comment, without ever overwriting existing prose.
//
// It fills each empty slot with either a TODO placeholder or a cleaned-up
// version of the legacy native-source-comment Doc field. Idempotent:
// existing prose is never overwritten.
func SeedRichPlaceholders(p *extract.PackageInfo) {
	if p == nil {
		return
	}

	// Module-level why.
	if strings.TrimSpace(p.ModuleWhy) == "" {
		p.ModuleWhy = "TODO: one-line purpose of the " + p.Name + " module."
	}

	// doc "Architecture" — append iff no case-insensitive match exists.
	if !hasDocTitle(p.Docs, "Architecture") {
		p.Docs = append(p.Docs, extract.DocBlock{
			Title:   "Architecture",
			Content: "TODO: explain the module's high-level structure.",
		})
	}

	// Per-decl Why for classes/structs and their methods.
	for name, s := range p.Structs {
		if strings.TrimSpace(s.Why) == "" {
			s.Why = seedWhy(name, s.Doc)
		}
		for mname, m := range s.Methods {
			if strings.TrimSpace(m.Why) == "" {
				m.Why = seedWhy(name+"."+mname, m.Doc)
			}
		}
		// Per-field doc: only seed from existing native-source FieldInfo.Doc
		// when non-empty. Do NOT manufacture TODOs at field scope — W005 only
		// fires on enum-bearing heavy structs and the nudge is meaningful only
		// when the human chooses to fill it.
		for i := range s.Fields {
			if strings.TrimSpace(s.Fields[i].Doc) == "" {
				// Field has no per-field doc. Field's legacy native-comment Doc
				// is unused today (extractors don't populate field-level doc
				// comments separately) — leave empty.
				continue
			}
			s.Fields[i].Doc = extract.CleanDocLine(s.Fields[i].Doc)
		}
	}

	// Per-decl Why for interfaces and their methods.
	for name, ifc := range p.Interfaces {
		if strings.TrimSpace(ifc.Why) == "" {
			ifc.Why = seedWhy(name, ifc.Doc)
		}
		for mname, m := range ifc.Methods {
			if strings.TrimSpace(m.Why) == "" {
				m.Why = seedWhy(name+"."+mname, m.Doc)
			}
		}
	}

	// Per-decl Why for top-level functions.
	for name, fn := range p.Functions {
		if strings.TrimSpace(fn.Why) == "" {
			fn.Why = seedWhy(name, fn.Doc)
		}
	}

	// Per-decl Why for typedefs.
	for name, td := range p.TypeDefs {
		if strings.TrimSpace(td.Why) == "" {
			td.Why = seedWhy(name, td.Doc)
		}
	}
}

// hasDocTitle reports whether docs contains a block with the given title
// (case-insensitive comparison, matching lint W002's logic).
func hasDocTitle(docs []extract.DocBlock, title string) bool {
	for _, d := range docs {
		if strings.EqualFold(d.Title, title) {
			return true
		}
	}
	return false
}

// seedWhy returns a why: value for a declaration. If the legacy native-source
// Doc field has content, it's cleaned (first non-empty line, comment markers
// stripped) and used directly. Otherwise a generic TODO placeholder
// referencing the declaration name is returned. The result is always a
// single line suitable for the one-line `why:` slot (spec §4 row-2).
func seedWhy(declName, doc string) string {
	if line := extract.CleanDocLine(doc); line != "" {
		return line
	}
	return "TODO: explain " + declName + "."
}

// cleanDocLine moved to pkg/extract as extract.CleanDocLine so that the
// language extractors (which cannot import pkg/gen without a cycle) can share
// the same one-line reduction logic.
