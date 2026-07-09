# Python Extractor Design

## Executive Summary

The `python` module provides the implementation for extracting, generating, updating, and verifying Lyre rich-documentation (`.py.lyric`) for Python source code. It follows the Lyre v2 architectural principle where the `.lyric` file is the single source of truth for human-authored documentation, while the source code remains the absolute source of truth for signatures, types, and exported structure.

The module bridges the Go-based Lyre system with the Python ecosystem by using a small, embedded Python script that leverages the standard `ast` module to perform precise static analysis. This approach ensures that the extractor is lightweight, requires no third-party Python dependencies, and remains faithful to Python's evolving syntax. It handles the full lifecycle of Python documentation, including discovery of exports, scaffolding initial files, updating existing documentation while preserving prose, and detecting drift between the source code and the documented API.

## File Inventory

- [python.go](python.go): The primary Go entry point. It manages the extraction lifecycle, invokes the Python-side script, merges results into the shared data model, and implements the `GeneratePy`, `UpdatePy`, and `VerifyPy` operations.
- [extract_api.py](extract_api.py): A Python 3.8+ script (embedded in `python.go`) that parses a single Python source file using the `ast` module and emits a JSON representation of its public API.
- [python_test.go](python_test.go): A comprehensive test suite that verifies the extraction logic, round-trip consistency, and drift detection across various Python constructs, including classes, protocols, and type aliases.

## Architecture and Data Flow

The `python` module operates as a bridge between the Go environment and the Python interpreter. The data flow follows a structured pipeline designed for high fidelity and minimal system dependencies:

1.  **Discovery**: The Go code scans the target directory for public `.py` files. It excludes files that follow private conventions (e.g., `_*.py`, `__init__.py`, `__main__.py`) or test patterns (e.g., `test_*.py`, `*_test.py`).
2.  **Execution**: For each discovered file, the Go code writes the embedded `extract_api.py` to a temporary file and executes it using the system's `python3` interpreter. The script accepts one file per invocation.
3.  **Analysis**: The Python script parses the source code into an Abstract Syntax Tree (AST). It identifies exported classes, protocols, functions, and type aliases. It honors the `__all__` variable if present, treating it as the definitive list of public exports.
4.  **Serialization**: The script serializes the extracted metadata—including verbatim signatures, field types, and docstrings—into a JSON blob.
5.  **Integration**: The Go code parses the JSON and merges the results into a `*extract.PackageInfo` structure. It performs final adjustments, such as re-adding the `self` parameter to method signatures to maintain consistency with the Lyre specification and other language extractors.
6.  **Persistence/Verification**: The resulting `PackageInfo` is then used to either generate a new `.py.lyric` file, update an existing one while preserving human-written prose, or verify that an existing `.py.lyric` file accurately reflects the current state of the source code.

## Interface Implementations

This module implements the Python-specific logic for the extraction framework defined in [pkg/extract](../design.md). While it doesn't implement a formal Go interface (as the extractors are invoked via functional dispatch in the CLI), it adheres to the standard suite of operations required by the Lyre system:

- **Extraction**: `ExtractPy(srcDir string) (*extract.PackageInfo, error)`
- **Generation**: `GeneratePy(srcDir string) (outPath, content string, err error)`
- **Update**: `UpdatePy(lyricPath string) (added []string, err error)`
- **Verification**: `VerifyPy(lyricPath string) (*VerifyResult, error)`

It heavily utilizes the `PackageInfo` and related structures from [pkg/extract](../design.md) and the parsing/writing capabilities of [pkg/cdd](../../cdd/design.md).

## Public API

The `python` package exports several high-level functions and types that drive the Lyre workflow for Python projects.

### Core Functions

- **ExtractPy**: Scans a directory and returns a `PackageInfo` representing the public API. It handles the complexity of shelling out to Python, merging multi-file results, and seeding initial "Why" documentation from source docstrings.
- **GeneratePy**: A convenience wrapper around `ExtractPy` that prepares the content for a new `.py.lyric` file. It returns the suggested output path and the serialized content.
- **UpdatePy**: Performs a "smart merge" between the current source code and an existing `.py.lyric` file. It refreshes signatures and positions while carefully preserving human-curated documentation like `ModuleWhy`, `Invariants`, and hand-written `Doc` blocks.
- **VerifyPy**: Compares a `.py.lyric` file against the actual source code and returns a `VerifyResult` containing any discrepancies (drift), such as missing exports or signature mismatches.

### Verification Types

- **VerifyResult**: A container for findings discovered during verification. It provides an `ErrorCount()` method to quickly determine if the verification passed.
- **Finding**: Represents a single discrepancy, including its severity (`SevError`, `SevWarning`, `SevInfo`), the affected file, and a descriptive message.
- **Severity**: An enumeration of finding levels: `SevError` (critical drift), `SevWarning` (minor discrepancies like extra fields in source), and `SevInfo` (informational).

## Implementation Details

### The Python Bridge (`extract_api.py`)

The heart of the extraction logic resides in `extract_api.py`. It uses the `ast` module to walk the code structure without executing it. Key features include:
- **Verbatim Annotations**: The `unparse_annotation` function reconstructs type annotations from the AST into strings (e.g., `List[int]`, `Optional[str]`, or `X | Y`) to be stored in the `SignatureText` fields. It includes compatibility shims for Python 3.8 (e.g., handling `ast.Index`).
- **Protocol Detection**: Classes that inherit from `typing.Protocol` or `Protocol` are automatically classified as `InterfaceInfo` rather than `StructInfo`.
- **Signature Reconstruction**: It builds function and method signatures in a canonical format: `Name(params) -> ReturnType`. It handles positional-only, keyword-only, varargs (`*args`), and kwargs (`**kwargs`).
- **Docstring Extraction**: The first-line docstring of a class or function is extracted and used as the initial `Why` metadata.
- **__all__ Handling**: If a module defines `__all__`, the script strictly limits its extraction to the names listed in that variable.

### Signature Normalization

To prevent spurious drift reports caused by whitespace differences or multi-line signatures in the source code, the module implements a `normalizeSig` function. This function collapses all whitespace (including newlines and tabs) into single spaces before comparing signatures during verification. This ensures that the `.py.lyric` file's flattened signatures match the source code's potentially formatted ones.

### Handling `self` and `cls`

Python methods explicitly include `self` or `cls` in their parameter list. The `extract_api.py` script strips these for internal processing to simplify its logic. However, the Go code in `python.go` re-adds `self` to the `SignatureText` of all methods. This ensures that the `.py.lyric` files remain consistent with other languages in the Lyre system (like Lyric itself) while still being valid Python-like signatures.

### Smart Merging and "Source is Truth"

During an update, the module follows a "source is truth" policy for signatures and positions. For documentation, it uses `extract.PreferFresh` for the `Why` fields: if the source code contains a docstring, it overwrites any existing `Why` prose in the `.lyric` file. However, for `ModuleWhy`, `Invariants`, and `Doc` fields (which have no direct equivalent in standard Python docstrings), the human-authored prose in the `.lyric` file is strictly preserved.

## Dependencies

- **[pkg/extract](../design.md)**: Provides the shared data model (`PackageInfo`, `StructInfo`, etc.) and utility functions like `PreferFresh`.
- **[pkg/cdd](../../cdd/design.md)**: Provides the parser and writer for the `.lyric` v2 DSL.
- **Python 3.8+**: The host system must have `python3` installed and available in the `PATH`. The extractor uses only the Python standard library, ensuring it works in minimal environments.

## Technical Debt and Future Work

- **Python Versioning**: The `ast` module's behavior can vary between Python versions. While the current implementation includes compatibility shims for 3.8 and uses `ast.unparse` where available (3.9+), future updates should continue to monitor changes in Python's grammar.
- **Complex Type Aliases**: Currently, only simple type aliases (e.g., `MyType = int`) are captured. More complex assignments or those involving generics might be missed or incorrectly represented.
- **Performance**: For very large Python projects, the overhead of spawning a new Python process for every file can be significant. If performance becomes a bottleneck, a persistent worker process or a batch-mode for the Python script could be implemented.
- **Error Reporting**: Errors from the Python script (like `SyntaxError`) are captured and reported, but they could be made more granular to help users debug issues in their Python source code directly from the Lyre CLI.
