# Gen Module Design

## Executive Summary

The `gen` module provides post-processing logic for the Lyre system, specifically focusing on the "rich-documentation" upgrade. Its primary responsibility is to take a freshly-extracted `PackageInfo` and seed it with `TODO` placeholders or migrate existing native source comments into the appropriate rich-doc slots. This process ensures that a subsequent run of the Lyre linter will report only unfilled placeholders rather than missing structural documentation components. The module is designed to be idempotent and non-destructive, preserving any human-authored prose that already exists in the package metadata.

By automating the scaffolding of documentation, the `gen` module reduces the friction for developers transitioning to the Lyre documentation system. It bridges the gap between raw source code extraction and a fully documented architectural model, providing a clear roadmap for what needs to be explained.

## File Inventory

- [rich.go](rich.go): Implements the core seeding logic that populates `PackageInfo` with placeholders and migrates native comments.
- [rich_test.go](rich_test.go): Contains unit tests for the seeding logic, verifying idempotency, preservation of existing prose, and the contract with the linter.

## Architecture and Data Flow

The `gen` module operates as a transformation step in the Lyre documentation pipeline. It typically sits between the extraction phase and the final documentation generation or linting phase. The module follows a functional transformation pattern, where it receives a data model, modifies it in place, and returns it for further processing.

The data flow begins with a pointer to an `extract.PackageInfo` struct, which has been populated by a language-specific extractor. The `SeedRichPlaceholders` function then performs a comprehensive traversal of the entire package structure. This includes module-level metadata, structs, interfaces, functions, and type definitions. For each structural element, the module evaluates whether the corresponding rich-doc field, such as the `Why` explanation or the "Architecture" block, is currently empty.

If a field is empty, the module attempts to find a legacy native source comment in the `Doc` field. If such a comment exists, it is cleaned and migrated into the rich-doc slot. If no legacy comment is available, the module generates a standardized `TODO` placeholder. This transformation ensures that the `PackageInfo` struct is mutated to contain all the necessary placeholders to satisfy the baseline requirements of the Lyre linter, effectively turning "missing documentation" errors into "unfilled placeholder" warnings.

## Interface Implementations

This module does not explicitly implement any external interfaces. It provides a functional utility that operates on the shared data model defined in the [pkg/extract](../extract/design.md) module. It acts as a consumer of the `PackageInfo` contract, providing a specific service to the rest of the toolchain.

## Public API

The `gen` package exports a single primary function that serves as the entry point for rich-doc seeding.

### SeedRichPlaceholders

The `SeedRichPlaceholders(p *extract.PackageInfo)` function is the core of the module's public interface. It mutates the provided `PackageInfo` in place, ensuring that every empty rich-doc slot that would otherwise trigger a lint warning is filled with either a `TODO` placeholder or a cleaned-up version of the legacy native-source-comment `Doc` field.

The function handles several specific areas of the package metadata:
- **Module-level Why**: It seeds a placeholder if the `ModuleWhy` field is empty.
- **Architecture Block**: It appends a "TODO: explain the module's high-level structure" block to the `Docs` slice if no "Architecture" block (case-insensitive) already exists.
- **Declarations**: It iterates through all structs, interfaces, functions, and type definitions, seeding their respective `Why` fields.
- **Methods**: It also iterates through the methods of structs and interfaces, ensuring their `Why` fields are populated.
- **Fields**: It cleans existing field-level documentation but intentionally does not manufacture `TODO` placeholders for fields. This is because field-level documentation is considered optional and is only nudged by the linter in specific "heavy" contexts where a human developer should make a conscious choice to add clarity.

## Implementation Details

### Idempotency and Preservation

A core tenet of the `gen` module is that it must never overwrite human-authored prose. Before seeding any field, the module checks if the field already contains non-whitespace content. This makes the `SeedRichPlaceholders` function idempotent; it can be run multiple times on the same `PackageInfo` without creating duplicate placeholders or altering existing documentation. This behavior is critical for the "update" workflow, where Lyre must merge fresh source data with existing documentation without destroying the developer's work.

### Native Comment Migration

When seeding a `Why` field, the module first looks at the legacy `Doc` field, which contains the raw comments extracted from the native source code. It uses the `extract.CleanDocLine` utility to strip comment markers and reduce the comment to its first non-empty line. This allows Lyre to automatically migrate existing documentation into the new rich-doc format, providing a smoother transition for legacy projects. This migration logic is shared with language extractors to ensure consistent behavior across the toolchain.

### The Linter Contract

The seeding logic is specifically tuned to satisfy the requirements of the Lyre linter ([pkg/lint](../lint/design.md)). After running `SeedRichPlaceholders`, a clean package should only report `W008` (unfilled TODO placeholders) and potentially `W003` (missing invariants for heavy classes).

The module intentionally does NOT manufacture invariants to satisfy `W003`. Invariants are considered critical, human-authored records of caught bugs and system guarantees, and auto-generating boilerplate for them would be counterproductive. Similarly, it does not manufacture `TODO`s for fields (satisfying `W005`) because the linter only nudges for field documentation in "heavy" structs where the human developer should make a conscious choice to add clarity.

## Dependencies

The `gen` module has a direct dependency on the `extract` module for its data structures and utility functions.

- **[pkg/extract](../extract/design.md)**: Provides the `PackageInfo` model and the `CleanDocLine` utility.

While it does not import the [pkg/lint](../lint/design.md) module, its logic is tightly coupled with the warning codes and requirements defined in the linter to ensure a consistent user experience.

## Technical Debt and Future Work

- **Placeholder Customization**: Currently, the `TODO` placeholders are hardcoded. Future versions could allow for template-based placeholders to provide more context or project-specific instructions.
- **Language-Specific Seeding**: While the current logic is language-agnostic, some languages might benefit from more specific placeholder text or different seeding strategies for certain constructs.
- **Integration with CLI**: The module is currently called by the `lyre` CLI during the `gen --rich` command. Further integration could allow for more granular control over which parts of the package are seeded, perhaps through command-line flags.
