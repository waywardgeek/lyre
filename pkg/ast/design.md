# AST Module Design

## Executive Summary

The `ast` module defines the Abstract Syntax Tree (AST) for the Forge language. It provides the foundational data structures that represent parsed source code, encompassing declarations, statements, expressions, and type expressions. These structures are the primary medium of exchange between the compiler's stages: the parser produces them, the type checker annotates them, and the code generator consumes them. This module is a core component of the [Lyre Project Architecture](../../project-design.md).

The module is designed to handle both `.lyric` (declaration-only) and `.fg` (full code) files, providing a unified representation for the entire language. It prioritizes a flat, data-oriented structure over deep inheritance hierarchies, using a "Kind + Data" pattern to represent variant types. This approach simplifies serialization and traversal while maintaining the flexibility needed for a multi-stage compiler.

## File Inventory

- [ast.go](ast.go): Defines the top-level `File` structure, source positioning (`Pos`, `Span`), type expressions (`TypeExpr`), and all major declarations including functions, classes, structs, enums, interfaces, relations, and implementation blocks.
- [expr.go](expr.go): Defines the representation of executable code, including various expression types (`Expr`), statements (`Stmt`), and match patterns (`Pattern`).

## Architecture and Data Flow

The `ast` module is a passive data-definition package. It does not contain logic for processing code but instead defines the "schema" for the program's intermediate representation.

The AST is organized hierarchically:
1.  **File**: The root node representing a single source file. It contains a list of `LyricBlock`s and all source comments.
2.  **LyricBlock**: A named container within a file (e.g., `module ast { ... }`) that holds imports, documentation, invariants, and declarations. This structure allows Forge to interleave formal declarations with rich narrative documentation.
3.  **Declarations**: Nodes like `ClassDecl`, `FuncDecl`, and `InterfaceDecl` define the structure and API of the program.
4.  **Statements and Expressions**: Found within function bodies or initializers, these nodes represent the logic of the program.

Data flows through the AST in a linear pipeline:
- **Parsing**: The parser (in `pkg/parser`) reads source text and constructs the AST nodes, populating `Span` information for every element.
- **Type Checking**: The checker (in `pkg/checker`) traverses the AST. It resolves names and types, populating the `ResolvedType` fields in `Expr` and `Pattern` nodes. It also fills in `InferredTypeArgs` for generic calls.
- **Transformation**: Various passes may desugar the AST. For example, interface-defined fields or destructors might be injected into concrete class declarations.
- **Code Generation**: The final stage (in `pkg/codegen`) performs a final traversal of the annotated AST to emit target code (typically Go).

## Interface Implementations

The `ast` module is a provider of data structures and does not implement interfaces from other modules. It is a foundational package upon which almost all other compiler modules depend.

## Public API

The public API consists of a large set of exported structs and enums. The design follows a consistent pattern for variant types (Types, Expressions, Statements, Patterns):

### Core Types
- **Pos and Span**: Represent source locations. `Pos` tracks file, line, and column; `Span` tracks a start and end `Pos`. These are critical for high-quality error reporting throughout the compiler.
- **File**: The entry point, containing `LyricBlock`s and a `DocComment` helper for retrieving comments.

### Declarations
- **FuncDecl**: Represents functions and methods, including parameters, return types, where clauses, and annotations (e.g., `pure`, `concurrent`).
- **ClassDecl / StructDecl / EnumDecl**: Represent nominal types. `ClassDecl` supports methods and fields, while `StructDecl` is a simpler named tuple. `EnumDecl` represents sum types with variants.
- **InterfaceDecl**: Defines interfaces, including embedded interfaces and default fields. It also supports `DestructorBlock`s for resource management.
- **ImplBlock**: Maps interfaces to concrete types, supporting alias, field-bind, and inline-function mappings.
- **RelationDecl**: Defines relationships between types (e.g., `owns`, `refs`), often used for automated field injection.

### Type Expressions (`TypeExpr`)
Represents types as written in source code (e.g., `map[string]int?`).
- **Kind**: An enum (`TypeNamed`, `TypeOptional`, `TypeUnion`, etc.).
- **Data**: A type-specific struct (e.g., `MapType`, `OptionalType`) held as `any`.

### Expressions (`Expr`)
Represents value-producing computations.
- **Kind**: An enum (`ExprCall`, `ExprBinary`, `ExprIdent`, etc.).
- **Data**: A type-specific struct (e.g., `CallExpr`, `BinaryExpr`) held as `any`.
- **ResolvedType**: An `any` field (intended to hold a `*checker.Type`) populated by the type checker.

### Statements (`Stmt`)
Represents executable actions.
- **Kind**: An enum (`StmtVarDecl`, `StmtIf`, `StmtFor`, etc.).
- **Data**: A type-specific struct (e.g., `VarDeclStmt`, `IfStmt`) held as `any`.

### Patterns (`Pattern`)
Represents patterns used in `match` arms or `if let` / `let..else` statements.
- **Kind**: An enum (`PatVariant`, `PatIdent`, `PatLiteral`, etc.).
- **Data**: A type-specific struct (e.g., `VariantPattern`) held as `any`.

## Implementation Details

### Variant Representation
To avoid deep and complex interface hierarchies, the AST uses a "Kind + Data" pattern. For example, an `Expr` has a `Kind` field that identifies what it is, and a `Data` field of type `any` that holds the specific details. This simplifies traversal and serialization at the cost of some Go-level type safety (requiring type assertions). This pattern is applied consistently to `TypeExpr`, `Expr`, `Stmt`, and `Pattern`.

### String Interpolation
`StringInterpExpr` handles formatted strings like `f"Value: {x}"`. It stores a slice of `Expr` nodes where literal parts are `ExprStringLit` and interpolated parts are arbitrary `Expr` nodes. The parts always alternate, starting and ending with a string literal (which may be empty).

### Error Propagation and Optionals
The AST explicitly supports Forge's error handling and optional types:
- **TryExpr (`expr?`)**: Represents error propagation.
- **UnwrapExpr (`expr!`)**: Represents forced optional unwrapping.
- **OptionalType (`T?`)**: Represents a nullable type.

### Concurrency and Safety
The AST includes nodes for Forge's concurrency primitives:
- **SpawnStmt**: Represents starting a new task.
- **SelectStmt**: Represents channel multiplexing.
- **LockStmt**: Represents explicit mutex acquisition.
- **Annotations**: `FuncDecl` includes `Annotations` for tracking `concurrent`, `pure`, and lock requirements (`RequiresLock`, `ExcludesLock`).

### Relations and Field Injection
The `RelationDecl` and `InterfaceFieldDecl` types support Forge's unique approach to data modeling. Relations like `owns` or `refs` can trigger the injection of fields (defined in interfaces) into concrete classes, allowing for automated management of complex data structures like doubly-linked lists or parent-child hierarchies.

### Documentation and .lyric Files
The `ast` module is itself documented using the Lyre system. The `ast.go.lyric` file serves as the "source of truth" for human-authored intent, architectural overviews, and invariants for this module. This file is parsed by the `lyric` extractor and merged with the structural information from the Go source files to produce the final documentation.

## Dependencies

The `ast` module is designed to be highly independent:
- **Standard Library**: Depends only on `fmt`.
- **Internal Modules**: No dependencies on other `pkg/` modules. This prevents circular dependencies, as almost every other module needs to import `ast`.

## Technical Debt and Future Work

- **Type Safety**: The use of `any` for `Data` and `ResolvedType` fields requires careful type assertions throughout the compiler. While this simplifies the AST structure, it moves some error checking from compile-time to run-time (within the compiler itself).
- **Documentation**: The `.lyric` file for this module contains many "TODO: explain" entries, indicating that the field-level documentation could be significantly improved.
- **AST Mutation**: As the compiler evolves, the patterns for how the AST is mutated (e.g., during desugaring) should be more formally defined to ensure consistency across different transformation passes.
- **Visitor Pattern**: Currently, most consumers of the AST use manual type switches. Implementing a standard Visitor pattern might simplify some traversal logic, though the current "Kind + Data" pattern is already quite ergonomic for Go's `switch` statement.
