# CDD Module Design

## Executive Summary

The `cdd` module (Context-Driven Development) is the core persistence layer for the Lyre toolchain. It implements the parser and writer for the `.lyric` v2 format, a human-readable, indent-significant DSL designed to capture the architectural intent and invariants of a software module. 

The primary goal of this module is to provide a lossless round-trip between the in-memory `extract.PackageInfo` data model and its on-disk representation. By treating native-language signatures as opaque strings and focusing on rich-doc sections (why-prose, doc blocks, and invariants), the `cdd` module enables a development workflow where documentation and code are kept in tight synchronization without requiring the toolchain to fully parse every target language.

The module follows the formal [spec.md](spec.md), which defines the grammar, whitespace rules, and deterministic writer requirements. It is designed to be language-agnostic, supporting Go, TypeScript, Python, and Lyric by delegating signature extraction to language-specific extractors while providing a unified framing for documentation.

## File Inventory

- [parser.go](parser.go): A recursive-descent parser that transforms `.lyric` source text into an `extract.PackageInfo` structure. It handles indent-based scoping, heredoc extraction, and quoted-string unescaping.
- [writer.go](writer.go): A deterministic writer that serializes `extract.PackageInfo` into the canonical `.lyric` v2 format. It ensures stable output for version control by enforcing strict ordering and whitespace normalization.
- [spec.md](spec.md): The formal specification for the `.lyric` v2 format, defining the grammar, whitespace rules, and deterministic writer requirements.
- [parser_test.go](parser_test.go): Comprehensive unit tests for the parser, including tests for various block types, heredocs, and error conditions.
- [writer_test.go](writer_test.go): Unit tests for the writer, focusing on determinism and the round-trip property.

## Architecture and Data Flow

The `cdd` module acts as a translation layer between the `pkg/extract` data model and the filesystem. 

1. **Ingestion**: The `Parse` function takes raw text (typically read from a `.lyric` file) and a filename (for error reporting). It produces a `PackageInfo` populated with structs, interfaces, functions, and rich-doc metadata.
2. **Persistence**: The `Write` function takes a `PackageInfo` and produces a canonical string representation.

The module is entirely stateless; all state is contained within the `parser` and `writer` internal structs during the duration of a single call. It relies heavily on the `PackageInfo` structure defined in [pkg/extract](../extract/design.md), which serves as the "narrow waist" of the Lyre system.

### The Round-trip Property

A core design requirement is that `Parse(Write(p))` must be structurally equal to `p` for any well-formed `PackageInfo`. This property ensures that the toolchain can safely update `.lyric` files (e.g., via `lyre update`) without losing human-authored documentation or introducing unintended changes. To achieve this, the writer uses deterministic ordering and the parser strictly follows the grammar defined in [spec.md](spec.md).

### Signature Opaque-ness

A critical architectural decision is treating signatures (field types, method/function signatures) as opaque strings. The `cdd` module does not attempt to understand the syntax of the target language. Instead, it captures the "rest-of-line" text after a keyword like `field`, `method`, or `func`. This allows the format to support any programming language without modification to the parser, while the writer ensures these signatures are flattened to a single line to maintain the line-oriented grammar.

## Interface Implementations

This module does not implement any external interfaces. It provides a functional API used by the `lyre` command-line tool and other high-level components to manage `.lyric` files.

## Public API

The public API is intentionally minimal, consisting of two primary functions:

- `Parse(text, filename string) (*extract.PackageInfo, error)`: Parses the provided text as a `.lyric` v2 file. It returns a `PackageInfo` or a detailed error including line and column information. It handles CRLF normalization and ignores comments and blank lines outside of heredocs.
- `Write(p *extract.PackageInfo) string`: Serializes the `PackageInfo` into a deterministic string. It handles signature flattening to ensure that multi-line signatures from native extractors do not break the line-oriented grammar of the `.lyric` format.

## Implementation Details

### Parser Logic

The parser in [parser.go](parser.go) uses a line-oriented, recursive-descent approach. It classifies each line by its indentation level (exactly 2 spaces per level) and identifies "structural" lines (non-blank, non-comment). 

- **Scoping**: Indentation defines the hierarchy. A `module` block is at indent 0, its children at indent 1, and so on. The parser uses `peekStructural` and `consumeStructural` to navigate the line stream while respecting these boundaries.
- **Heredocs**: Multi-line prose in `doc` and `invariant` blocks is captured using `"""` heredocs. The parser strips exactly the amount of indentation present on the opening `"""` line from every content line, allowing for natural-looking nested documentation.
- **Quoted Strings**: The parser uses a minimal escaping scheme, recognizing only `\"` and `\\` as escapes, as defined in the spec.
- **Decl Meta**: For each declaration (class, struct, interface, etc.), the parser looks for optional `source:` and `why:` lines in its body.
- **Invariant Metadata**: Invariants can include `verified-by:` (a comma-separated list of test names) and a `procedural` marker, which are parsed before the heredoc body.
- **Enum Support**: The `enum` keyword is currently treated as a `struct` in the parser, with variants represented as `field` lines. This is a placeholder for future first-class enum support.

### Writer Logic

The writer in [writer.go](writer.go) is designed for strict determinism. This is critical because `.lyric` files are checked into version control, and non-deterministic output would create noisy diffs.

- **Ordering**: The writer sorts declarations to ensure stable output. If all declarations have `source:` information (file and line number), they are sorted by position. Otherwise, they are sorted alphabetically by name.
- **Signature Flattening**: To maintain the line-oriented nature of the format, the writer collapses all internal whitespace and newlines in native signatures into single spaces using the `flattenSig` helper.
- **Heredoc Emission**: The writer ensures that heredoc bodies are emitted with the same indentation as the opening `"""` marker, ensuring the parser can correctly strip it.
- **Whitespace**: The writer enforces exactly 2 spaces per indent level, no trailing whitespace on lines, and exactly one trailing newline at the end of the file.
- **Metadata Handling**: `source:` and `why:` lines are emitted in a consistent order within each block. At the module level, `source:` is emitted as a JSON list.

### State Management and Concurrency

The `cdd` module is entirely stateless. The `parser` and `writer` structs are internal and are instantiated fresh for each `Parse` or `Write` call. 

- **Concurrency**: The `Parse` and `Write` functions are thread-safe as they do not share any mutable state. However, the `extract.PackageInfo` structure itself is not thread-safe, so callers must ensure that a single `PackageInfo` is not being modified while it is being written.
- **Error Handling**: The parser returns detailed errors that include the filename and line number. It uses an `errf` helper to format these messages consistently. While it currently stops at the first fatal error, the design allows for future expansion to multi-error reporting.

## Dependencies

- [pkg/extract](../extract/design.md): The `cdd` module is tightly coupled to the `PackageInfo` data model. It uses the types and helper functions defined there to represent the extracted API and its associated documentation.

## Technical Debt and Future Work

- **Error Recovery**: While the parser reports errors with line numbers, it currently stops at the first fatal error. Improving error recovery to report multiple issues in a single pass would improve the developer experience.
- **Enum Support**: As noted in the spec, `enum` is currently a reserved keyword that maps to `TypeDefInfo`. Future work will involve adding first-class support for enums in both the data model and the parser/writer.
- **Performance**: For extremely large modules, the line-by-line string splitting and processing could be optimized by using a `bufio.Scanner` or a similar streaming approach, though current performance is more than adequate for typical module sizes.
