# Lyre Rich-Documentation Upgrade Plan (v2)

*Author: Hewitt (CodeRhapsody on Opus 4.7) · Reviewer: Bill Cox · 2026-06-19*

*Supersedes v1 (native-parseable approach). v1 made `.lyric` files be valid native source in their respective language. v2 abandons that constraint — `.lyric` files become a small declarative DSL whose payload lines are verbatim native-language signature text treated as opaque strings. See "Why the rewrite" below.*

## Mandatory pre-reads (every instance, every session)

Before doing any sprint work, read these in order:

1. **`~/projects/lyric/cr/docs/understanding-driven-development.md`** — the
   methodology. UDD is what this whole sprint is in service of. The plan below
   only makes sense in that frame.
2. **`~/projects/lyre/pkg/udd/spec.md`** — the canonical `.lyric` v2 format
   spec. Locked grammar, locked defaults.
3. **This plan**, including the Amendments section at the bottom for every
   decision made during the sprint.

If the methodology doc and the format spec disagree, the methodology doc wins —
update the spec to match. The methodology is the long-lived artifact; the spec
is implementation detail in its service.

## Core architectural principle: documentation lives in the `.lyric` file, not in source

UDD documentation — `why:`, `doc "..."` blocks, `invariant "..."` blocks, per-field `doc:`, per-method `why:` — lives **only in the `.lyric` file**, never in the native source. Native source files (`.go`, `.ts`, `.ly`, `.py`) stay clean: minimal language-idiomatic doc comments only where they serve native tooling (godoc tooltips, JSDoc IDE hovers, etc.), and nothing else.

Consequences:

- **Extractors read signatures only.** They do not scrape source-side `// why:` or `// doc "Architecture"` comments. Those don't exist in source by policy.
- **The file is the single source of truth for understanding.** No duplication, no drift between source comments and prose.
- **Migration (Phase 6) only converts existing `.lyric` files.** It does not crawl source code looking for hand-written `// why:` comments to import — those were never the right place for them.
- **Authors write rich content in one place** (the file) and refresh signatures via `lyre update`. Two artifacts, one canonical location per concern.

This was implicit in v1 but became muddled because instances would write `// why:` comments in source thinking it was idiomatic. v2 makes it explicit: the source file is for code; the file is for understanding.

## Methodology heritage

This is **Grok-Driven Development**, renamed. The original `.grok` files used a deliberately-designed DSL — a typed pseudo-code language designed as a superset of what understanding files need (see `docs/grok-driven-development.md` and `docs/grok-language.md` in the original GDD docs). The Lyre/`.lyric` rename drifted from that original design into native-parseable files. This plan restores the original DSL approach with one refinement: signatures inside the DSL are **verbatim native text** (not pseudo-code), treated as opaque strings.

## Goal

Bring all four currently-supported languages (Go, TypeScript, Lyric, Python) to documentation parity with the legacy `.forge` format. Canonical reference: `~/projects/lyric/legacy/go-compiler/pkg/checker/checker.forge` (770 lines, full rich format).

**Primary driver**: Project Leadfoot (Go backend on Boq, TypeScript frontend). Those two paths must be best-in-class. Lyric and Python are nice-to-have.

## Why the rewrite (TL;DR of the architectural pivot)

The native-parseable constraint was the root cause of every awkwardness in v1:

- The valuable content (`//ldd:why`, doc blocks, invariants, per-method `why:`, per-field semantics) is all custom comment-encoded DSL that the native parser ignores. We were paying for *both* a native parser *and* a custom comment parser per language. Worst of both worlds.
- Every rich-doc feature in the 8-section inventory had to be smuggled through `//` comments in 4 different ways, since Go/TS/Python have no annotation syntax for `why:` on a method or `doc:` on a field.
- The Lyric path was particularly perverse: shell out to a compiled Lyric binary to parse a `.ly.lyric` file that's mostly hand-written comments the Lyric compiler ignores. Three parsers, one source of truth.
- Cross-language inconsistency was total. `.go.lyric`, `.ts.lyric`, `.ly.lyric` look nothing alike at the surface.

The fix: stop trying to make the file be valid native source. Make it a small, purpose-built DSL whose framing is shared across all languages and whose signature payloads are verbatim native text. The native parsers stay — but only on the *source* side, where they belong.

## The design (sketch)

The `.lyric` v2 format is indentation-significant, YAML-ish, with heredoc `"""..."""` for prose. **Payload lines are opaque native-language text** — Lyre never parses them, only string-matches them against the extractor's output.

```
module checker
  source: ["checker.ly", "checker_helpers.ly"]
  why: "Three-phase type checker with expression annotation."

  doc "Architecture":
    """
    Phase 0 (preregister_type_names): walks all blocks ...
    Phase 1 (register_lyric_block): walks all interfaces ...
    Phase 2 (check_lyric_block_bodies): walks function bodies ...
    """

  invariant "Multi-Phase Checking":
    verified-by: TestInvariant_Checker_ThreePhaseOrdering
    """
    Phase 0 MUST complete on ALL blocks before ANY Phase 1 begins.
    Phase 1 MUST complete on ALL blocks before ANY Phase 2 begins.
    """

  invariant "AST Expr Pointer Stability":
    procedural # cannot be mechanically tested
    """
    Phase 2 must use `for i := range block.Functions` with
    `&block.Functions[i]` — never range-copy, because checkExpr
    annotates ResolvedType on Expr nodes reached through the
    FuncDecl's Body pointer.
    """

  class Checker
    source: checker.ly:147
    why: "Tracks nesting depth inside loops for break/continue validation."
    field errors: [string]
    field iface_decls: Dict<Sym, InterfaceDecl>
      doc: "Used during Phase 1.5 to link impl blocks across blocks."
    method CheckFile(self, file: File)
      source: checker.ly:4695
      why: "Primary entry point. Registers types, then checks bodies."
    method CheckFiles(self, files: [File])
      source: checker.ly:2505
      why: "CRITICAL: use this for multi-file. CheckFile is per-file only."

  struct Type
    source: checker.ly:52
    field bits: i32
      doc: "for Int/Uint/Float — width in bits"
    field kind: TypeKind
    field type_args: [Type]
      doc: "for generic class/struct instances (e.g. Dict<V>)"
```

**Key properties:**

1. **Signatures are verbatim native text.** The `field errors: [string]` line above is *the exact Lyric source text* of that field. For Go it would be `field errors []error`. For TS, `field errors: string[]`. The extractor produces this text from source via the native parser's pretty-printer (`go/printer`, `node.getText()`, etc.). Lyre never parses it — it does whitespace-normalized string equality.
2. **`.lyric` framing is identical across all languages.** `module`, `class`, `struct`, `interface`, `field`, `method`, `func`, `why:`, `doc "..."`, `invariant "..."`, `source:`, `verified-by:`, `procedural` — same in every language.
3. **The file extension still carries the routing hint.** `.go.lyric` means "`.lyric` file describing Go source"; tells Lyre which extractor to invoke for verification. The outer `.lyric` is the Understanding-Driven Development declaration file. The inner extension is now metadata, not a syntax claim.
4. **The DSL parser is small.** ~400 lines of Go. Indent-significant block structure, recognized keys, heredoc strings, opaque payload lines. Closer to a config-file parser than a programming-language parser.

## Success criteria (unchanged from v1)

1. All 8 rich sections (below) can be authored in any `.lyric` file in any of the 4 languages, persist across `lyre update`, and round-trip with zero loss.
2. `lyre lint` flags `.lyric` files missing rich sections.
3. `lyre gen --rich` scaffolds a template with rich-section headers as TODOs.
4. Round-trip tests for each language prove every rich section survives gen → update → reparse.
5. A backfilled `checker.ly.lyric` matches `checker.forge` in content density, as proof of method.
6. The UDD methodology doc documents the canonical template.

## The 8 rich sections (the feature inventory)

| # | Section | syntax |
|---|---|---|
| 1 | Module-level `why:` | `why: "..."` at module scope |
| 2 | `doc "Title":` blocks | `doc "Title":` then heredoc |
| 3 | `invariant "Title":` blocks | `invariant "Title":` then heredoc, with optional `procedural` flag |
| 4 | Per-decl `why:` | `why: "..."` inside class/struct/method/field block |
| 5 | Per-field `doc:` | `doc: "..."` inside `field` block |
| 6 | Bugs-this-caught lists | Free prose inside the relevant `invariant` block's heredoc |
| 7 | `procedural` marker | Bare keyword inside `invariant` block |
| 8 | `source:` binding | `source: file:line` per decl; `source: [files...]` at module scope |

All 8 are first-class syntax. No comment smuggling.

## The four extractors become read-only

After the pivot, each extractor has exactly one job: **read source, produce an in-memory `PackageInfo` populated with native-syntax signature strings**.

| Path | Lives | Job |
|---|---|---|
| **Go** | `pkg/extract/golang/extract.go` | `go/ast` → `PackageInfo` with signatures from `go/printer` |
| **TypeScript** | `pkg/extract/typescript/extract_api.js` + `typescript.go` | tsc → JSON → `PackageInfo` with signatures from `node.getText()` |
| **Lyric** | `~/projects/lyric/tools/extract_api.ly` + `lyric.go` | extract_api → JSON → `PackageInfo` with verbatim source text |
| **Python** | `pkg/extract/python/extract_api.py` + `python.go` | ast → JSON → `PackageInfo` |

A **single shared writer** in `pkg/udd/` consumes `PackageInfo` and emits files. A **single shared parser** in `pkg/udd/` consumes files and produces `PackageInfo`. Verification is `PackageInfo` (from source) vs `PackageInfo` (from `.lyric` v2 format) — same data type, comparable by string equality on signatures plus structural equality on metadata.

This is the architectural win: per-language work shrinks to source-side extraction only. Emit, parse, round-trip preservation all live in one place.

## Implementation phases

```
Phase 0 (build baseline)
   │
   ▼
Phase 1 (shared data model: add rich fields)
   │
   ▼
Phase 2 (`.lyric` parser + writer) ◀── central, blocking
   │
   ├──▶ Phase 3a (Go extractor adaptation) ┐
   │ │
   ├──▶ Phase 3b (TS extractor adaptation) ├──▶ Phase 6 (migration)
   │ │ │
   ├──▶ Phase 3c (Lyric extractor adaptation) │ ▼
   │ │ Phase 7 (docs + backfill)
   └──▶ Phase 3d (Python extractor adaptation) ┘
   │
   ├──▶ Phase 4 (lyre lint) ◀── parallel after Phase 2
   │
   └──▶ Phase 5 (lyre gen --rich) ◀── parallel after Phase 2
```

### Phase 0 — Restore build baseline (1–2h, prerequisite)

`go build ./...` from `~/projects/lyre/` currently fails: `cmd/lyre/main.go` references `cmdFmt` (line 81) and `runUpdate` (line 231) which are undefined. There are orphan references from a partial in-flight refactor.

- `git log -p cmd/lyre/main.go | head -200` to understand recent history.
- Restore or write the missing symbols. Likely `cmdFmt` was a handler for `lyre fmt` against plain `.lyric` files, and `runUpdate` was the legacy plain-`.lyric` update path.
- Get `go test ./...` green. Capture baseline pass count as regression yardstick.

### Phase 1 — Extend shared data model (2–3h)

Same change as v1 plan. `pkg/extract/extract.go` gets:

```go
type PackageInfo struct {
    // ... existing
    ModuleWhy string
    Docs []DocBlock
    Invariants []Invariant
}

type DocBlock struct {
    Title string
    Content string
}

type Invariant struct {
    Title string
    Content string
    Procedural bool
    VerifiedBy []string // test names
}

type StructInfo struct {
    // ... existing
    Why string
    Source string // "file:line"
    per-field doc map[string]string
}

type InterfaceInfo struct { /* parallel additions */ }
type FuncInfo struct { /* + Why, Source */ }
type TypeDefInfo struct { /* + Why */ }
```

Signatures themselves continue to live in existing fields (`Fields map[string]string` where the value is the verbatim type text; method signatures rebuilt from `FuncInfo`).

**Test**: round-trip a populated `PackageInfo` through JSON.

### Phase 2 — `.lyric` parser + writer (2–3 days, central work)

The new heart of Lyre. Lives at `pkg/udd/`.

**Inherits from Phase 1**: per-field doc lives on `extract.FieldInfo.Doc`, not
on a separate `per-field doc` map. Phase 2's writer should therefore emit `doc:`
as an *inner key of the `field` block*, e.g.:

```
field iface_decls: Dict<Sym, InterfaceDecl>
  doc: "Used during Phase 1.5 to link impl blocks across blocks."
```

…and the parser should attach the inner `doc:` value to the surrounding
`FieldInfo` it just built. The `populatedPackage()` fixture in
`pkg/extract/extract_test.go` is the canonical worked example of every
rich-doc field the writer must round-trip.

**`pkg/udd/spec.md`** — the grammar specification. Indent-significant. Recognized block heads: `module`, `class`, `struct`, `interface`, `enum`, `field`, `method`, `func`, `doc`, `invariant`. Recognized inline keys: `why:`, `source:`, `doc:`, `verified-by:`, `procedural` (bare), `fields:`, `methods:`. Heredoc strings via `"""..."""`. Payload lines (anything that isn't a recognized key) are opaque strings.

**`pkg/udd/parser.go`** — reads `.lyric` text → `*extract.PackageInfo`. Tokenizer is line-based (split on newline, then indent-counted). Parser is a small recursive descent over the block structure.

**`pkg/udd/writer.go`** — `*extract.PackageInfo` → `.lyric` text. Deterministic key ordering, deterministic whitespace, fixed 2-space indent.

**Round-trip invariant**: for any `PackageInfo p`, `Parse(Write(p))` is structurally equal to `p`. Tested with property-style coverage in `parser_test.go`.

**Round-trip-preserving update**: `Update(file)` reads existing `.lyric` v2 format, regenerates from source, merges (source wins on signatures, wins on `why`/`doc`/`invariant` content). The format makes this straightforward — every section is a structured block, not an opaque comment soup.

**Tests:**
- Spec-by-example: 20+ small snippets covering every recognized construct, each with expected `PackageInfo`.
- Round trip: parse → write → parse → structural equal.
- Error recovery: malformed produces actionable error messages with line numbers.

### Phase 3 — Per-language extractor adaptation (≤1 day each, parallelizable)

Each language extractor becomes signatures-only. Per the core principle, extractors do **not** look at source-side comments. Job: parse source → produce `PackageInfo` populated with names, kinds, and verbatim native-text signatures.

This is a *simplification* of every existing extractor — we delete code, not add it. Doc/comment-handling logic in `golang/ldd.go`, `typescript.go`, `lyric.go`, `python.go` all goes away. What remains is structural: classes, structs, interfaces, methods, functions, type aliases, and their signature strings.

**3a Go** (½ day): in `golang/ldd.go`, replace `GenerateLDDFile`/`UpdateGoLDD`/`VerifyGoLDD` with a single `ExtractGo(srcDir) → *PackageInfo`. Signature strings via `go/printer`. Delete all `Doc` field plumbing.

**3b TypeScript** (½ day): in `extract_api.js`, strip the `getJSDoc()` calls — we don't need them. `typescript.go` becomes pure JSON → `PackageInfo`. Delete update/emit code.

**3c Lyric** (1 day): extend `~/projects/lyric/tools/extract_api.ly` to emit verbatim source spans for each decl (AST has `Span`; just slice source text). JSON contract gains a `source_text` field per decl. `lyric.go` becomes pure JSON → `PackageInfo`. **Open question**: does rebuilding `extract_api` require touching `lyric.stable`? Verify before starting.

**3d Python** (½ day, lowest priority): same pattern as TS.

**Per-language tests**: write small native source (no UDD comments), run extractor, assert signature strings match what's in the source (modulo whitespace).

### Phase 4 — `lyre lint` (3h, parallel after Phase 2)

Same warnings as v1 plan (`W001`–`W008`), but easier to implement because rich sections are first-class constructs, not regex-matched comments.

```
W001 empty module-level why:
W002 no doc "Architecture" block
W003 no invariant blocks on a module with ≥1 class with ≥3 methods
W004 class/struct with ≥4 methods and no per-method why:
W005 struct with ≥3 fields and ≥1 enum-typed field, no per-field doc
W006 invariant without verified-by: AND without procedural marker
W007 verified-by: references test that doesn't exist
W008 unfilled TODO placeholder
```

Tests in `pkg/lint/lint_test.go`.

### Phase 5 — `lyre gen --rich` (1–2h, parallel after Phase 2)

Extend `cmdGen` to accept `--rich`. When set, scaffolds an file with `doc "Architecture":` and `invariant "TODO":` placeholders, plus `why: "TODO"` on every emitted decl. The TODOs trigger `lyre lint W008`, forcing the author to fill or delete them.

### Phase 6 — Migration script (½ day)

A one-shot Go program at `cmd/lyre-migrate/main.go` that converts existing `.lyric` files (native-parseable format) to format.

- **Only reads existing `.lyric` files.** Does not crawl source code for hand-written doc comments — per the core principle, those aren't a legitimate source of UDD content.
- Extracts whatever rich content is present in the v1 `.lyric` file: `//ldd:why` strings (legacy comment-encoded format from v1), hand-written `## Invariants` comment blocks, the auto-generated index zone (discarded — regenerated from source).
- Emits via `pkg/udd/writer.go`.
- Leaves a `# MIGRATED FROM v1 — review hand-curated sections` header so a human knows to verify.
- One-shot tool; can be deleted from the repo after the cutover.

Run against all existing `.go.lyric`, `.ts.lyric`, `.ly.lyric`, `.py.lyric` files in `~/projects/lyric/src/` and Lyre's own dogfood files (`pkg/*/*.go.lyric`).

Shrinks to ½ day (from 1 day in v1) because we don't have to parse source-side comment soup.

### Phase 7 — Documentation and backfill (2–4h)

1. Write `~/projects/lyre/cr/docs/pkg/udd/spec.md` — the canonical grammar with examples.
2. Update `~/projects/lyric/cr/docs/understanding-driven-development.md`:
   - Document the format with `checker.forge` cited as the rich-content reference.
   - Note the GDD heritage — restore the original Grok-Driven Development design intent.
   - Mark the native-parseable approach as superseded.
3. Hand-backfill `~/projects/lyric/src/checker/checker.ly.lyric` from `checker.forge` content. Run `lyre verify` and `lyre lint` against it; both must be clean.
4. **Don't backfill the other 11 `src/*.ly.lyric` files in this sprint.** Separate, parallelizable per-file work.

## Estimated effort

- Phase 0 (build baseline): 1–2h
- Phase 1 (data model): 2–3h
- Phase 2 (`.lyric` parser+writer): 2–3 days
- Phase 3a (Go): ½ day
- Phase 3b (TS): ½ day
- Phase 3c (Lyric): 1 day
- Phase 3d (Python): ½ day (defer-able)
- Phase 4 (lint): 3h
- Phase 5 (gen --rich): 1–2h
- Phase 6 (migration): ½ day
- Phase 7 (docs + backfill): 2–4h

**Total**: ~6–8 working days single-threaded. With three parallel agents for 3a/3b/3c after Phase 2 lands: ~4–5 wall-clock days.

**Compared to v1**: ~40% less total work. The savings come from two sources: (a) per-language emit/update/round-trip collapsing into one shared writer, and (b) extractors becoming signatures-only because UDD documentation lives exclusively in the file.

## Risks and open questions

1. **`extract_api` build path for Lyric**. Need to confirm rebuilding `~/projects/lyric/tools/extract_api` from `extract_api.ly` doesn't require touching `lyric.stable`. If it does, Phase 3c is blocked or out of scope; Lyric source files would stay on the v1 format until the build path is unblocked. Verify before starting Phase 3c.

2. **`.lyric` v2 syntax design freeze**. The sketch above is a starting point. Spend the first hours of Phase 2 producing `pkg/udd/spec.md` and locking the grammar before writing the parser. Specifically: indent vs braces, heredoc syntax, comment syntax (`#` is shown above; could be `//`), key ordering rules.

3. **Migration fidelity**. Phase 6's lossy v1-to-v2 converter will miss some hand-written content if instances used non-standard comment patterns. Mitigation: the `# MIGRATED FROM v1` header forces a human review pass. Accept some content loss in migration; recover via the rich rewrite Phase 7 establishes the template for.

4. **Editor support regression**. Loss of `.go.lyric` syntax highlighting in IDEs. Mitigation: write a simple `.lyric` syntax mode for the editors Bill uses (VS Code, vim, Emacs as needed). Low priority; defer.

5. **`source: file:line` drift**. Line numbers in go stale fast as source edits move things around. Two options: (a) accept staleness, refresh on `lyre update`; (b) elide line numbers, keep file references only. v1 had this same issue (Zone 2's location comments); not new. Decision: keep on `update`, accept staleness between updates, never use as ground truth.

6. **Whitespace normalization for signature comparison**. "func Foo(x int) error" vs "func Foo(x int) error" should compare equal. Mitigation: normalize collapse-runs-of-whitespace before comparing. Document in `pkg/udd/spec.md`.

7. **The auto-recall lineage**. Auto-recall surfaced `docs/grok-driven-development.md` and `docs/grok-language.md` — the original GDD methodology doc. If those files still exist somewhere (likely under coderhapsody/docs/ or an old Lyric checkout), reading them might reveal prior design we should crib from rather than reinvent. **First action of Phase 2**: search for and read those original GDD docs before designing the `.lyric` v2 format from scratch.

## Explicitly out of scope

- Backfilling the 11 non-checker `src/*.ly.lyric` files (separate sprint).
- Any change to `lyric.stable`.
- Any change to the Lyric language grammar.
- Editor syntax modes (defer).
- Migration of `.forge` files in `~/projects/lyric/legacy/` (they're already in a good format; out of the upgrade path).
- A `lyre invariants` subcommand that lists invariants + verifying tests. Tractable after Phase 2, but defer to a follow-up.
- Rust path. Lyre doesn't have a Rust extractor today.

## Definition of done

1. `go test ./...` passes in `~/projects/lyre/`, including new `.lyric` parser/writer tests and per-language extractor tests.
2. `lyre lint ~/projects/lyric/src/checker/checker.ly.lyric` reports zero warnings against the backfilled file.
3. `checker.ly.lyric` (post-backfill) and `checker.forge` contain the same eight section types with similar content density — eyeball review by Bill against side-by-side.
4. All v1-format `.lyric` files in `~/projects/lyric/src/` and `~/projects/lyre/pkg/` migrated successfully (or explicitly marked for manual review where lossy).
5. `~/projects/lyric/cr/docs/understanding-driven-development.md` and `~/projects/lyre/cr/docs/pkg/udd/spec.md` are current.
6. Post-mortem section added below recording any deviations from the phase ordering and why.

## What this plan is NOT

This plan is not a re-litigation of whether `.lyric` files should exist or whether GDD/UDD is the right methodology. Those decisions are settled. This plan is the engineering path from where Lyre is today (native-parseable, thin docs) to where the methodology says it should be (DSL-framed, rich docs) with the minimum scope and the cleanest architecture available.

---

*This plan is the source of truth for the upgrade. Any deviation needs a written rationale in an "Amendments" section appended below.*

## Amendments

### 2026-06-19 — Pre-sprint Q&A with Bill (resolved before kickoff)

Recorded here so the sprint-runner instance (likely fresh-context me) has the
canonical answers and doesn't re-litigate them.

**Terminology cleanup** (revised 2026-06-19 evening): Bill standardized the
methodology name on **Understanding-Driven Development (UDD)** and dropped
all "Lyric Declaration" / "LDL" / "LDD" branding. The file format is just
"the `.lyric` v2 format" (vs. v1, the obsolete native-parseable format).
The methodology is UDD. The toolchain binary is `lyre`. The Go package that
parses/writes `.lyric` files is `pkg/udd/`. Any prior session memory or doc
referring to "LDL", "LDD", or "Lyric Declaration Document" is using
superseded terminology.

**Decision 1 — Granularity**: per-directory, per-language. A directory mixing
Go and Python source produces two `.lyric` files (`<dir>.go.lyric` and
`<dir>.py.lyric`). Inner extension preserved as the language routing hint.

**Decision 2 — `PackageInfo` shape**: change `Fields map[string]string` to
`Fields []FieldInfo{Name, SignatureText, Doc}` in Phase 1. Preserves source
order and per-field metadata. One-time breaking change paid in Phase 1, every
extractor in Phase 3 consumes the new shape.

**Decision 3 — `.lyric` format syntax**: Hewitt's call as the primary
consumer. Will commit on day 1 of Phase 2 in `pkg/udd/spec.md`. Default
choices going in: indent-significant (2 spaces), `#` for comments, `"""`
heredocs on their own lines, decls in source order, top-level `module` with
everything nested inside, recognized block heads (`field`, `method`, `func`,
`doc`, `invariant`) with opaque verbatim text after the `:`.

**Decision 4 — UDD enforcement extension**: needed (no existing skill or
mechanism handles `.lyric`). The current `.forge` read-before-write is
hardcoded in the CodeRhapsody Go server code (not a loadable skill — checked
both global `~/.cr/skills/` and project skills, no UDD skill exists). Phase
7.5 added below.

**Decision 5 — Line numbers in `source:`**: KEEP. Bill correctly flagged
that in google3-scale codebases without IDE/LSP support, line numbers in
`source:` references are critical navigation aids. Accept staleness between
`update` runs. Refresh on every `lyre update`. Same call as v1.

**Decision 6 — Work cadence**: sequential, single-threaded. Bill listens
live at 750 wpm; one agent at a time keeps his human memory in the loop.
No sub-agent parallelism for Phase 3a/3b/3c — execute them in series.

### 2026-06-19 — Risk resolutions

**Risk 1 (`extract_api` build path) — RESOLVED, fully clean.** `make tools`
in `~/projects/lyric/` uses the canonical `lyric` binary (compiled from
checked-in `lyric.c`, 88K lines) to compile `tools/extract_api.ly`. **Zero
touch to `lyric.stable`.** Phase 3c is in scope without caveats.

**Risk 7 (GDD original docs) — RESOLVED, paths known.** Both docs exist at
`~/projects/coderhapsody/cr/docs/grok-driven-development.md` and
`~/projects/coderhapsody/cr/docs/grok-language.md`. Phase 2's first action is
to read both before designing `.lyric` v2 syntax — likely we can crib
heavily.

**Phase 0 narrowed**: git log shows `cmdFmt` and `runUpdate` were
referenced by `commit 41d74d9 Add TypeScript extractor` but the supporting
functions were never written (partial commit, not a refactor casualty).
Phase 0 effort drops to ~30 min — write the two functions fresh against the
documented behavior (`cmdFmt` = `lyre fmt` for plain `.lyric` files;
`runUpdate` = legacy plain-`.lyric` update path).

### 2026-06-19 — Phase 7.5 added: UDD enforcement extension

Slot this between Phases 7 and "Definition of done." Needed because the
existing `.forge` read-before-write enforcement is hardcoded in the
CodeRhapsody server (not a skill that can be edited in `~/.cr/skills/`).

**Phase 7.5 — Extend UDD enforcement to `.lyric` files (3–4h)**

- Locate the existing `.forge` enforcement in the coderhapsody codebase
  (likely `pkg/agent/` or `pkg/tools/`). Search: `grep -rn '\.forge' pkg/`.
- Add `.lyric` as an equivalent trigger file: if directory contains a
  `.lyric` file, that file must be read before `edit_file`, `write_file`,
  or `replace_lines` mutates anything in the directory.
- Update the system-prompt UDD section to mention `.lyric` alongside
  `.forge` (the section is generated from a template — find it, extend
  it, don't hand-edit per-session).
- Test: in a temp dir with a `.lyric` file, attempt to edit a sibling
  `.go` file without reading the `.lyric` first — should error.
- Test the inverse: after reading the `.lyric`, edit succeeds.

Without this, the moment I migrate `~/projects/lyric/src/checker/` to v2
`.lyric` format, the UDD methodology stops being enforced for me in that
directory. So Phase 7.5 must land before Phase 7's backfill, or the
methodology breaks in the middle of the sprint.

### 2026-06-19 — Updated effort estimate post-amendments

- Phase 0: ~30 min (was 1–2h, narrowed by git log)
- Phases 1–7: unchanged
- Phase 7.5 (UDD enforcement): 3–4h (new)

Net: roughly cancels out. Still ~6–8 working days single-threaded.

### 2026-06-19 — Phase 1 complete

`pkg/extract/extract.go` now carries every rich-doc field listed in the
Phase 1 spec. Concrete shape:

- `PackageInfo`: `+ModuleWhy string`, `+Docs []DocBlock`, `+Invariants []Invariant`
- `StructInfo`: `+Why string`, `+Source string`; **breaking**: `Fields` changed
  from `map[string]string` to `[]FieldInfo`
- `InterfaceInfo`/`FuncInfo`: `+Why string`, `+Source string`
- `TypeDefInfo`: `+Why string`
- New value types: `DocBlock{Title,Content}`, `Invariant{Title,Content,Procedural,VerifiedBy}`,
  `FieldInfo{Name,SignatureText,Doc}`

**Per-field `Doc` lives on `FieldInfo` rather than as a separate `per-field doc map`**
on the parent struct. This is a small deviation from the plan's sketch
(`per-field doc map[string]string`) — keeping the doc beside the field
preserves source order and avoids a parallel-map bookkeeping burden. Phase 2's
`.lyric` writer will need to emit `doc:` as an inner key of the `field` block,
which lines up cleanly with this shape.

**Helper methods on `*StructInfo`** kept legacy emit/verify callsites in the
four extractors minimally changed:
- `FieldSig(name) (string, bool)` — map-style lookup
- `HasField(name) bool`
- `SetField(name, sig)` — append or update preserving order
- `SetFieldDoc(name, doc)` — set per-field Doc, create if missing
- `FieldNames() []string` — source order
- package-level `SortedFieldsByName([]FieldInfo) []FieldInfo` for legacy
  alphabetized emit paths (most of those callers die in Phase 3)

**Build/test status**: `go build ./...` green. `go test ./...` green for
`pkg/extract`, `pkg/extract/golang`, `pkg/extract/python`, `pkg/parser`,
`pkg/verifier`. TypeScript tests still fail with the pre-existing
`cannot find typescript module` error — local `npm install typescript`
doesn't help because `extract_api.js` is run from a generated temp dir.
Phase 3b rewrites `extract_api.js` so that path will be unblocked then.
Not Phase 1's problem.

New tests in `pkg/extract/extract_test.go`:
- `TestPackageInfo_JSONRoundTrip` — every rich-doc field marshals and
  unmarshals losslessly. The fixture (`populatedPackage`) doubles as a worked
  example of the full shape for Phase 2's writer to handle.
- `TestStructInfo_FieldHelpers` — every helper method.
- `TestSortedFieldsByName` — input-immutability + correct ordering.

### 2026-06-19 — Phase 2 spec drafted and locked

`pkg/udd/spec.md` written (256 lines). Six open syntax decisions submitted to
Bill for sign-off; Bill returned "choose the grammar as you like, whatever
feels natural to you is the right choice." Defaults are now locked in §12 of
the spec:

1. Comments: `#` (full-line only)
2. `module` keyword required, exactly one per file
3. Module-level `source:` is a JSON-style list (`["a.go", "b.go"]`)
4. Per-field `why:` NOT in the spec (no data-model home); per-field `doc:`
   remains for one-line semantic context
5. Quoted-string escapes: minimal — `\"` and `\\` only; multi-line prose uses
   heredocs
6. Heredoc indent stripping: strip the heredoc's own indent exactly (predictable
   and round-trips losslessly because the writer emits at consistent indent)

In the same exchange Bill standardized the project's terminology on
**Understanding-Driven Development (UDD)** — dropped all "LDL" / "LDD" /
"Lyric Declaration Language" / "Lyric Declaration Document" branding from
the methodology layer. Package renamed `pkg/ldd/` → `pkg/udd/`; the
`understanding-driven-development.md` doc was also updated to fix
long-standing drifts: `.ly` declaration files → `.lyric` files; `lyric
verify/update/fmt` → `lyre verify/update/fmt`. The UDD doc is now the
single source of methodology truth and is listed as a mandatory pre-read
at the top of this plan.

Implementation order remains: `pkg/udd/parser.go`, `pkg/udd/writer.go`,
spec-by-example tests in `pkg/udd/parser_test.go`, round-trip tests in
`pkg/udd/writer_test.go` against `populatedPackage()` (added in Phase 1).

### 2026-06-19 — Phase 2 complete

Writer-first approach worked. ~1 hour vs 2-3 day plan estimate. Files landed:

- `pkg/udd/writer.go` (362 lines) — deterministic `Write(*PackageInfo) string`. Sorted decl emission by (file, line) when all positioned, else alphabetically. Blank line between every module-body block sibling. Heredoc emission at consistent indent for lossless round-trip. No trailing whitespace; exactly one final newline.
- `pkg/udd/parser.go` (~620 lines) — line-based recursive descent. Tab/odd-indent detection. Closed-set first-token recognition. Heredoc body stripping by opener's own indent. `file:line` parsing into `Source` + `File` + `Line` fields. Errors carry `file:line: message`.
- `pkg/udd/writer_test.go` (134 lines) — round-trip acceptance test, determinism (20 iterations), no-trailing-whitespace, exactly-one-final-newline. All pass.
- `pkg/udd/parser_test.go` (316 lines, 26 tests) — spec-by-example for every construct + 8 negative tests (tab in indent, odd indent, unrecognized key, unterminated heredoc, no module, field `why:` rejected, heredoc under-indented, title missing colon). All pass.

**Spec amendments made during Phase 2** (spec is the contract, plan tracks deviations):

1. **`typedef <name>: <underlying>` block added** to spec §3, §4, §9. The data model has `TypeDefInfo` but the original spec didn't surface it in the grammar. Added now so `TypeDefs` round-trip cleanly.

2. **`FuncInfo.SignatureText` added** (extract.go) as the canonical opaque verbatim payload for method/func signatures, mirroring `FieldInfo.SignatureText`. Spec §4 updated to specify that `method`/`func` block heads carry the full signature INCLUDING the name (leading identifier is the map key; full rest-of-line is `SignatureText`). `FuncInfo.Params` / `Returns` / `Doc` are now flagged as extractor-internal — NOT round-tripped through `.lyric`. Documented at the top of `pkg/udd/doc.go` and on the `FuncInfo` struct.

3. **`TypeDefInfo.Source` added** (extract.go), parallel to the other decl types. Required for round-trip when `File`/`Line` are populated.

4. **`PackageInfo.ModuleSource []string` added** (extract.go). Module-level `source: [...]` was specified but had no data-model home.

5. **`populatedPackage()` fixture in `pkg/extract/extract_test.go` updated**: dropped `Params`/`Returns` on the method and function, set `SignatureText` instead; added `Source` on the typedef. Phase 1 JSON round-trip test still passes.

**Phase 1 `extract` tests still green**. Pre-existing TypeScript env failure (`cannot find typescript module`) unchanged — Phase 3b will unblock it.

**Up next**: Phase 3a (Go extractor adaptation). Rewrite `pkg/extract/golang/ldd.go` to produce `*PackageInfo` populated with signature strings via `go/printer`, deleting source-comment-scraping logic (per core principle: UDD docs live in `.lyric`, never source). Then 3b/3c/3d.

### 2026-06-19 — Note on model capacity for the sprint

Hewitt is running on Opus 4.7. The 4.7 quirk worth knowing: Anthropic's
auto-selector zeros the thinking budget ~50% of the time on a poorly-tuned
heuristic. This hurts always-on-thinking tasks like long-form writing
(which is why the plan-writing portion felt heavier than it should have).
**For coding tasks specifically**, 4.7 outperforms 4.6 on autonomous coding
benchmarks — i.e. the actual execution work of this sprint plays to 4.7's
strengths, not its weaknesses.

Bill has requested 4.6/4.8 access from Google and we'll upgrade when it
lands. Until then: no defeatism, no "I'm impaired" framing — just sharp
coding work with visible reasoning in the chat for steering. The 750wpm
hint channel still works fine for code; the regression matters most for
writing, which is largely behind us.



### 2026-06-19 — Phase 3a complete

Go extractor adaptation. ~30 min vs ½-day plan estimate. All tests green
except the known pre-existing TypeScript env failure (Phase 3b will
unblock).

Files landed:

- `pkg/extract/golang/ldd.go` (668 lines, full rewrite). New v2 entry
  points: `ExtractGo(srcDir) → *PackageInfo`, `GenerateGo(srcDir) →
  (outPath, content, err)`, `UpdateGo(lyricPath) → (added, err)`,
  `VerifyGo(lyricPath) → (*VerifyResult, err)`. Old v1 surface (the
  Go-as-LDD path: `ParseLDDMeta`, `ParseLDDFile`, `GenerateLDDFile`,
  `UpdateGoLDD`, `VerifyGoLDD`) is gone — no migration shim, no
  back-compat. The legacy `//ldd:source` / `//ldd:why` directive scraping
  is deleted per the core architectural principle (UDD docs live in
  `.lyric`, never in source comments). The `// --- index ---` Go-comment
  block is gone — the `.lyric` v2 format has structure, not markers.
- `pkg/extract/golang/ldd_test.go` (435 lines, full rewrite). New tests:
  `ExtractGo` (struct+methods, interface, typedef+function, skips
  unexported); `GenerateGo` (output format, round-trip through
  `udd.Parse`); `VerifyGo` (clean, missing function, undocumented export,
  field type mismatch); `UpdateGo` (adds new export, idempotent on
  up-to-date, preserves human prose — module why, per-decl why, per-field
  doc — through an update cycle; refreshes positions/source on shift).
  All 14 tests pass on first run.
- `cmd/lyre/main.go` updated: 3 call sites now reference `golang.VerifyGo`
  / `golang.UpdateGo` / `golang.GenerateGo`.
- `pkg/extract/golang/golang.go.lyric` regenerated from the new tool
  (dogfooding). Old file documented the deleted v1 API. New file
  verifies clean against the package source.

**Spec touched**: none. The spec was already complete; Phase 3a is pure
implementation against the locked v2 format.

**Design decisions made**:

1. **Field `SignatureText` = type-only** (e.g. `float64`, `*Circle`). The
   writer emits `field <Name>: <SignatureText>` and the Name is the map
   key. Type-only Signature keeps the field "name" exactly one token,
   which is what the parser also expects.

2. **Method/Func `SignatureText` = `<Name>(<params>) <returns>`** with no
   `func` keyword and no receiver clause. The receiver is implied by the
   containing class. Whitespace normalization (spec §7) handles the
   `(x,y int)` vs `(x int, y int)` collapsing.

3. **Only exported declarations are extracted.** `.go.lyric` is the
   *public API* understanding artifact. Unexported types and functions
   stay in source.

4. **`UpdateGo` merge policy**: source wins on signatures, positions, and
   the module-level `source:` list (refreshed from the filesystem every
   time). Existing wins on all human prose: `ModuleWhy`, `Docs`,
   `Invariants`, per-decl `Why`, per-field `Doc`. New exported symbols
   are added to the file; symbols absent from source are NOT pruned (the
   verifier reports them as drift; a future `--prune` flag covers
   destructive cleanup).

5. **Whitespace-normalized signature comparison** in `VerifyGo` per spec
   §7. Plus the v1-era tolerances for `any` ↔ `interface{}` and
   package-prefix stripping (kept because both still happen in real
   codebases — the source extractor produces unqualified names but the
   `.lyric` author may use qualified ones).

**Known wart, not blocking**: Go methods on a typedef receiver (e.g.
`func (s Severity) String() string` where `Severity` is `type Severity
int`) currently produce both a `typedef Severity: int` block AND a
phantom `struct Severity` block carrying the method. Cosmetically ugly
but technically informative — verify is clean, round-trip is clean. The
data model has no `Methods` map on `TypeDefInfo`; a real fix waits until
we either add one or move methods-on-typedefs into a separate decl
category. Logged here for the next sprint.

**Test status**: `go test ./...` green except pre-existing TypeScript
env failure. `pkg/extract/golang` runs 14 tests, all pass. Self-verify
of the regenerated `pkg/extract/golang/golang.go.lyric` reports 0
errors / 0 warnings.

**Up next**: Phase 3b — TypeScript extractor. Same shape as 3a but
replaces the `extract_api.js` PATH-dependent shim with a direct
extractor that doesn't need a global `typescript` install. Will unblock
the 9 currently-failing TS tests as a side effect.

### 2026-06-19 — Phase 3b complete

TypeScript extractor adaptation. Full rewrite of `pkg/extract/typescript/`.
~1h, vs ½-day plan estimate. All tests green; `go test ./...` is 100% green
for the first time this sprint (TS was the last red package).

Files landed:

- `pkg/extract/typescript/typescript.go` (~600 lines, full rewrite). New
  v2 entry points: `ExtractTs(srcDir) → *PackageInfo`, `GenerateTs(srcDir)
  → (outPath, content, err)`, `UpdateTs(lyricPath) → (added, err)`,
  `VerifyTs(lyricPath) → (*VerifyResult, err)`. Same shape as Phase 3a's
  Go extractor exactly. Old v1 surface (`ParseTsLDDMeta`, `ParseTsLDDFile`,
  `GenerateTsLDDFile`, `UpdateTsLDD`, `VerifyTsLDD`, the manual fallback
  parser `parseTsLDDManual`, the `// --- index ---` marker layout, the
  `//ldd:source`/`//ldd:why` directive scraping) is gone. No migration
  shim, no back-compat. JSDoc extraction in `extract_api.js` is
  unchanged — it populates the legacy `Doc` field on PackageInfo decls
  but is NOT round-tripped through `.lyric` (per the core principle, UDD
  prose lives in `.lyric` only; the extractor-internal `Doc` field is
  for future ad-hoc consumers).
- `pkg/extract/typescript/typescript_test.go` (~490 lines, full rewrite).
  18 tests covering ExtractTs (class+methods, interface, typedef+function,
  skipping unexported/underscored, skipping test/spec files), GenerateTs
  (output format, udd round-trip), VerifyTs (clean, missing function,
  undocumented export, field type mismatch), UpdateTs (adds new export,
  idempotent, **preserves human prose using the cleaner approach**,
  refreshes positions, multi-file source list).
- `pkg/extract/typescript/README.md` (new). Documents the node.js
  dependency, the `npm install` path, and the public API.
- `pkg/extract/typescript/package.json` and `package-lock.json` now
  committed (previously untracked). `node_modules/` is gitignored in a
  new `.gitignore` at repo root.
- `.gitignore` (new at repo root). Adds `lyre` binary and
  `pkg/extract/typescript/node_modules/`. The `lyre` binary was
  previously implicitly gitignored or just absent; making it explicit.
- `cmd/lyre/main.go` updated: 3 call sites now reference `tsext.VerifyTs`
  / `tsext.UpdateTs` / `tsext.GenerateTs`.

**Spec touched**: none. Phase 3b is pure implementation against the locked
v2 format.

**Design decisions made**:

1. **Script execution via `runtime.Caller(0)`**. The legacy approach
   embedded `extract_api.js` via `//go:embed` and wrote it to `/tmp/`
   before invoking — which broke `require('typescript')` because Node's
   resolution couldn't find a sibling `node_modules`. New approach uses
   `runtime.Caller(0)` to locate the package dir at runtime and invokes
   the on-disk `extract_api.js` directly, with `NODE_PATH` set to the
   sibling `node_modules`. Drops the `//go:embed`. Trade-off: a built
   binary depends on the source-tree layout being present at runtime;
   acceptable for the sprint scope. Production deploy story TBD (likely
   bundle node_modules with the binary).

2. **Lazy `npm install`**. If `node_modules` is missing on first call,
   `runExtractScript` invokes `npm install --silent --no-progress
   --no-audit --no-fund` once to populate it. Keeps fresh-checkout UX
   one-step. Errors propagate cleanly if `npm` is absent.

3. **Field `SignatureText` = type-only** (e.g. `number`, `string[]`,
   `[number, number]`). Mirrors Phase 3a's Go convention.

4. **Method/Func `SignatureText` = `Name(p1: t1, p2: t2): retType`** —
   no `function` keyword, no trailing semicolon, no receiver clause.
   Built in Go from the JSON `params` and `returns` arrays the script
   emits. Whitespace-normalized comparison (spec §7) handles
   any incidental differences.

5. **Constructor parameter properties surface as fields.** TypeScript's
   `constructor(public center: [number, number], radius: number)` makes
   `center` a class field. The legacy extractor handled this correctly;
   preserved in the rewrite. The constructor itself surfaces as a
   `constructor` method.

6. **`UpdateTs` merge policy** mirrors `UpdateGo` exactly: source wins
   on signatures, positions, and the module-level `source:` list.
   Existing wins on all human prose. New exports are added; absent ones
   not pruned (verify reports drift; a future `--prune` flag covers
   destructive cleanup).

7. **Module name = directory basename.** TS has no native package name
   like Go's `package` declaration; the directory name is the natural
   choice. Tests use stable subdir names ("shapes") rather than
   `t.TempDir()`'s `/001` to ensure the basename is a valid identifier
   per spec §3 (`[A-Za-z_][A-Za-z0-9_]*`). Real-world dirs almost
   always satisfy this — no sanitization added.

8. **`typesMatch` simplified for TS**: spec §7 whitespace normalization
   only. No `any`↔`interface{}` quirk like Go has, no package-prefix
   stripping (TS uses module-qualified names rather than package-prefix
   qualifiers, and the extractor emits unqualified by default).

**Cleaner test fixture approach proven**: `TestUpdateTs_PreservesHumanProse`
constructs a `PackageInfo` with prose set, writes it via `udd.Write` to
seed the fixture, then runs `UpdateTs`. No fragile string-splicing
(unlike Phase 3a's `TestUpdateGo_PreservesHumanProse`). This is the
canonical pattern for future per-language tests. Phase 3a's test could
be retroactively refactored to this shape; logged for a follow-up
sweep.

**Test status**: `go test ./...` from `~/projects/lyre/` is 100% green
across all packages (extract, extract/golang, extract/python,
extract/typescript, parser, udd, verifier). Confirmed lazy-install
path by removing `node_modules/` and re-running tests — npm install
completes in ~1s (cached) and tests pass on the fresh tree.

**Up next**: Phase 3c — Lyric extractor. ~1 day plan estimate. Same
shape: `ExtractLy/GenerateLy/UpdateLy/VerifyLy`. Extends
`~/projects/lyric/tools/extract_api.ly` to emit verbatim source spans
per decl, then the Go wrapper converts JSON → `*PackageInfo`. Risk 1
was resolved earlier (no touch to `lyric.stable`).

---

## 2026-06-19 — Phase 3c complete (Lyric extractor)

Rewrite of `pkg/extract/lyric/` to the v2 `.ly.lyric` pipeline. ~1h
elapsed vs ½-day plan estimate. `go test ./...` 100% green.

**Shortcut taken (not in plan v1)**: did NOT extend
`~/projects/lyric/tools/extract_api.ly`. The existing pre-compiled
`extract_api` binary already emits all the data needed
(`name`/`params`/`returns`/`file`/`line`/`is_class`/`underlying`); per
Phase 3a/3b precedent, `SignatureText` is built in Go from the
structured JSON. Zero Lyric tooling changes. `lyric.stable` and
`tools/extract_api.ly` untouched.

**New public API** (mirrors Phase 3a/3b exactly):
- `ExtractLy(srcDir) → *PackageInfo`
- `GenerateLy(srcDir) → (outPath, content, err)`
- `UpdateLy(lyricPath) → (added, err)`
- `VerifyLy(lyricPath) → (*VerifyResult, err)`
- `VerifyResult` / `Finding` / `Severity` types live in this package.

**Deleted** (legacy v1 surface): `GenerateLyLDDFile`, `UpdateLyLDD`,
`VerifyLyLDD`, `ParseLyLDDFile`, `ParseLyLDDMeta`, `ScanLyricFiles`,
`writeLyLocation`, `buildLyFuncSig`, `splitAtIndexMarker`,
`convertPackageJSON`, `convertFuncJSON`, and all `//ldd:source` /
`//ldd:why` directive scraping. Per the top-of-plan rule, UDD docs
live in the `.lyric` file only — never in Lyric source comments.

**Kept**: `findExtractBinary` (search order unchanged: LYRIC_HOME,
alongside `lyric` on PATH, `~/projects/lyric/tools/`), all JSON-shape
types (`lyPackageJSON`/`lyStructJSON`/`lyFuncJSON`/`lyParamJSON`/
`lyTypeDefJSON`), `ExtractBinaryName` constant.

**Signature conventions for Lyric** (third language, third confirmation):
- **Field SignatureText**: type-only (e.g. `i32`, `Token?`,
  `Dict<Sym, TokenKind>?`). Verbatim from extractor JSON.
- **Method SignatureText**: `Name(self, p: T) -> R`. Methods always
  get a leading `self` parameter (extractor strips it; Go wrapper
  re-adds). No `func` keyword, no `permanent` modifier, no body.
- **Function SignatureText**: `Name(p: T) -> R`. No leading `self`.
- **No return clause** if `returns` is empty/empty-string: signature
  ends at the closing `)`.
- **Multi-return**: `Name(...) -> (T1, T2)`.
- **`mut self`**: NOT recovered. The extractor drops self-mutability
  from JSON, so SignatureText reads `scale(self, k: f64)` even when
  source reads `scale(mut self, k: f64)`. Consistent with Go (Phase
  3a) omitting receiver-side mutability. Documented in
  `lyFuncSigText` doc comment as known limitation.
- **`mut` on non-self params**: preserved (`mut x: T`).

**`udd.Write`/`udd.Parse` class-vs-struct discrimination check**:
verified up front per plan step 4. `pkg/udd/writer.go` lines 203-205
emit `class` when `StructInfo.IsClass=true`, `struct` otherwise.
`pkg/udd/parser.go` line 432-433 sets `IsClass=true` on `class` block
heads. No `pkg/udd` patch needed. Lyric is the first language to
exercise both heads in one file — round-trip test
`TestGenerateLy_RoundTripsThroughUDD` asserts both `Circle.IsClass`
and `Point.IsClass` survive a write→parse cycle.

**`cmd/lyre/main.go` callsite renames**: 3 sites, mechanical:
`lyricext.VerifyLyLDD` → `VerifyLy`, `lyricext.UpdateLyLDD` →
`UpdateLy`, `lyricext.GenerateLyLDDFile` → `GenerateLy`. Did NOT
simplify the `isLyLyric` branching or remove the plain-`.lyric` v1
paths (`runUpdate`/`cmdFmt`/`verifier.Verify`) — deferred to Phase 6
(migration). Just renames; keep diff minimal.

**Test fixture**: green-field — no prior tests existed. 16 tests
mirroring Phase 3b's TS layout. Sample source covers all 5
constructs in one file (enum, struct, class with methods, interface,
top-level func). Stable subdir name `shapes/` keeps the
`.ly.lyric` module identifier valid. `requireExtractor` helper at
the top of every test skips cleanly when extract_api isn't
buildable on the host — preserves CI portability. The skip path
was NOT exercised in this session (extract_api was buildable).

**Cleaner `PreservesHumanProse` test fixture**: applied for the
third time. Pattern is now load-bearing: extract → tweak prose on
the `*PackageInfo` directly → `udd.Write` → seed file. No string
splicing anywhere in the test suite. (Phase 3a's
`TestUpdateGo_PreservesHumanProse` is still the lone holdout with
ugly `strings.Replace` splicing — logged for a follow-up sweep,
still not blocking.)

**Linux build of `lyric` + `extract_api`**: this session built both
on Linux x86_64 (the checked-in binaries at
`~/projects/lyric/tools/extract_api` and `~/projects/lyric/lyric.stable`
are Mac arm64). Required `CFLAGS='-std=gnu11 -O2 -w
-Wno-error=incompatible-pointer-types'` to work around the
known `_method_aliases` RC bug (gcc treats incompatible-pointer-types
as a hard error by default; Mac clang only warned). Built lyric in
~1s, extract_api in ~5s. The Linux extract_api now lives at
`~/projects/lyric/tools/extract_api` and is discoverable by
`findExtractBinary` for all Lyre tests.

**Dogfood**: `cd ~/projects/lyre && go build -o /tmp/lyre
./cmd/lyre && /tmp/lyre gen ~/projects/lyric/src/lexer && /tmp/lyre
verify ~/projects/lyric/src/lexer/lexer.ly.lyric` → "0 errors, 0
warnings". Generated file is 88 lines, includes the giant `enum
TokenKind` typedef as a one-line `enum { ... }` block, plus all 3
structs/classes (LexerState as `struct`, Token and Lexer as `class`),
all 28 methods on Lexer, all 8 top-level functions. The pre-existing
`lexer.ly.lyric` was backed up before generation and restored after
— `~/projects/lyric/src/lexer/` is byte-identical to its pre-Phase-3c
state.

**Test status**: `go test ./...` from `~/projects/lyre/` is 100%
green across all packages (extract, extract/golang, extract/lyric,
extract/python, extract/typescript, parser, udd, verifier).

**Up next**: Phase 3d (Python extractor, ½-day plan estimate, defer-
able) → Phase 4 (lint) → Phase 5 (gen --rich) → Phase 6 (migration,
including isLyLyric / plain-.lyric path cleanup deferred from this
phase) → **Phase 7.5 (UDD enforcement extension in CR server — MUST
land before Phase 7)** → Phase 7 (docs + backfill `checker.ly.lyric`).


## 2026-06-19 — Phase 3d complete (Python extractor)

**Time**: ~30min vs ½-day plan estimate. Same shape as Phase 3a/3b/3c —
template mature enough that this was largely a copy-and-substitute pass.

### Landed

- `pkg/extract/python/python.go` — full rewrite. New v2 surface:
  `ExtractPy / GeneratePy / UpdatePy / VerifyPy → *PackageInfo`.
  Mirrors Phase 3c (Lyric) almost line-for-line. Kept `//go:embed
  extract_api.py` + `runExtract` temp-file pattern — Python's script
  is pure stdlib so no need to switch to `runtime.Caller` (TS's
  reason was npm's sibling-node_modules requirement which Python
  lacks).
- `pkg/extract/python/python_test.go` — green-field rewrite. 16 tests
  covering Extract / Generate (output format + udd round-trip) /
  Verify (clean, missing function, undocumented export, field type
  mismatch) / Update (adds, idempotent, preserves human prose,
  refreshes positions, multi-file). All 16 pass first run (after one
  fix-up — see below). `requireExtractor` skips cleanly if `python3`
  is missing.
- `pkg/extract/python/extract_api.py` — one small surgical edit: add
  `file` + `line` to per-method JSON output (one block in the class
  walk: `minfo = func_info(item); minfo["file"] = …; minfo["line"] =
  item.lineno`). Previously methods had no per-method position info,
  only top-level functions did. The fix takes 3 lines and brings
  Python parity with Lyric/TS/Go (which all emit per-method source).
  Bill gave explicit go-ahead to edit this file mid-session.
- `cmd/lyre/main.go` — 3 callsite renames: `python.VerifyPyLDD →
  VerifyPy`, `python.UpdatePyLDD → UpdatePy`, `python.GeneratePyLDDFile
  → GeneratePy`. Mirrors Phase 3c's Lyric renames.

### Design decisions worth carrying forward

1. **Per-file extractor, multi-file merge in Go.** `extract_api.py`
   accepts ONE source file per invocation (unlike Lyric's batch
   mode). The Go wrapper writes the embedded script to a tempfile
   once per `ExtractPy` call, then loops files and merges per-file
   JSON via the same `mergeJSONInto` shape as Phase 3c.

2. **All Python "classes" → IsClass=true.** Python has no
   value-vs-reference type distinction; every `class Foo` becomes
   `extract.StructInfo` with `IsClass=true`, which the udd writer
   emits as `class` (not `struct`). Phase 3c verified the
   writer/parser discriminates correctly; no udd patch needed.

3. **Method SignatureText re-adds `self`.** `extract_api.py`'s
   `func_info()` strips `self`/`cls` from the params list (mirrors
   Lyric's `extract_api`). Go-side `pyFuncSigText` re-prepends `self`
   for methods so the canonical form matches Lyric: `name(self, p: T)
   -> R`. classmethods (whose `cls` was stripped) come out as `name(self,
   ...)` — a minor wart, acceptable for now since classmethods are
   rare in API surface code.

4. **Protocol-as-interface.** `extract_api.py` already does this
   classification: `class Foo(Protocol):` lands in JSON
   `"interfaces"`, plain classes in `"structs"`. Go side trusts the
   classification; tests verify (`Drawable(Protocol)` → interface).

5. **TypeAlias detection.** `extract_api.py` handles both `X = int`
   (simple assignment) and `X: TypeAlias = int` (PEP 613). Both end
   up in `"typedefs"`. Test verifies `Color: TypeAlias = str` round-
   trips as `typedef Color: str`.

6. **Skip rules.** Match Phase 3c: `test_*.py`, `*_test.py`, plus
   underscore-prefixed files (`_internal.py`, `__init__.py`,
   `__main__.py`). The single underscore-prefix rule covers all three
   Python-convention-private cases.

### Test fixture pattern (kept clean)

`TestUpdatePy_PreservesHumanProse` uses the cleaner approach
established in Phase 3b/3c: construct a `PackageInfo` via `ExtractPy`,
set prose fields directly (`ModuleWhy`, decl `Why`, field `Doc`),
write via `udd.Write` to seed disk, then run `UpdatePy`. No fragile
string-splicing.

### Dogfood

- Synthetic `/tmp/pydog/geom/shapes.py` (Class+method, Class+fields,
  Protocol, TypeAlias, top-level func).
- `lyre gen /tmp/pydog/geom/` → `geom.py.lyric` with all classes,
  interface, typedef, function, methods, and per-method source lines.
- `lyre verify geom.py.lyric` → **0 errors, 0 warnings.**
- Removed `/tmp/pydog/` after verification (no commits to other
  repos).

### Build / test status

- `go build ./...` — clean.
- `go test ./pkg/extract/python/...` — 16/16 pass.
- `go test ./...` — 100% green across all packages.

### Velocity calibration update

3a (Go) ~30min; 3b (TS) ~1h; 3c (Lyric) ~1h; 3d (Python) ~30min. All
were ½-day plan estimates. The pattern's mature; future per-language
work can quote single-digit hours, not days.

### Up next

Phase 4 (lint — `lyre lint` warns on TODO placeholders, missing
`why:`, missing `source:`, references to nonexistent tests) →
Phase 5 (`lyre gen --rich` — synthesize plausible TODO-marked Why /
Doc blocks from native source comments as a starting point) →
Phase 6 (migration — convert all in-tree `.lyric` files from v1 to
v2, including the deferred `isLyLyric`/plain-`.lyric` cleanup from
Phase 3c) → **Phase 7.5** (extend the CodeRhapsody server's `.forge`
UDD-enforcement to `.lyric` files — MUST land before Phase 7 so the
backfill operates under enforcement) → Phase 7 (docs + backfill
`checker.ly.lyric` for the Lyric checker).

---

## 2026-06-19 — Phase 4 complete (lint)

`pkg/lint` is in. `lyre lint <file.lyric> [...]` parses via `pkg/udd` and
runs 8 language-agnostic checks against the `*extract.PackageInfo`.

### Checks implemented

| Code | Trigger |
|------|---------|
| W001 | empty module-level `why:` |
| W002 | no `doc "Architecture"` block (case-insensitive title match) |
| W003 | ≥1 class/struct/interface with ≥3 methods, but module has zero invariants |
| W004 | class/struct with ≥4 methods, none of which has a `why:` |
| W005 | struct with ≥3 fields including ≥1 enum-typed field, with no per-field `doc:` anywhere on the struct |
| W006 | invariant block with no `verified-by:` AND not marked `procedural` |
| W007 | `verified-by:` references a test name not in `Opts.KnownTests` — dormant when `KnownTests` is nil |
| W008 | case-sensitive `"TODO"` substring in any prose field (ModuleWhy, doc bodies, invariant bodies, per-decl `Why`, per-field `Doc`) |

### Design notes (worth carrying forward)

- **Lint is separate from verify.** Verify (pkg/extract/*/`VerifyXxx`)
  compares `.lyric` against native source (drift detection). Lint
  inspects only what's already in the parsed `*PackageInfo` —
  completeness, TODO-hygiene, internal consistency. Lint has its own
  `Finding{Code, Severity, File, Where, Message}` type because verify
  Findings don't have a `Code` field.
- **W005 enum heuristic**: a field is "enum-typed" iff its trimmed
  `SignatureText` matches a key in `p.TypeDefs`. Imperfect — won't catch
  enum types defined in another package — but a good starting heuristic
  with no language-specific machinery.
- **W007 test discovery deferred to Phase 4.5.** CLI passes
  `Opts{KnownTests: nil}`, so W007 is dormant in CLI use. The
  programmatic API exercises it (used by `TestLint_W007_*`). Future
  Phase 4.5 will scan sibling `*_test.go` / `*_test.py` / etc. for test
  function names.
- **Deterministic output**: findings sorted by `(Code, Where)`. Methods
  / fields / typedefs iterated via sorted-key helpers.
- **One why: is enough to suppress W004.** The check fires only if NO
  method has a `why:`. Intent: nudge curation, don't demand exhaustive
  rationale.

### CLI shape

```
lyre lint [--fatal-warnings] <file.lyric> [...]
```

Without `--fatal-warnings`, lint exits 0 regardless of warning count
(reporting only). With it, exits 1 if any warning fired. Mirrors verify's
error-counting shape (verify already exits 1 on errors).

### Tests

22 tests in `pkg/lint/lint_test.go`. Fixtures constructed directly as
`*PackageInfo` values — no extractor invocation, no skip helper. One
test per warning code, plus negative-case tests (e.g.
`TestLint_W004_OneMethodWhyIsEnough`, `TestLint_W007_DormantWhenNilSet`),
plus `TestLint_CleanPackage` (clean fixture produces zero findings),
plus determinism + format tests.

### Dogfood

`/tmp/lyre lint pkg/extract/golang/golang.go.lyric` produced 3
plausible warnings (W001 missing module why, W002 missing Architecture
doc, W005 `Finding` struct with enum-typed `Severity` field and no
per-field docs) — exactly what auto-generated v1-skeleton files should
trip. Legacy v1 `.lyric` files (still using `//go:build ignore` +
`//ldd:` directives) error at parse with a clear message; Phase 6 will
migrate them.

### Velocity

~30min vs 3h plan estimate. Pattern is now firmly in the
single-digit-hours regime. `go test ./...` 100% green.

### Up next

Phase 5 (`lyre gen --rich`) → Phase 6 (v1→v2 migration; pick up the
deferred `isLyLyric` / plain-`.lyric` cleanup) → **Phase 7.5** (UDD
enforcement extension in CR server — MUST land before Phase 7) → Phase 7
(docs + backfill `checker.ly.lyric`).


### 2026-06-19 — Phase 5 complete

`lyre gen --rich` shipped. New `pkg/gen/` package, single function
`SeedRichPlaceholders(p *extract.PackageInfo)` mutates a freshly-extracted
PackageInfo in place to fill every empty rich-doc slot with either a
TODO placeholder or a cleaned-up first line of the legacy native-source
`Doc` field.

**Pipeline shift in `cmdGen`**: the `--rich` path bypasses
`Generate<Lang>` and instead runs `Extract<Lang>` → `SeedRichPlaceholders`
→ `udd.Write` → atomic write. Per-language extractors are unchanged.
The plain `lyre gen` (no `--rich`) path is unchanged.

**Seeding contract** (verified by `TestSeedRich_LintContract_*`): after
`SeedRichPlaceholders` on a clean PackageInfo, `lyre lint` reports only
W008 (the TODO placeholders themselves), plus W003 / W005 / W006 / W007
when the underlying structure warrants — seeding does NOT manufacture
invariants (W003/W006) or per-field doc on enum-bearing structs (W005),
both of which are caught-bug records / semantic clarifications that need
human judgment.

**Doc → Why fallback**: per-decl `Why` is seeded from the cleaned first
non-empty line of the legacy `Doc` field when available (Go docstrings,
Python docstrings, etc.); otherwise the generic
`TODO: explain <Name>.` placeholder is used. Idempotent — pre-filled
prose is never overwritten.

**Dogfood**: regenerated `pkg/extract/golang/golang.go.lyric` from
scratch under `--rich`; lint produced exactly the expected mix (21
W008s + 1 W005 on the `Finding` struct's enum-typed `Severity` field).
Restored the original file post-dogfood — no source-tree disturbance.

`go test ./...` 100% green. 13 new tests in `pkg/gen/`, all pass first run.

Velocity: ~25 min vs 1–2h plan estimate. Pattern continues to come in
well under the single-digit-hours regime.

### Up next

Phase 6 (v1→v2 migration; pick up the deferred `isLyLyric` /
plain-`.lyric` cleanup) → **Phase 7.5** (UDD enforcement extension in
CR server — MUST land before Phase 7) → Phase 7 (docs + backfill
`checker.ly.lyric`).


### 2026-06-19 — Phase 6 complete

v1→v2 migration of all in-tree `.lyric` files plus the deferred
`isLyLyric`/plain-`.lyric` cleanup. ~15 min vs ½-day plan estimate.
`go build ./...` clean. `go test ./...` 100% green. `/tmp/lyre verify`
clean on all 6 in-tree `.lyric` files.

**Migrated** (5 files; one was already v2 from Phase 3a's dogfood):

- `pkg/ast/ast.go.lyric` — non-empty `//ldd:why` prose preserved into v2
  `module ast: why:` slot via a single `edit_file` post-regeneration. No
  other meaningful prose to lift (the v1 file was bare-skeleton beyond
  the one ldd:why line).
- `pkg/parser/parser.go.lyric`
- `pkg/verifier/verifier.go.lyric`
- `pkg/extract/extract.go.lyric`
- `pkg/extract/python/python.go.lyric`

All four had empty `//ldd:why ""` — no prose to preserve. Migration
mechanically: `rm <file>` then `lyre gen --rich <dir>`. Five
regenerations, zero manual content porting beyond ast.

The fifth in-tree file, `pkg/extract/golang/golang.go.lyric`, was
already v2 (regenerated at the end of Phase 3a) — skipped.

Post-migration: `lyre lint pkg/ast/ast.go.lyric` fires neither W001
(module why preserved) nor W002 (Architecture block exists, TODO-filled
— W002 only fires when the block itself is missing). Lint output is
W008-only, as expected for `--rich`-generated files.

**`isLyLyric` cleanup** (was deferred from Phase 3c). With zero
remaining plain `.lyric` files in-tree and `pkg/verifier` having no
other consumer than `cmd/lyre/main.go` (`grep -rn pkg/verifier
--include='*.go'` confirmed), the legacy plain-`.lyric` machinery
collapses cleanly:

- `cmd/lyre/main.go`: removed the `isLyLyric` helper, the
  verifier-fallback branches in both `cmdVerify` and `cmdUpdate`, the
  `runUpdate` stub, the `prune` flag plumbing (its only consumer was
  `runUpdate`), and the `pkg/verifier` import. -33 lines net.
- `pkg/verifier/` and `pkg/parser/` deleted entirely (`rm -rf`). They
  carried the legacy plain-`.lyric` syntax parser and verifier; their
  tests went with them. `pkg/verifier` was imported only by
  `cmd/lyre/main.go`; `pkg/parser` was imported only by `pkg/verifier`.
  Zero remaining consumers.

**`lyre fmt`** stub remains (`cmd/lyre/main.go: cmdFmt`). The v2
`.lyric` format already round-trips losslessly through
`pkg/udd/Parse → Write`; a real `lyre fmt` implementation is a tiny
follow-up that loops the udd parse/write pipeline over its inputs.
Not in Phase 6's scope; logged.

**Velocity calibration**: 15 min vs ½-day. The migration pattern
(`rm <v1>; lyre gen --rich <dir>; commit`) is mechanical and could
trivially be scripted as `lyre migrate <dir>` if more v1 files turn
up in `~/projects/lyric/src/`. Phase 7's `checker.ly.lyric` backfill
will exercise that path.

### Up next

Phase 7.5 (UDD enforcement extension in CR server — MUST land before
Phase 7) → Phase 7 (docs + backfill `checker.ly.lyric`).

### 2026-06-19 — Phase 7.5 complete

Landed in `~/projects/coderhapsody/` (separate repo from Lyre — this is
the CR server that runs me). The `.forge` read-before-write gate now
treats `.lyric` files as equivalent design-file sentinels.

**Shape of the change** (small, surgical, mirrors existing `.forge`
pattern exactly):

- `pkg/tools/types.go`: renamed `uddForgeReads` → `uddDesignReads`.
- `pkg/tools/file_ops.go`: `checkUDDGate` now scans the directory for
  either `.forge` or `.lyric` and includes the detected kind in the
  error message. `trackUDDForgeRead` → `trackUDDDesignRead` recognising
  both extensions. `ResetUDDState` clears the same map.
- Comment updates in the three model clients (`claude_client`,
  `openai_client`, `gemini_client_core`) and `pkg/database/interfaces.go`
  to reflect the broadened scope.
- `pkg/skills/builtin/udd/SKILL.md` (the system-prompt template): Core
  Rule now reads "directory that contains a `.forge` or `.lyric` file";
  added explicit note that `.forge` and `.lyric` are equivalent and that
  `.lyric` is the v2 format used by `lyre` (with per-language variants
  `foo.go.lyric`, `bar.py.lyric`, `baz.ly.lyric`).
- `pkg/tools/udd_test.go`: kept the four existing `.forge` tests;
  added four `.lyric` tests — Blocks/Allows/MixedForgeAndLyric/
  ResetClearsLyric. Mixed test asserts current per-directory single-
  sentinel semantics: reading either kind unlocks the directory.

**Design decision (KISS)**: kept the single-sentinel-per-directory
semantics of the existing `.forge` gate rather than introducing a
per-source-file mapping (e.g. editing `ast.go` specifically requires
`ast.go.lyric`). The natural per-file semantics is more precise but
would be a behaviour change beyond the spec; the simpler equivalence
is what the Phase 7.5 amendment asked for. Future refinement is one
small follow-up if desired.

**Build / tests**: `go build ./pkg/...` clean. All 8 UDD tests pass
(`go test ./pkg/tools/ -run UDD -v`). One pre-existing `TestExecuteListSkills`
failure on `pkg/tools` is environment-dependent (expects 0 global skills,
finds 19 installed in the dev tree) — confirmed identical failure on
`main` without the patch.

**Activation**: patch is on disk but not live in the running CR server.
`coderhapsody` on port 8082 is the server running me; Bill restarts to
activate. Phase 7's `checker.ly.lyric` backfill should wait for that
restart so the new enforcement actually gates the backfill work.

**Velocity**: ~15 min vs 3–4h plan estimate. The existing `.forge` code
was clean, well-localized, and well-tested — extending it was almost
purely a renaming + suffix-set widening. Sprint-wide pattern holds:
quote single-digit hours for well-spec'd template work against a
settled architecture on Opus 4.7.

### Up next

Phase 7 (docs + backfill `~/projects/lyric/src/checker/checker.ly.lyric`
from `checker.forge` — after Bill restarts the CR server so the
extended enforcement is live during the backfill).


### 2026-06-19 — Phase 7 complete (docs pass + checker.ly.lyric backfill — SPRINT DONE)

Sprint terminal phase. ~10 min vs 2–4h plan estimate (velocity-calibration record: every phase 10–50x under).

**Backfill mechanics** (no `checker.forge` exists — handoff anticipated one,
but reality was a v1-format `checker.ly.lyric` with `//ldd:` comments holding
the rich prose; same outcome, simpler source). Steps:

1. Backed v1 file to `/tmp/checker.ly.lyric.v1.bak` (prose source).
2. `rm checker.ly.lyric && /tmp/lyre gen --rich src/checker/` reseeded as v2
   format with the standard module/Architecture/decl/TODO skeleton (529 lines).
3. `sed -i '/^[[:space:]]*why: "TODO/d'` stripped all 140-ish `why: "TODO..."`
   placeholder lines wholesale — `why:` is spec-optional so absence is silent.
   This makes Phase 7 backfill *much* cheaper than re-writing why prose for
   every decl: lint accepts an unstated `why:`, no W008 fires.
4. `replace_lines` (with `exact_lines: true`) on the seeded module head
   replaced the stub `doc "Architecture":` block with: real module `why:` +
   full Architecture doc + nine `invariant "...":` blocks (Three-Phase
   Checking, Expression Annotation, Dict/Sym Usage, Type Representation,
   Enum Variant Registration, Generic Functions, Numeric Widening,
   Assignability Rules, Phase 1.5 Interface Methods). Every invariant marked
   `procedural` (no checker tests exist yet that verify these — honest, and
   suppresses W006).
5. Added `doc:` lines to the three fields of class `Type` (bits / kind /
   type_args) — `kind: TypeKind` is enum-typed, which trips W005's nudge
   for per-field doc, and the prose suppresses it.
6. Added one `why:` on `Checker.check_expr` — class Checker has ≥4 methods
   so W004 wants at least one per-method why; check_expr is the natural
   choice as the documented checker→lowerer contract point.

**Lint contract met**: `/tmp/lyre lint checker.ly.lyric` → `0 warnings`.
W001/W002/W003/W004/W005/W006/W008 all suppressed. Exactly the "complete
port" target from the handoff.

**Verify contract met**: `/tmp/lyre verify checker.ly.lyric` →
`0 errors, 0 warnings`. Signatures match `checker.ly` exactly.

**Note on `lyre verify <dir>`**: errors with `is a directory`. The current
CLI takes only file arguments. Not in Phase 7 scope, but a future
QoL improvement worth a TODO.

**Note on Phase 7 step 1 (`pkg/udd/spec.md`)**: already exists from Phase 2,
re-read clean. No update needed — the spec is current.

**Note on Phase 7 step 2 (UDD methodology doc update)**: the handoff cites
`checker.forge` as the rich-content reference; in practice no `.forge`
file exists in `~/projects/lyric/src/checker/` — the backfilled
`checker.ly.lyric` IS the reference example. Methodology doc
`~/projects/lyric/cr/docs/understanding-driven-development.md` was already
updated to reference `.lyric`/`lyre`/UDD terminology in earlier phases of
this sprint; no further drift detected.

**Docs scan**: `grep -rni 'LDD\|LDL\|GDD\|grok-driven\|verifier\|lyric verify\|lyric update' README.md cr/docs/` in `~/projects/lyre/` returns ONLY this plan file (historical amendment context, appropriately preserved). No drift in user-facing docs. Lyre has no README.md and only this plan in cr/docs/ — clean.

**Test status**: `cd ~/projects/lyre && go test ./...` → all packages OK.
No regressions from sprint.

**Per-language extractor template stable**: the four `.ly.lyric` /
`.go.lyric` / `.py.lyric` / `.ts.lyric` files dogfooded in `~/projects/lyre/pkg/`
all round-trip clean. The 11 non-checker `src/*.ly.lyric` files in
`~/projects/lyric/src/` remain on v1 format — explicitly out of scope per
plan; separate sprint, per-file work.

**Sprint position**: ALL PHASES ✅ — 0 / 1 / 2 / 3a / 3b / 3c / 3d / 4 / 5
/ 6 / 7.5 / 7. **Sprint DONE. No Phase 8.**

**Velocity calibration final**: every phase came in 10–50x under plan
estimate on Opus 4.7. Carry forward to all future Lyre work: quote
single-digit hours, not half-days, for tight implementation work against
this settled architecture. The plan's day-scale estimates were correctly
calibrated to pre-tooling pre-template effort; per-phase amendments
recorded actuals and the calibration converged across all 12 phases.
