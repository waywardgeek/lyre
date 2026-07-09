# TypeScript Extract Module Design

## Executive Summary

The `typescript` module provides the implementation for extracting API information from TypeScript source files. It is a specialized component of the `pkg/extract` hierarchy, designed to bridge the gap between TypeScript's rich type system and Lyre's language-agnostic documentation model.

The module follows a "bridge" pattern: a Go wrapper manages the execution of a Node.js script ([extract_api.js](extract_api.js)) that leverages the official TypeScript Compiler API. This ensures high fidelity in parsing and type resolution, including support for modern TypeScript features like JSX (`.tsx`), constructor parameter properties, and complex type aliases. The extracted information is then mapped into the shared `extract.PackageInfo` structure, enabling the generation, update, and verification of `.ts.lyric` files.

A key design goal is "zero-config" operation. The module automatically manages its Node.js environment, including installing the necessary `typescript` compiler package if it is missing from the local `node_modules`.

## File Inventory

- [extract_api.js](extract_api.js): A Node.js script that uses the TypeScript Compiler API to perform the heavy lifting of source analysis. It identifies exported classes, interfaces, functions, type aliases, and enums, and extracts their signatures and JSDoc comments.
- [typescript.go](typescript.go): The Go entry point. It handles file system scanning, manages the Node.js execution environment (including automatic `npm install`), and converts the extractor's JSON output into the Lyre data model.
- [typescript_test.go](typescript_test.go): A comprehensive test suite that verifies the extraction pipeline against a variety of TypeScript constructs and ensures the integrity of the update and verification workflows.
- [package.json](package.json): Defines the Node.js dependencies for the extractor script, primarily the `typescript` compiler.
- [package-lock.json](package-lock.json): Ensures deterministic installation of Node.js dependencies.

## Architecture and Data Flow

The extraction process follows a linear pipeline:

1.  **Discovery**: When `ExtractTs` is called, it scans the target directory for `.ts` and `.tsx` files. It automatically filters out test files (`.test.ts`, `.spec.ts`), declaration files (`.d.ts`), and internal files (prefixed with `_`).
2.  **Environment Setup**: The Go wrapper checks for the existence of `node_modules`. If missing, it executes `npm install` to ensure the TypeScript compiler is available.
3.  **External Analysis**: For each discovered file, the Go wrapper executes `extract_api.js` using `node`. The wrapper sets `NODE_PATH` to the package's local `node_modules` to ensure the script can import the `typescript` module.
4.  **TypeScript Parsing**: Inside `extract_api.js`, the TypeScript Compiler API creates a "program" for the source file. It traverses the Abstract Syntax Tree (AST) to find exported symbols. If no symbols are exported, it treats the file as a script and extracts all top-level public symbols.
5.  **Metadata Extraction**: For each symbol, the script extracts:
    -   **Signatures**: Verbatim text of types, parameters, and return types.
    -   **Documentation**: JSDoc or leading comments, cleaned of markers.
    -   **Location**: File name and line number for traceability.
6.  **JSON Bridge**: The script outputs a structured JSON object to stdout, which is captured and decoded by the Go wrapper into the `tsPackageJSON` struct.
7.  **Model Mapping**: The Go code merges the results from all files into a single `extract.PackageInfo`. During this phase, signatures are normalized into a canonical form: `Name(p1: t1, p2: t2): retType`.
8.  **CDD Integration**: The final `PackageInfo` is passed to [pkg/cdd](../../cdd/design.md) for persistence in the `.ts.lyric` format.

## Interface Implementations

This module implements the standard extraction interface pattern used by the Lyre CLI for language-specific support. While it does not satisfy a formal Go interface (as the CLI performs language-based dispatch), it provides the following expected behaviors:

- **Extraction**: Implements `ExtractTs` to produce a `PackageInfo` from source.
- **Generation**: Implements `GenerateTs` to scaffold new `.lyric` files.
- **Maintenance**: Implements `UpdateTs` to merge source changes into existing `.lyric` files while preserving human-authored prose.
- **Verification**: Implements `VerifyTs` to detect drift between source and documentation.

## Public API

The module exports several high-level functions and types:

- **ExtractTs(srcDir string)**: The primary entry point. It returns a `*extract.PackageInfo` containing the public API of the TypeScript module. It handles the discovery of source files and the execution of the Node.js extractor.
- **GenerateTs(srcDir string)**: Returns the path and content for a fresh `.ts.lyric` file. It does not write to disk, leaving that to the caller.
- **UpdateTs(lyricPath string)**: Refreshes an existing `.ts.lyric` file. It re-extracts signatures from the source, adds new exports, and preserves all human-authored prose. It returns a list of newly discovered exports.
- **VerifyTs(lyricPath string)**: Performs a deep comparison between the `.ts.lyric` file and the current source code, returning a `VerifyResult` containing any discrepancies (errors, warnings, or info messages).
- **VerifyResult / Finding / Severity**: Types used to report verification findings. `VerifyResult` contains a slice of `Finding` objects, each with a `Severity` (Error, Warning, Info), file location, and descriptive message.

## Implementation Details

### Signature Normalization and Matching

To avoid spurious verification failures due to formatting changes, the module employs a robust normalization strategy. The `normalizeSig` function in `typescript.go` trims leading/trailing whitespace and collapses all internal runs of whitespace (including newlines and carriage returns) into a single space. This ensures that a multi-line signature in the source code matches its flattened representation in the `.lyric` file. This normalization is applied to both function/method signatures and type definitions.

### Node.js Environment Management

The module is designed to be "zero-config" for developers. The `runExtractScript` function in `typescript.go` manages the execution of the Node.js script. It uses `runtime.Caller` to locate its own source directory on disk to find the `extract_api.js` script and the local `node_modules`. If the `node_modules` directory is missing (e.g., on a fresh checkout), it automatically runs `npm install` using the `ensureNodeModules` function. It also sets the `NODE_PATH` environment variable to ensure the script can find the `typescript` package.

### Handling TypeScript Idioms

The `extract_api.js` script is specifically tuned for TypeScript-specific patterns using the Compiler API:
- **Constructor Parameter Properties**: Shorthand like `constructor(public name: string)` is correctly identified as a field declaration by inspecting the modifiers on constructor parameters.
- **Interfaces as Types**: Interface properties are surfaced as fields, while method signatures are captured as methods. Property-shape members in interfaces are treated as zero-arity methods returning the property's type in the Go model to maintain consistency.
- **Type Aliases and Enums**: These are mapped to Lyre's `TypeDefs`. Enums are captured with the `enum` keyword as their underlying type.
- **Arrow Functions and Variable Declarations**: Functions declared as variables (e.g., `export const foo = () => ...`) or using arrow function syntax are correctly identified and extracted as functions.
- **JSDoc Extraction**: The script extracts JSDoc or leading comments for each declaration, cleaning them of markers (like `/**`, `*/`, and leading `*`) before passing them to the Go wrapper.

### Merging and Verification Logic

The `mergeFreshIntoExisting` function handles the complex task of updating a `.lyric` file. It refreshes signatures and source positions from the fresh extraction while carefully preserving all human-authored prose (`Why` blocks, `Doc` blocks, and `Invariants`).

Verification is performed by comparing the `PackageInfo` parsed from the `.lyric` file with a freshly extracted `PackageInfo`. The module checks for missing declarations, signature mismatches, and undocumented exports. Findings are categorized by severity, with signature mismatches and missing documented exports treated as errors.

## Dependencies

- **[pkg/extract](../design.md)**: The parent package providing the shared data model (`PackageInfo`, `StructInfo`, etc.).
- **[pkg/cdd](../../cdd/design.md)**: The parser and writer for the `.lyric` DSL.
- **TypeScript Compiler API**: The authoritative parser for TypeScript source, executed via Node.js.
- **Node.js**: The runtime environment for the extraction script.

## Technical Debt and Future Work

- **Performance Optimization**: Currently, a new `node` process is spawned for every source file. For large modules, this overhead can be significant. Future versions could use a persistent worker process or pass all files to a single `node` invocation.
- **Complex Type Truncation**: The TypeScript compiler's `typeToString` method may truncate extremely long or complex types. While `NoTruncation` flags are used, very deep types might still be simplified.
- **JSX/TSX Support**: While `.tsx` is supported, the extractor primarily focuses on the API (props, components) rather than the internal JSX structure.
- **Error Handling in Script**: The Node.js script's error handling could be more robust, providing better feedback when parsing fails.
