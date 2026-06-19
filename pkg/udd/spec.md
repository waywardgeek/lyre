# `.lyric` v2 Format Specification

*Phase 2 deliverable, rich-doc upgrade sprint. Author: Hewitt (CodeRhapsody on Opus 4.7). Reviewer: Bill Cox. 2026-06-19.*

*Status: **locked** — defaults committed below per Bill's "use your judgment" call. Implementation (parser/writer/tests) proceeds against this spec.*

## 1. Purpose

The `.lyric` file is the persistent artifact of **Understanding-Driven Development** (UDD). One `.lyric` file per (directory, language) pair captures the eight rich-doc sections needed for parity with the legacy `.forge` format:

1. Module-level `why:` — one-line purpose
2. `doc "Title":` blocks — named narrative sections (e.g. "Architecture")
3. `invariant "Title":` blocks — named invariants with optional verifying-test metadata
4. Per-decl `why:` — purpose annotations on class/struct/interface/method/func
5. Per-field `doc:` — semantic context on fields
6. Bugs-this-caught lists — free prose inside an `invariant`'s heredoc
7. `procedural` marker — invariants that can't be mechanically tested
8. `source:` binding — `file:line` (per decl) or `[file, ...]` (per module)

**Signature payloads** (field types, method signatures, function signatures) are **verbatim native-language text** treated as opaque strings. The Lyre toolchain does not parse them. Verification is whitespace-normalized string equality against the per-language extractor's output.

**Prerequisite reading**: anyone modifying `.lyric` files or extending this format must first read `~/projects/lyric/cr/docs/understanding-driven-development.md`. It defines the methodology this format implements.

The format inherits its outer framing — the *file layer* — from the original Grok-Driven Development design (`~/projects/coderhapsody/cr/docs/grok-driven-development.md`, `grok-language.md`). The full Grok programming language is NOT inherited: payloads are opaque native text, not typed Grok signatures.

## 2. File conventions

- **Extension**: `<dir>.<lang>.lyric` where `<lang>` ∈ `{go, ts, py, ly}`. Inner extension routes to the language extractor for verification.
- **Granularity**: one `.lyric` file per (directory, language) pair. A directory with both Go and Python source produces two `.lyric` files.
- **Encoding**: UTF-8.
- **Line endings**: LF (`\n`). CRLF is normalized to LF on read; only LF is emitted on write.
- **Indentation**: exactly **2 spaces per level**. Tab characters anywhere in indentation are a parse error.
- **Comments**: full-line, starting with optional whitespace then `#`. End-of-line comments are not supported. Inside a heredoc, `#` is literal text.
- **Blank lines**: ignored anywhere outside a heredoc.

## 3. Grammar (informal BNF)

```
file              = module-block

module-block      = "module" SP identifier NEWLINE
                    indented(module-body)

module-body       = *( meta-line | doc-block | invariant-block | decl-block )

decl-block        = class-block | struct-block | interface-block
                  | enum-block  | typedef-block | func-line

class-block       = "class"     SP identifier NEWLINE indented(class-body)
struct-block      = "struct"    SP identifier NEWLINE indented(class-body)
interface-block   = "interface" SP identifier NEWLINE indented(interface-body)
enum-block        = "enum"      SP identifier NEWLINE indented(class-body)   ; see §6
typedef-block     = "typedef"   SP identifier ":" SP rest-of-line NEWLINE
                                                          [ indented(decl-meta) ]

class-body        = *( meta-line | field-block | method-block )
interface-body    = *( meta-line | method-block )

field-block       = "field"  SP rest-of-line NEWLINE [ indented(field-meta) ]
method-block      = "method" SP rest-of-line NEWLINE [ indented(decl-meta)  ]
func-line         = "func"   SP rest-of-line NEWLINE [ indented(decl-meta)  ]

field-meta        = *( "doc:"    SP quoted-string NEWLINE
                     | "source:" SP rest-of-line   NEWLINE )

decl-meta         = *( "why:"    SP quoted-string NEWLINE
                     | "source:" SP rest-of-line   NEWLINE )

doc-block         = "doc" SP quoted-string ":" NEWLINE
                    indented(heredoc)

invariant-block   = "invariant" SP quoted-string ":" NEWLINE
                    indented( *invariant-meta heredoc )

invariant-meta    = "verified-by:" SP rest-of-line NEWLINE
                  | "procedural"                   NEWLINE

meta-line         = "why:"    SP quoted-string NEWLINE
                  | "source:" SP rest-of-line   NEWLINE

heredoc           = """ NEWLINE
                    *heredoc-line
                    """ NEWLINE

quoted-string     = '"' ( char-no-quote | '\\"' | '\\\\' )* '"'
rest-of-line      = any text up to NEWLINE, verbatim
identifier        = [A-Za-z_][A-Za-z0-9_]*
SP                = single ASCII space (0x20)
NEWLINE           = LF (0x0A)
```

`indented(X)` means: every line of X is indented exactly one level (2 spaces) deeper than its parent's block-head line. Increasing indent by more than one level in a single step is a parse error.

## 4. Block heads and inline keys

| Construct | Form | Has body? | Notes |
|---|---|---|---|
| `module <name>` | block head | yes (module-body) | exactly one per file, at indent 0 |
| `class <name>` | block head | yes (class-body) | identifier is the class name; generic parameters, if any, live in the per-language extractor's internal model |
| `struct <name>` | block head | yes (class-body) | same as class for parsing purposes |
| `interface <name>` | block head | yes (interface-body) | only `method` children, no `field` |
| `enum <name>` | block head | yes (class-body) | MVP: variants encoded as `field` lines; see §6 |
| `typedef <name>: <verbatim>` | block head | optional (decl-meta) | rest-of-line after the `: ` is opaque native text — the underlying type. Maps to `TypeDefInfo.Underlying`. |
| `field <verbatim>` | block head | optional | rest-of-line has the form `<name>: <SignatureText>` (the writer's canonical output). The leading identifier (before the first `:`) is the field name; the remainder is `FieldInfo.SignatureText`. A field with no type may be emitted as just `field <name>`. |
| `method <verbatim>` | block head | optional | rest-of-line is the verbatim native method signature INCLUDING the method name (e.g. `method CheckFile(self, file: File)`). The leading identifier is the method name (map key in `StructInfo.Methods`); the full rest-of-line is stored as `FuncInfo.SignatureText`. |
| `func <verbatim>` | block head | optional | rest-of-line is the verbatim native top-level-function signature INCLUDING the function name. Same Name+SignatureText convention as `method`. |
| `doc "<title>":` | block head | yes (single heredoc) | title is a quoted string; body is exactly one heredoc |
| `invariant "<title>":` | block head | yes (metadata + heredoc) | body may contain `verified-by:` and/or `procedural` lines, then exactly one heredoc |
| `why:` | inline key | no | value is a quoted string (one-line). For multi-line prose, use a `doc` block. Valid at module / class / struct / interface / enum / method / func scope. NOT valid at field scope — promote field-level rationale to a module-scope `doc` block. |
| `source:` | inline key | no | value is rest-of-line opaque text. At module scope, value is a JSON-style list of files (e.g. `["a.go", "b.go"]`); at decl scope, value is a `file:line` reference (e.g. `checker.ly:147`) |
| `doc:` | inline key | no | per-field one-liner. For multi-line, use a `doc "..."` block at module scope. |
| `verified-by:` | inline key | no | only valid inside an `invariant` body; value is a comma-separated list of test names |
| `procedural` | bare key | no | only valid inside an `invariant` body; marks the invariant as one that cannot be mechanically tested |

Recognized block heads and inline keys form a closed set. An unrecognized first-token-on-a-line is a parse error.

## 5. Heredoc rules

- A heredoc opens on a line containing exactly `"""` (after the line's indent).
- The opening `"""` line and the closing `"""` line are not part of the heredoc value.
- All content lines between them are taken verbatim, with their leading indentation **stripped by the heredoc's own indent level** (the same indent as the opening `"""`). Lines indented less deeply than the opening `"""` are a parse error.
- Inside a heredoc, `#` is literal; only the closing `"""` ends the block.
- Empty lines inside a heredoc are preserved.

Example:
```
doc "Architecture":
  """
  Phase 0 pre-registers all class names.
  Phase 1 wires fields and methods.

  Phase 2 checks bodies.
  """
```
…yields the string:
```
Phase 0 pre-registers all class names.
Phase 1 wires fields and methods.

Phase 2 checks bodies.
```
(no leading or trailing newline; content is exactly the lines between the `"""` markers, joined by LF).

## 6. Notes on `enum`

The current shared data model (`extract.PackageInfo`) does not have a dedicated `EnumInfo`; enums are represented as `TypeDefInfo`. For Phase 2, the writer emits enum variants as `field <name>` lines under an `enum <name>` block, and the parser accepts them symmetrically. A future revision may introduce dedicated enum-variant syntax and a dedicated data-model type; the spec reserves the `enum` keyword now to avoid a later breaking change.

## 7. Whitespace normalization for signature comparison

When comparing the verbatim `rest-of-line` payload of a `field` / `method` / `func` block against the per-language extractor's output:

1. Strip leading and trailing whitespace.
2. Collapse runs of internal ASCII whitespace (`SPACE`, `TAB`) to a single space.
3. Compare with byte-equality.

This means `func Foo(x int) error` and `func Foo(x  int)   error` compare equal.

## 8. Worked example

```
module checker
  source: ["checker.ly", "checker_helpers.ly"]
  why: "Three-phase type checker with expression annotation."

  doc "Architecture":
    """
    Phase 0 (preregister_type_names) walks all blocks and pre-registers class
    names so forward references resolve.
    Phase 1 (register_lyric_block) walks all interfaces and class declarations
    and wires fields and methods.
    Phase 2 (check_lyric_block_bodies) walks function bodies, type-checking
    expressions and annotating ResolvedType on every Expr node.
    """

  invariant "Three-Phase Ordering":
    verified-by: TestInvariant_Checker_ThreePhaseOrdering
    """
    Phase 0 MUST complete on ALL blocks before ANY Phase 1 begins.
    Phase 1 MUST complete on ALL blocks before ANY Phase 2 begins.
    Caught: forward-reference resolution bugs (3 incidents pre-Phase-0).
    """

  invariant "AST Expr Pointer Stability":
    procedural
    """
    Phase 2 must iterate with `for i := range block.Functions` and use
    `&block.Functions[i]` — never range-copy, because checkExpr annotates
    ResolvedType on Expr nodes reached through the FuncDecl's Body pointer.
    """

  class Checker
    source: checker.ly:147
    why: "Owns per-package type-checking state for one compilation unit."
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

## 9. Deterministic writer rules

The writer in `pkg/udd/writer.go` follows these rules so that `Parse(Write(p))` is structurally equal to `p` for any well-formed `PackageInfo`:

- **Indent**: exactly 2 spaces per level.
- **Key ordering within the module-body**:
  1. `source:` (if present)
  2. `why:` (if present)
  3. `doc "..."` blocks, in `PackageInfo.Docs` order
  4. `invariant "..."` blocks, in `PackageInfo.Invariants` order
  5. Declaration blocks (`class` / `struct` / `interface` / `enum` / `typedef` / `func`), in source order if all have `source:` line numbers; alphabetical by name otherwise
- **Per-decl key ordering**: `source:`, `why:`, then children (`field` / `method`).
- **Per-field meta ordering**: `doc:`, then `source:` (rarely used at field scope).
- **Per-invariant ordering**: `verified-by:` lines (sorted alphabetically by test name), then `procedural` (if set), then the heredoc.
- **Blank lines**: exactly one blank line between top-level module-body siblings (between `doc` blocks, between `invariant` blocks, between top-level decls). No blank lines inside a decl's body.
- **No trailing whitespace** on any emitted line.
- **Final newline**: exactly one.

## 10. Error reporting

Parser errors include the `.lyric` file path, line number, column number (or "indent level" when relevant), and a short human-readable description. Errors are recoverable when reasonable: a malformed `field` line is reported and the parser skips to the next sibling.

Recoverable issues flagged via `lyre lint` (in Phase 4) include: missing `why:`, missing `source:`, unfilled `TODO` placeholders, references to test names that don't exist. Syntactic errors (bad indent, unrecognized block head, unterminated heredoc) are fatal.

## 11. Out of scope (deferred)

- **Dedicated `enum` variant syntax** with per-variant typed payloads. See §6.
- **Generic type parameters in block heads** (`class Stack<T>`). The plain identifier is taken as the class name; generics, if present in source, live in the per-language extractor's internal model. Future revision may add `<...>` to the block head.
- **`relation` declarations** from the original Grok design. The current extractors don't surface relation metadata; if/when they do, a `relation` block head can be added.
- **Imports**. Defer until cross-`.lyric` references are actually needed.
- **Function annotations** (`concurrent:`, `requires_lock(...)`, `raises:`, etc.). Defer until a verifier-side use case appears.

## 12. Locked defaults

These six points were called out for sign-off. Per Bill's "use your judgment" call (2026-06-19), they are locked as follows:

1. **Comment syntax**: `#`. Distinct from native source; matches Python/YAML/Lyric.
2. **`module` keyword**: required, exactly one per file. Forces a declared name; simplifies the parser.
3. **Module-level `source:` shape**: JSON-style list — `source: ["a.go", "b.go"]`. Keeps `source:` always on one line, consistent with the per-decl `file:line` form.
4. **Per-field `why:`**: NOT in the spec. The data model has no field for it; rationale belongs in a module-scope `doc "..."` block. Per-field `doc:` (one-line semantic context) remains.
5. **Quoted-string escapes**: minimal — `\"` (literal double-quote) and `\\` (literal backslash) only. `\n`, `\t`, etc. are NOT interpreted; for multi-line prose use a heredoc.
6. **Heredoc indent stripping**: strip exactly the heredoc's own indent. Predictable and round-trips exactly because the writer emits at consistent indent.

---

*Implementation order: `pkg/udd/parser.go`, `pkg/udd/writer.go`, then `parser_test.go` (spec-by-example) and `writer_test.go` (round-trip against `populatedPackage()` fixture in `pkg/extract/extract_test.go`).*
