# Lyre Project Architecture

## Executive Summary

The Lyre project is a sophisticated, multi-language toolchain designed to enable Context-Driven Development (CDD) and rich documentation. Its primary mission is to solve the persistent disconnect between source code and architectural documentation. By treating the source code as the absolute source of truth for structural elements (like signatures, types, and exports) and a dedicated `.lyric` file as the source of truth for human-authored intent (such as "why" prose, architectural overviews, and system invariants), Lyre ensures that documentation remains accurate, synchronized, and highly valuable. 

At its core, Lyre extracts API information from diverse programming languages—including Go, TypeScript, Python, and the custom Lyric/Forge language—and normalizes this data into a unified, language-agnostic representation. This unified model allows the system to generate, update, verify, and lint documentation across an entire polyglot codebase with a single, consistent workflow. The project is built for high velocity and reliability, employing deterministic serialization, robust drift detection, and automated scaffolding to minimize developer friction while maximizing architectural clarity.

## System Architecture

The architecture of Lyre is fundamentally pipeline-oriented and data-driven, centered around a "narrow waist" design. The system is divided into three primary phases: extraction, transformation, and persistence/verification. 

The philosophy of the architecture is built on the concept of "verbatim signatures" and "opaque payloads." Rather than attempting to construct a universal Abstract Syntax Tree that can perfectly represent the nuances of Go's concurrency, TypeScript's mapped types, and Python's dynamic protocols, Lyre captures the exact text of signatures directly from the source code. This pragmatic approach allows the system to remain language-agnostic while providing high-fidelity documentation. 

The central nervous system of Lyre is the `PackageInfo` data model, defined in the `extract` module. Every language-specific extractor acts as a provider that translates native source code into this shared model. Once the data is in the `PackageInfo` format, the rest of the system—including the Context-Driven Development (CDD) parser/writer, the rich-documentation generator, and the quality linter—can operate entirely independently of the original source language. 

This decoupled design ensures that adding support for a new programming language requires only the implementation of a new extractor, without any modifications to the core parsing, linting, or documentation generation logic. The architecture heavily favors stateless, functional transformations; state is generally confined to the lifecycle of a single command execution, ensuring that operations like updating or verifying documentation are highly predictable and easily testable.

## Interface & Contract Map

While Lyre relies less on deep Go interface hierarchies and more on functional pipelines, its boundaries are strictly defined by several core contracts and data structures that govern how modules interact.

The most critical contract in the system is the `PackageInfo` data structure defined in the `extract` module. This structure serves as the universal exchange format for the entire toolchain. It acts as the "narrow waist" of the architecture. Every language-specific extractor (Go, TypeScript, Python, Lyric) is contracted to produce a fully populated `PackageInfo` from native source code. Conversely, the `cdd` module consumes this structure to write `.lyric` files, and the `lint` and `gen` modules consume it to perform quality checks and scaffolding. This strict data contract ensures that the downstream processors never need to know the origin language of the code they are analyzing.

The second major boundary is the Extractor Functional Contract. Although not defined as a formal Go `interface` due to the CLI's dispatch mechanism, every language provider must implement a standard suite of operations: extraction, generation, updating, and verification. The CLI expects each language module to provide these entry points, ensuring a uniform user experience regardless of the target language. The update operation, in particular, enforces a strict contract of preservation: it must merge fresh source data with existing documentation without ever destroying human-authored prose.

The third contract is the `.lyric` v2 file format itself, managed by the `cdd` module. This format is a persistent, on-disk contract between the Lyre toolchain and the human developer. It guarantees a deterministic, line-oriented grammar that allows developers to write rich documentation in a natural way, while ensuring that the toolchain can perform lossless round-trip parsing and serialization. The determinism of the writer is a strict requirement to prevent noisy diffs in version control.

Finally, there is a symbiotic contract between the `gen` and `lint` modules. The `lint` module defines the rules for what constitutes acceptable documentation quality (e.g., requiring invariants for heavy classes, or module-level explanations). The `gen` module is contracted to understand these rules and seed a freshly extracted `PackageInfo` with the exact placeholders (like `TODO` markers) necessary to satisfy the linter's structural requirements, leaving only the content-generation task to the human developer.

## Module Map

The Lyre project is composed of several highly cohesive modules, categorized by their role in the documentation pipeline.

### Core Data Models

- [pkg/ast](pkg/ast/design.md)
- [pkg/extract](pkg/extract/design.md)

### Language Extractors

- [pkg/extract/golang](pkg/extract/golang/design.md)
- [pkg/extract/lyric](pkg/extract/lyric/design.md)
- [pkg/extract/python](pkg/extract/python/design.md)
- [pkg/extract/typescript](pkg/extract/typescript/design.md)

### Processing & Persistence

- [pkg/cdd](pkg/cdd/design.md)
- [pkg/gen](pkg/gen/design.md)
- [pkg/lint](pkg/lint/design.md)

### Module Details

The **ast** module defines the Abstract Syntax Tree for the Forge language. It provides the foundational data structures that represent parsed source code, encompassing declarations, statements, expressions, and type expressions. These structures are the primary medium of exchange between the compiler's stages. It is a passive data-definition package that does not contain logic for processing code, but instead defines the schema for the program's intermediate representation. It handles both declaration-only and full code files, providing a unified representation. The module uses a "Kind + Data" pattern for variant types to avoid complex interface hierarchies, simplifying traversal and serialization.

The **extract** module is the architectural hub for language-specific source code analysis. Its primary responsibility is to provide the language-agnostic `PackageInfo` representation of a package's exported API. This module defines the core data structures that capture both the structural elements of a package and the rich-doc metadata required for Context-Driven Development. It establishes the "read-only" extractor pattern, where language-specific extractors parse native source files and populate the shared model. Crucially, it enforces the design decision to treat signatures as opaque, verbatim native-language text, ensuring that the documentation remains faithful to the source language's syntax.

The **golang** extractor module is responsible for extracting exported declarations from Go source files. It serves as the bridge between Go's native abstract syntax trees and Lyre's language-agnostic representation. The module implements the "source of truth" policy, using Go source comments to seed documentation while preserving human-curated design prose during updates. It uses the standard library's parsing tools to generate ASTs, builds canonical signature strings, and manages the lifecycle of Go documentation files through generation, updating, and verification processes.

The **lyric** extractor module provides language-specific extraction for the Lyric programming language. Unlike the Go extractor, it follows a bridge pattern, locating and executing an external binary from the Lyric compiler toolchain to perform parsing and type analysis. It translates the resulting JSON into the system-wide data model. The module handles the full lifecycle of Lyric documentation, including discovery of exports, scaffolding initial files, updating existing documentation while preserving prose, and detecting drift between the source code and the documented API. It normalizes signatures to ensure consistency, such as explicitly including the receiver parameter in methods.

The **python** extractor module bridges the Go-based Lyre system with the Python ecosystem. It uses a small, embedded Python script that leverages the standard library to perform precise static analysis, ensuring the extractor is lightweight and requires no third-party dependencies. The Go code manages the execution of this script, parses the resulting JSON, and merges the results into the shared data model. It handles Python-specific constructs like protocols and type annotations, reconstructing them into verbatim strings. The module performs smart merges during updates to refresh signatures while carefully preserving human-curated documentation.

The **typescript** extractor module extracts API information from TypeScript source files. It employs a bridge pattern where a Go wrapper manages the execution of a Node.js script that uses the official TypeScript Compiler API. This ensures high fidelity in parsing and type resolution, supporting modern features like JSX and complex type aliases. The module automatically manages the Node.js environment, including installing necessary dependencies if missing. It extracts signatures, JSDoc comments, and location data, normalizing the signatures to prevent spurious verification failures due to formatting changes.

The **cdd** module is the core persistence layer for the Lyre toolchain. It implements the parser and writer for the `.lyric` v2 format, a human-readable, indent-significant domain-specific language designed to capture architectural intent. The primary goal of this module is to provide a lossless round-trip between the in-memory data model and its on-disk representation. It treats native-language signatures as opaque strings and focuses on rich-doc sections. The module is entirely stateless, relying on deterministic ordering and strict grammar rules to ensure stable output for version control, allowing documentation and code to be kept in tight synchronization.

The **gen** module provides post-processing logic for the system, focusing on the rich-documentation upgrade. Its responsibility is to take a freshly-extracted data model and seed it with placeholders or migrate existing native source comments into the appropriate rich-doc slots. This process ensures that a subsequent run of the linter will report only unfilled placeholders rather than missing structural components. The module is designed to be idempotent and non-destructive, strictly preserving any human-authored prose that already exists. It operates as a transformation step, mutating the data model in place to satisfy the baseline requirements of the quality checker.

The **lint** module implements a language-agnostic quality checker for the documentation files. While the verification process focuses on drift detection, the linter focuses on quality hygiene. It inspects the content of a parsed data model to identify missing architectural explanations, undocumented invariants, unfilled placeholders, and other recoverable quality issues. The linter operates on the high-level semantic model rather than raw text, allowing it to apply sophisticated heuristics, such as requiring more rigorous documentation for heavy classes with many methods. It is designed to be fast, deterministic, and easily extensible with new rules, returning a sorted list of findings to ensure stable output.

## Integration Patterns & Workflows

The modules in the Lyre toolchain collaborate through well-defined, functional pipelines. The system avoids complex, stateful objects in favor of passing the `PackageInfo` data structure through a series of transformations. Below are the primary workflows that demonstrate how these modules integrate.

The most complex and critical workflow is the Update Pipeline, typically triggered by a command like `lyre update`. This workflow is responsible for synchronizing the documentation with the source code without losing human effort. The process begins when the CLI invokes a language-specific extractor, such as the Python or Go module. The extractor analyzes the current source code—either in-process or by bridging to an external script—and constructs a fresh `PackageInfo` representing the exact current state of the API. Simultaneously, the extractor uses the `cdd` module to parse the existing `.lyric` file from disk, creating a second `PackageInfo` that holds the historical, human-authored documentation. The extractor then performs a meticulous deep merge. It takes the updated signatures, file paths, and line numbers from the fresh model, but carefully maps over the `Why` prose, `Doc` blocks, and `Invariants` from the existing model. Finally, this merged, hybrid `PackageInfo` is passed back to the `cdd` module, which deterministically serializes it back to disk, resulting in a clean, updated file with minimal version control noise.

Another fundamental integration is the Generation Pipeline, used to scaffold new documentation. When a developer runs a generation command, the language extractor first builds a raw `PackageInfo` from the source code. Before this is written to disk, the `gen` module intercepts the data structure. The `gen` module traverses the entire package, identifying empty documentation slots. It attempts to migrate any legacy native source comments into these slots; if none exist, it injects standardized `TODO` placeholders. This seeded `PackageInfo` is then handed to the `cdd` module for serialization. This workflow ensures that newly generated files immediately provide a clear template for the developer to fill out, bridging the gap between raw extraction and rich documentation.

The Linting Pipeline demonstrates how the system enforces quality hygiene independently of the source language. This workflow begins with the `cdd` module parsing an existing `.lyric` file into a `PackageInfo`. The source code itself is not analyzed during this phase. The `PackageInfo` is passed to the `lint` module, which runs a suite of heuristic checks. Because the data model is language-agnostic, the linter can apply universal rules, such as determining if a class is "heavy" based on its method count and subsequently requiring it to document its invariants. The linter compiles a deterministic, sorted list of findings, which the CLI then presents to the user. This pipeline highlights the power of the decoupled architecture: the linter can enforce complex documentation standards without knowing whether the underlying code is Go, TypeScript, or Python.

## Dependency Overview

The dependency graph of the Lyre project is strictly layered to prevent circular dependencies and maintain clear boundaries. The architecture resembles a hub-and-spoke model, with the `extract` module serving as the central hub.

At the foundational level, the `ast` module is completely independent, relying only on the Go standard library. It provides the core data structures for the Forge language and does not import any other Lyre packages.

The `extract` module sits at the center of the documentation toolchain. It defines the `PackageInfo` model and has minimal external dependencies. It is the most heavily imported package in the system.

The `cdd` module depends directly on the `extract` module to understand the data structures it must parse and serialize. It does not depend on any language-specific extractors, maintaining its role as a pure persistence layer.

The language-specific extractors (`golang`, `python`, `typescript`, `lyric`) form the next layer. They depend on both the `extract` module (to populate the data model) and the `cdd` module (to manage the reading and writing of the `.lyric` files during update operations). These modules also introduce external system dependencies: the TypeScript extractor requires Node.js and the TypeScript Compiler API, the Python extractor requires a local Python 3 interpreter, and the Lyric extractor requires the external `extract_api` binary from the Lyric toolchain. The Go extractor is self-contained, relying only on the Go standard library's parsing packages.

Finally, the `gen` and `lint` modules sit alongside the extractors as consumers of the `PackageInfo` model. They depend on the `extract` module for data definitions but are entirely decoupled from the `cdd` module and the language extractors. This isolation ensures that the rules for documentation quality and scaffolding remain independent of how the data is extracted or persisted.
