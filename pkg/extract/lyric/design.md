# Lyric Extractor Module Design

## Executive Summary

The `lyric` module is a language-specific implementation of the Lyre extraction system for the Lyric programming language. It serves as the bridge between Lyric source files (`.ly`) and the Lyre Rich-Documentation (v2) format (`.ly.lyric`). 

Unlike the Go extractor which uses in-process AST parsing, the Lyric extractor follows a "bridge" pattern: it locates and executes an external `extract_api` binary (part of the Lyric compiler toolchain) to perform the heavy lifting of parsing and type analysis. The module then translates the resulting JSON into the system-wide `PackageInfo` model.

This module is responsible for the full lifecycle of Lyric documentation:
1.  **Extraction**: Discovering exported structs, classes, interfaces, functions, and type definitions.
2.  **Scaffolding**: Generating initial `.ly.lyric` files.
3.  **Maintenance**: Updating existing documentation while preserving human-written prose.
4.  **Verification**: Detecting drift between the source code and the documented API.

## File Inventory

- [lyric.go](lyric.go): The primary implementation containing the extraction logic, binary bridging, and the `Generate`/`Update`/`Verify` entry points.
- [lyric_test.go](lyric_test.go): Comprehensive test suite covering round-trip extraction, update preservation, and verification of drift. It includes a `requireExtractor` helper to skip tests if the Lyric toolchain is missing.

## Architecture and Data Flow

The `lyric` module operates as a translation layer between the Lyric compiler's internal representation and the Lyre system's language-agnostic model. The data flow for a typical extraction is as follows:

1.  **Discovery**: `ExtractLy` scans the target directory for non-test `.ly` files, specifically excluding files starting with `test_` or ending with `_test.ly`.
2.  **External Analysis**: The module locates the `extract_api` binary (searching `LYRIC_HOME`, the system `PATH`, or standard development locations) and executes it against the discovered source files.
3.  **JSON Parsing**: The binary emits a JSON representation of the Lyric API. This JSON is unmarshaled into internal `lyPackageJSON` structures, which mirror the schema of the `extract_api` output.
4.  **Normalization**: The raw JSON data is converted into the `extract.PackageInfo` format. During this phase, signatures are normalized into a canonical Lyric form. For example, methods always include `self` as the first parameter to match Lyric source conventions.
5.  **Integration**: The resulting `PackageInfo` is then used by the `lyre` CLI or other modules (like `pkg/cdd`) to generate or update `.lyric` files.

## Interface Implementations

While this module does not implement a formal Go interface (as the `extract` package uses a functional entry-point pattern), it provides the standard suite of operations required for a Lyre language provider:

- `ExtractLy`: Implements the core extraction logic by bridging to the external binary.
- `GenerateLy`: Implements the scaffolding logic, returning the path and content for a new `.lyric` file.
- `UpdateLy`: Implements the preservation-aware update logic, merging fresh source data with existing documentation.
- `VerifyLy`: Implements the drift detection logic, comparing the documented state against the actual source.

These functions are consumed by the `lyre` command-line tool to provide a consistent experience across all supported languages.

## Public API

The module exports several high-level functions and types:

- **`ExtractLy(srcDir string) (*extract.PackageInfo, error)`**: The "front door" for extraction. It returns a complete model of the Lyric API in the given directory.
- **`GenerateLy(srcDir string) (outPath, content string, err error)`**: Creates the content for a new `.ly.lyric` file based on the current source.
- **`UpdateLy(lyricPath string) (added []string, err error)`**: Reads an existing `.ly.lyric` file, re-extracts the source, and merges the two, preserving all `why`, `doc`, and `invariant` fields. It returns a list of newly discovered exports.
- **`VerifyLy(lyricPath string) (*VerifyResult, error)`**: Compares the documented API in a `.ly.lyric` file against the actual source code and returns a list of findings (errors, warnings, or info).
- **`VerifyResult` and `Finding`**: Types used to report drift and inconsistencies during verification. `VerifyResult` includes an `ErrorCount()` method for easy status checking.

## Implementation Details

### Binary Bridging
The module relies on the `extract_api` binary. The `findExtractBinary` function uses a specific search order to locate this tool:
1.  The `tools` subdirectory of the path specified in the `LYRIC_HOME` environment variable.
2.  The `tools` subdirectory relative to the `lyric` binary found on the system `PATH`.
3.  The `~/projects/lyric/tools/` directory in the user's home.

If the binary is missing, the module returns descriptive errors. The `runExtract` function handles the execution and JSON unmarshaling.

### Signature Normalization
Lyric signatures are captured as opaque strings but are normalized to ensure consistency. 
- **Methods**: Always include `self` as the first parameter, matching Lyric source conventions.
- **Functions**: Formatted as `Name(params) -> ReturnType`. Multiple return values are parenthesized: `Name(params) -> (R1, R2)`.
- **Whitespace**: The `normalizeSig` function collapses multiple spaces and trims whitespace to ensure that minor formatting differences in source code don't trigger verification errors.

### Merging Logic
The `mergeFreshIntoExisting` function is the heart of the `UpdateLy` process. It performs a deep merge:
- It updates file paths and line numbers for all declarations.
- It refreshes signatures for existing functions and methods.
- It adds new structs, classes, interfaces, and functions.
- **Crucially**, it preserves all human-authored prose (`Why` fields, `Doc` fields on struct fields, etc.) by mapping them from the existing model to the fresh one. It uses sorted keys to ensure deterministic behavior.

### Verification Logic
`VerifyLy` performs a structural comparison between the "Declared" state (from the `.lyric` file) and the "Actual" state (from the source code). It checks for:
- Missing declarations (declared in `.lyric` but gone from source).
- Undocumented exports (present in source but missing from `.lyric`).
- Signature mismatches for functions and methods.
- Type mismatches for fields and type definitions.
- Field completeness (warning if source has extra fields not in `.lyric`).

## Dependencies

- **[pkg/extract](../design.md)**: Provides the core `PackageInfo` data structures and shared utilities.
- **[pkg/cdd](../../cdd/design.md)**: Used for parsing and writing the `.lyric` DSL format.
- **Lyric Toolchain**: Requires the `extract_api` binary to be present on the system.

## Technical Debt and Future Work

- **Self Mutability**: The current `extract_api` JSON output does not capture the mutability of the `self` receiver (e.g., `mut self`). While the extractor preserves the `self` parameter, it cannot currently distinguish between mutable and immutable receivers in the generated signatures.
- **Error Reporting**: Verification findings currently use string-based messages. Future versions could benefit from more structured error codes to allow for better programmatic handling of drift.
- **Binary Dependency**: The hard dependency on an external binary makes the module more difficult to test in environments where the Lyric compiler is not installed. The test suite currently skips tests if the binary is missing or incompatible with the host architecture.
