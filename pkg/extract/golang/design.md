# Golang Extractor Module

## Executive Summary

The `golang` module is a specialized component of the Lyre toolchain designed to bridge the gap between native Go source code and the language-agnostic `PackageInfo` model. Its primary mission is to extract exported declarations—structs, interfaces, functions, and type definitions—from Go packages and manage the lifecycle of their corresponding `.go.lyric` documentation files. 

The module operates on a "source of truth" philosophy where the Go source code remains the definitive authority for API signatures and initial documentation. It leverages the Go standard library's robust parsing tools to generate high-fidelity representations of the code, which are then transformed into the unified model used by the rest of the Lyre system. By treating signatures as verbatim, opaque strings, the module ensures that the documentation remains faithful to Go's syntax while enabling sophisticated cross-language documentation workflows.

## File Inventory

*   [golang.go](golang.go): Implements the core extraction logic, providing functions to parse directories, specific files, or raw source strings into the `PackageInfo` model. It handles the traversal of Go AST nodes and the conversion of complex Go types into canonical string representations.
*   [ldd.go](ldd.go): Defines the high-level pipeline entry points for the `.go.lyric` v2 format, including extraction, generation, updating, and verification. It contains the critical merge logic that preserves human-authored prose while refreshing signatures from source.
*   [ldd_test.go](ldd_test.go): Contains the comprehensive test suite for the module, ensuring the reliability of the extraction and synchronization processes across various Go language constructs.

## Architecture and Data Flow

The architecture of the `golang` module is centered around a functional transformation pipeline that maps Go's native Abstract Syntax Trees (AST) to the `PackageInfo` structure defined in [pkg/extract](../design.md). The data flow begins with the discovery of source files, where the module identifies all non-test `.go` files within a target directory. If a directory contains only test files, the module gracefully falls back to analyzing those files to ensure that even test-heavy packages can be documented.

Once the files are identified, the module uses the `go/parser` package to generate ASTs, preserving comments for documentation seeding. The extraction process then traverses these trees, filtering for exported declarations. For each declaration, the module captures its name, its verbatim signature, and its precise location in the source code (file and line number). This location data is critical for the "source of truth" policy, allowing the toolchain to link documentation back to the exact line of code it describes.

The extracted data is then enriched with documentation. The module scrapes Go doc comments and uses the `extract.SeedWhyFromDoc` utility to collapse multi-line comments into a single-line `why:` summary. This summary serves as the initial documentation in the `.lyric` file, ensuring that developers' existing efforts in the source code are not lost. Finally, the populated `PackageInfo` is either returned for in-memory processing or passed to the [pkg/cdd](../../cdd/design.md) module for serialization to disk.

## Interface Implementations

While the `golang` module does not implement a formal Go interface, it adheres to the implicit "Extractor" contract required by the Lyre CLI. It provides a standardized set of entry points—`ExtractGo`, `GenerateGo`, `UpdateGo`, and `VerifyGo`—that allow the CLI to treat Go packages identically to those written in TypeScript, Python, or Lyric. The module's primary output is the `PackageInfo` structure, which serves as the universal exchange format for the entire Lyre ecosystem.

## Public API

The `golang` module exposes a clean, functional API designed for both CLI integration and programmatic use. The primary "front door" for most operations is `ExtractGo`, which scans a directory and returns a fully populated `PackageInfo` representing the exported API. For scaffolding new documentation, `GenerateGo` provides the content for a fresh `.go.lyric` file, while `UpdateGo` handles the complex task of synchronizing an existing documentation file with changes in the source code.

The module also provides several utility functions for fine-grained control over the extraction process. `ExtractFiles` and `ExtractSource` allow for parsing specific files or raw strings, which is particularly useful for testing and for processing the "understanding" files used by the Lyre system. Supporting these are low-level tools like `TypeString`, which converts Go AST type expressions into their string representations, and `BuildSignature`, which constructs full Go function signatures. These utilities ensure that the module can handle the full breadth of Go's type system, including generics, maps, and channels.

For quality assurance, the `VerifyGo` function provides a robust mechanism for detecting drift between the source code and the documentation. It re-extracts the API from the source and performs a deep comparison against the content of a `.lyric` file, reporting any mismatches in signatures, missing declarations, or undocumented exports as a set of structured findings.

## Implementation Details

### Signature and Type Normalization

A critical challenge in cross-language documentation is ensuring that minor formatting differences in the source code do not trigger false positives during verification. The `golang` module addresses this through sophisticated normalization logic. The `sigMatch` function collapses all internal whitespace and strips leading/trailing spaces before comparing signatures. This ensures that a multi-line function signature in the source code is considered identical to its flattened representation in the `.lyric` file.

Type comparison is handled by `typesMatch`, which accounts for Go-specific equivalencies. It treats `any` and `interface{}` as identical and provides a `stripPackagePrefix` utility to allow for flexible documentation where package qualifiers (e.g., `context.Context` vs `Context`) might vary. This normalization is essential for maintaining a stable documentation set that is resilient to routine code refactoring and formatting changes.

### The Merge Algorithm

The `mergeFreshIntoExisting` function is the heart of the module's synchronization logic. It performs a non-destructive merge that prioritizes human-authored prose while ensuring that technical details remain accurate. The algorithm follows a strict hierarchy:
- **Signatures and Positions**: These are always updated from the fresh extraction to reflect the current state of the code.
- **Per-Declaration Prose**: The Go source comment is considered the primary source of truth. If a declaration has a doc comment in the source, it overwrites the `why:` prose in the `.lyric` file. If no source comment is found, the existing prose in the `.lyric` file is preserved.
- **Module-Level Metadata**: Sections such as `ModuleWhy`, named `Docs` blocks, and `Invariants` are treated as purely human-curated design documentation and are always preserved during updates.
- **Field Documentation**: The merge logic carefully preserves per-field documentation, falling back to the existing `.lyric` content only when the fresh source extraction provides no new information.

### Handling Complex Go Constructs

The module provides deep support for Go's unique language features. For example, the `receiverTypeName` function correctly identifies the target struct for methods, even when using pointer receivers or generic type parameters. The `TypeString` function handles the recursive nature of Go types, accurately representing nested structs, interfaces, and complex function signatures. This level of detail ensures that the extracted API is a faithful representation of the developer's intent, providing a solid foundation for the rest of the documentation pipeline.

## Dependencies

The `golang` module is designed to be highly self-contained, relying primarily on the Go standard library and the core Lyre data models.

*   [pkg/extract](../design.md): Provides the foundational `PackageInfo` and `FuncInfo` structures that define the system's data contract.
*   [pkg/cdd](../../cdd/design.md): Used for parsing and writing the `.lyric` DSL format during update and generation operations.
*   **Go Standard Library**: The module makes extensive use of `go/ast`, `go/parser`, and `go/token` for source code analysis, and `path/filepath` for robust cross-platform file handling.

## Technical Debt and Future Work

While the `golang` module is highly capable, several areas are identified for future enhancement:
- **Pruning**: The current `UpdateGo` implementation does not remove declarations from the `.lyric` file if they are deleted from the source code. A planned `--prune` option will address this in future phases.
- **Anonymous Types**: Extremely complex or deeply nested anonymous types are currently simplified to `struct{...}` in some contexts. Future work may involve providing more detailed representations for these types.
- **Integration Test Refinement**: The fallback behavior for test-only directories is effective but may require further refinement to handle complex integration test setups where multiple packages are co-located.
