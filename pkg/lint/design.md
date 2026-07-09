# Lint Module Design

## Executive Summary

The `lint` module provides the quality assurance engine for the Lyre toolchain, implementing the `lyre lint` command. While other components like the verifier focus on structural synchronization (drift detection) between source code and documentation, the linter is dedicated to "quality hygiene." It operates on the language-agnostic `PackageInfo` model to identify missing architectural explanations, undocumented invariants, unfilled placeholders, and other recoverable quality issues that might otherwise degrade the value of the documentation over time.

Designed to be fast, deterministic, and easily extensible, the linter applies a set of heuristic rules that reflect the project's philosophy: documentation should be proportional to the complexity of the code. By inspecting the semantic structure of a package—such as the number of methods in a class or the presence of enum-typed fields—the module can intelligently suggest where a developer's explanatory effort is most critically needed.

## File Inventory

- [lint.go](lint.go): The core implementation of the linting engine, containing the public `Lint` function, the `Finding` and `Result` data structures, and the individual logic for all lint rules (W001–W008).
- [lint_test.go](lint_test.go): A comprehensive suite of unit tests that verify each lint rule against both positive (clean) and negative (triggering) scenarios, ensuring the linter's accuracy and determinism.

## Architecture and Data Flow

The `lint` module is designed as a functional transformation pipeline that consumes a high-level semantic model and produces a set of quality findings. The process begins when the `Lint` function is called with a pointer to an `extract.PackageInfo` structure, which has typically been produced by the [pkg/cdd](../cdd/design.md) parser or a language-specific extractor.

The data flow is strictly one-way and stateless. The `Lint` function initializes a `Result` container and then sequentially executes a series of internal check functions, labeled `w001` through `w008`. Each check function traverses the relevant parts of the `PackageInfo` tree—such as the module-level prose, the list of structs, or the collection of invariants—and appends any discovered issues to the `Result`. 

A key architectural feature is the use of structural heuristics. The linter does not treat all code equally; instead, it uses the complexity of the code (e.g., method counts) to determine the expected level of documentation. Once all checks are complete, the findings are sorted to ensure that the output is perfectly deterministic, which is critical for use in continuous integration environments where stable reports are required.

## Interface Implementations

The `lint` module is a standalone service and does not currently implement any external interfaces defined in other packages. It provides a specialized API that is consumed directly by the Lyre command-line interface.

## Public API

The public API of the `lint` module is centered around a single entry point and a set of simple data structures that represent the results of the analysis.

The primary function is `Lint`, which takes a `*extract.PackageInfo`, a string representing the path to the `.lyric` file (used for reporting purposes), and an `Opts` configuration struct. This function is thread-safe and does not modify the input package information, making it suitable for use in concurrent environments.

The `Finding` struct represents a single quality issue. It contains a unique warning code (e.g., "W001"), a `Severity` level (either `SevWarning` or `SevInfo`), the file path, a "Where" string that provides a human-readable scope hint (such as "class Worker" or "invariant 'Safety'"), and a descriptive message. The `Result` struct acts as a container for these findings and provides a `WarningCount` method, which allows callers to easily determine if the number of warnings exceeds a threshold for failing a build.

The `Opts` struct allows for optional configuration of the linting process. Currently, its primary field is `KnownTests`, a map of test function names. When this map is provided, the linter can perform cross-reference validation, ensuring that invariants marked as "verified-by" actually correspond to existing tests in the project.

## Implementation Details

The core logic of the module is encapsulated in eight distinct lint rules, each targeting a specific aspect of documentation quality. These rules are implemented as private functions that are called in sequence by the main `Lint` entry point.

### Heuristic Rules (W001–W005)

The first five rules focus on the presence and depth of documentation based on the structural complexity of the code. **W001** ensures that every module has a high-level purpose statement in its `why:` block. **W002** requires a dedicated "Architecture" documentation block to explain the module's internal structure.

Rules **W003**, **W004**, and **W005** use quantitative thresholds to identify "heavy" components that require more rigorous documentation. For instance, **W003** triggers if a module contains a class or interface with three or more methods but lacks any invariant blocks, under the philosophy that substantial state machines must document their invariants. **W004** requires at least one method-level `why:` statement for any class or struct with four or more methods. **W005** targets structs with three or more fields that include at least one enum-typed field (detected by matching the field's signature against the package's type definitions), requiring at least one field-level documentation comment to provide semantic context.

### Invariant and Test Validation (W006–W007)

The linter also enforces the integrity of invariant documentation. **W006** ensures that every invariant is either linked to a test via a `verified-by:` clause or explicitly marked as `procedural`. This prevents the documentation from containing "hypothetical" invariants that are not actually enforced or verified. **W007** provides deeper validation by checking that every test name listed in a `verified-by:` block actually exists in the project, provided that the `KnownTests` map is supplied in the options.

### Placeholder Hygiene (W008)

The final rule, **W008**, performs a comprehensive search for the string "TODO" across all prose fields in the package, including module-level explanations, doc blocks, invariants, and declaration-level "why" and "doc" fields. This is a case-sensitive substring match designed to be strict and obvious, ensuring that no temporary placeholders are left in the published documentation.

### Determinism and Sorting

To provide a stable and predictable user experience, the `lint` module ensures that its output is deterministic. After all check functions have run, the findings are sorted using a stable sort algorithm. The primary sort key is the warning code, and the secondary key is the "Where" string. This ensures that the same input always produces the same sequence of findings, which is essential for diffing reports and for use in automated toolchains.

## Dependencies

- **[pkg/extract](../extract/design.md)**: The `lint` module is deeply integrated with the `extract` package. It relies on the `PackageInfo` data model and its constituent types (such as `StructInfo`, `FuncInfo`, and `Invariant`) to perform its analysis. It uses the semantic information captured by the extractors to apply its heuristic rules.

## Technical Debt and Future Work

The current implementation of the linter is robust but has several areas for potential enhancement. The thresholds for "heavy" components (e.g., 3 or 4 methods) are currently hardcoded and based on early project experience; these may eventually need to be made configurable or tuned as the system is applied to a wider variety of codebases.

The "TODO" check in **W008** is currently a simple substring match. While effective, it could be made more sophisticated to avoid false positives if "TODO" appears in a context where it is actually intended to remain. Additionally, the `Finding` structure could be expanded to include "Quick Fix" suggestions or automated refactoring hints, allowing the Lyre CLI to help developers resolve linting issues more efficiently. Finally, incorporating more advanced code complexity metrics into the heuristics could allow the linter to provide even more targeted advice on where documentation is most critical.
