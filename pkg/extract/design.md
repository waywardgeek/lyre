# Extract Module Design

## Executive Summary

The `extract` module serves as the architectural "narrow waist" of the Lyre system. It provides a language-agnostic representation of a package's exported API, enabling a unified workflow for documentation generation, synchronization, and verification across diverse programming languages. By normalizing native source code into a shared `PackageInfo` model, the module decouples the complexities of language-specific parsing from the downstream tasks of persistence, linting, and architectural documentation.

The module's primary mission is to capture both the structural elements of a package—such as structs, interfaces, functions, and type definitions—and the "rich-doc" metadata required for Context-Driven Development (CDD). This metadata includes module-level "why" prose, named documentation blocks, invariants with verification metadata, and per-declaration explanations. A key design principle of the module is the use of verbatim signatures: instead of attempting to model the nuances of every language's type system, extractors capture signatures as opaque, native-language strings. This ensures that the resulting documentation remains faithful to the source language's syntax while providing a consistent framing for human-authored intent.

## File Inventory

- [extract.go](extract.go): The core of the module, defining the `PackageInfo` data structures and shared utility functions used by all language-specific extractors.
- [extract_test.go](extract_test.go): Contains unit tests for the shared utilities and ensures the lossless JSON serialization of the `PackageInfo` model.
- [golang/](golang/design.md): Sub-package implementing the Go-specific extractor, leveraging the standard library's AST tools.
- [lyric/](lyric/design.md): Sub-package implementing the Lyric-specific extractor, bridging to the Lyric compiler's extraction binary.
- [python/](python/design.md): Sub-package implementing the Python-specific extractor, using an embedded Python script for static analysis.
- [typescript/](typescript/design.md): Sub-package implementing the TypeScript-specific extractor, bridging to a Node.js script using the TypeScript Compiler API.

## Architecture and Data Flow

The `extract` module operates as the central hub in a hub-and-spoke architecture. It defines the data contract that all language-specific extractors must satisfy and that all documentation processors consume. The flow of data through the module typically follows a functional pipeline:

1.  **Discovery and Dispatch**: The system identifies the target directory and its primary programming language.
2.  **Native Extraction**: The appropriate language-specific extractor (e.g., Go, Python, TypeScript) is invoked. This extractor parses the native source files—either in-process or via a bridge to an external tool—and identifies all exported declarations.
3.  **Model Population**: The extractor maps these native declarations into the shared `PackageInfo` structure. During this phase, signatures are captured as verbatim strings directly from the source code, and leading doc comments are scraped to serve as initial documentation.
4.  **Normalization and Seeding**: The shared utility functions in `extract.go` are used to normalize the data. For example, `SanitizeModuleName` ensures the module identifier is valid, and `SeedWhyFromDoc` migrates the first line of native source comments into the `Why` fields of the declarations.
5.  **Downstream Consumption**: The fully populated, language-agnostic `PackageInfo` is then passed to other modules. The [pkg/cdd](../cdd/design.md) module might serialize it to a `.lyric` file, the [pkg/lint](../lint/design.md) module might check it for quality issues, or the [pkg/gen](../gen/design.md) module might seed it with additional placeholders.

This decoupled architecture allows the Lyre toolchain to support new programming languages by simply adding a new extractor that populates the `PackageInfo` model, without requiring any changes to the core documentation logic.

## Interface Implementations

The `extract` module does not implement external interfaces; rather, it defines the primary data contract (`PackageInfo`) that the rest of the system depends on. It acts as the "Source of Truth" provider for the entire Lyre pipeline.

## Public API

The `extract` package exports a set of data structures and utility functions that form the foundation of the Lyre extraction system.

### Core Data Structures

The `PackageInfo` struct is the top-level container, holding maps for `Structs`, `Interfaces`, `Functions`, and `TypeDefs`. It also includes fields for module-level documentation: `ModuleWhy` (a high-level summary), `ModuleSource` (the files covered by the module), `Docs` (a list of named `DocBlock` sections), and `Invariants` (a list of `Invariant` blocks with optional verification metadata).

Each declaration type has a corresponding "Info" struct (e.g., `StructInfo`, `FuncInfo`, `InterfaceInfo`, `TypeDefInfo`). These structs capture the declaration's name, its verbatim `SignatureText`, its source location (`File` and `Line`), and rich-doc fields like `Why` (per-declaration prose) and `Source` (a canonical "file:line" reference). `StructInfo` specifically manages an ordered list of `FieldInfo` objects and a map of `FuncInfo` objects for methods. `FuncInfo` also contains `ParamInfo` and return type information, though these are primarily for internal extractor use and are not round-tripped through the `.lyric` format. The `LDDMeta` struct is also provided to hold legacy metadata parsed from structured comments.

### Utility Functions

- **NewPackageInfo(name string)**, **NewStructInfo()**, and **NewInterfaceInfo()**: Constructors that return initialized instances of the core data structures with their internal maps ready for use.
- **DetectLanguage(filename string)**: Analyzes a filename (e.g., `service.go.lyric`) to determine the underlying source language based on its compound extension.
- **SanitizeModuleName(name string)**: Converts a directory name into a valid Lyric identifier, ensuring compatibility with the `.lyric` file format by mapping invalid characters to underscores and handling leading digits.
- **CleanDocLine(doc string)**: Extracts a single, clean line of prose from a multi-line native source comment, stripping comment markers and collapsing whitespace to fit the single-line `why:` slot in `.lyric` files.
- **SeedWhyFromDoc(p *PackageInfo)**: A post-processing function that populates empty `Why` fields in a `PackageInfo` using the first line of the scraped native source comments, ensuring that source code remains the primary source of truth for per-declaration documentation.
- **PreferFresh(existing, fresh string)**: A helper used during merge operations to decide whether to keep existing human-authored prose or use a fresh comment from the source code (favoring the source code if a comment is present).
- **StructInfo Helpers**: A set of methods on `StructInfo` (e.g., `FieldSig`, `HasField`, `SetField`, `SetFieldDoc`, `FieldNames`) that provide a bridge for legacy callers transitioning from map-based field storage to the new ordered slice of `FieldInfo`.

## Implementation Details

### Verbatim Signatures and Opaque Payloads

A cornerstone of the Lyre design is the treatment of signatures as opaque, verbatim strings. The `SignatureText` field in `FuncInfo` and `FieldInfo` stores the exact text found in the source code (e.g., `func (r *Receiver) Method(a int) error`). This approach avoids the "Universal AST" problem, where a tool tries to create a single model that perfectly represents every nuance of every language. By keeping signatures verbatim, Lyre ensures that the documentation is always syntactically correct for the target language and that the extraction process is robust against language updates.

### State Management and Concurrency

The `PackageInfo` and its constituent structures are designed as passive data carriers. They do not manage their own concurrency; instead, they are typically populated in a single-threaded manner by an extractor and then passed through a series of functional transformations. Callers are responsible for any necessary synchronization if they choose to process a `PackageInfo` concurrently.

### The Seeding Heuristic

The `SeedWhyFromDoc` function implements a critical bridge between legacy source comments and the new rich-documentation system. It uses `CleanDocLine` to reduce potentially verbose source comments into a single-line "why" summary. This summary is only used to fill empty slots, ensuring that hand-authored prose in a `.lyric` file is never accidentally overwritten by a less descriptive source comment.

## Dependencies

The `extract` module is designed to be highly portable and has minimal dependencies, relying almost exclusively on the Go standard library (specifically `strings`, `encoding/json`, and `reflect` for tests).

It is a foundational module consumed by:
- [pkg/cdd](../cdd/design.md): For parsing and serializing `.lyric` files.
- [pkg/gen](../gen/design.md): For scaffolding and migrating documentation.
- [pkg/lint](../lint/design.md): For enforcing documentation quality standards.

## Technical Debt and Future Work

- **Signature Normalization**: While signatures are verbatim, minor formatting differences (like extra spaces) can cause spurious drift detection. A lightweight, language-aware normalization step could improve the reliability of the verification process.
- **Cross-Language Consistency**: While the core model is shared, individual extractors may have slight differences in how they handle edge cases (like test-only directories). Harmonizing these behaviors would improve the predictability of the system.
- **Performance of External Bridges**: For very large codebases, the overhead of spawning external processes for Python and TypeScript extraction can be significant. Implementing a persistent worker or daemon model for these extractors could provide a substantial performance boost.
- **Rich-Doc Evolution**: As the system moves toward Phase 2 and 3 of the rich-doc upgrade, the `PackageInfo` model may need to evolve to support more complex relationships between declarations and documentation.
