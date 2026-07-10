// Package ast defines the AST node types for the Forge language.
// Both .lyric (declaration-only) and .fg (full code) files parse into these types.
package ast

import "fmt"

// Pos is a position in source code.
type Pos struct {
	File   string
	Line   int
	Column int
}

// Span is a range in source code.
type Span struct {
	Start Pos
	End   Pos
}

// TypeExpr represents any type expression in Forge.
type TypeExpr struct {
	Kind TypeExprKind
	Data any // one of NamedType, OptionalType, UnionType, SequenceType, MapType, TupleType, FuncType, ChannelType, or nil for Lock/Unit
	Span Span
}

// TypeExprKind discriminates the kinds of type expressions.
type TypeExprKind int

const (
	TypeNamed     TypeExprKind = iota // T, Stack<T>, map[K]V
	TypeOptional                      // T?
	TypeUnion                         // T | U
	TypeSequence                      // [T]
	TypeMap                           // map[K]V
	TypeTuple                         // (T, U) or (x: T, y: U)
	TypeFunc                          // T -> U
	TypeChannel                       // channel<T>
	TypeGenerator                     // gen T
	TypeLock                          // lock
	TypeUnit                          // unit
	TypeRef                           // ref T — reference (Go *T)
)

var typeExprKindNames = [...]string{
	"Named", "Optional", "Union", "Sequence", "Map", "Tuple",
	"Func", "Channel", "Generator", "Lock", "Unit", "Ref",
}

// String returns the human-readable name of the type expression kind.
func (k TypeExprKind) String() string {
	if int(k) >= 0 && int(k) < len(typeExprKindNames) {
		return typeExprKindNames[k]
	}
	return fmt.Sprintf("TypeExprKind(%d)", int(k))
}

// NamedType holds the name and optional type arguments for TypeNamed.
type NamedType struct {
	Name string
	Args []TypeExpr
}

// OptionalType wraps the inner type for T?.
type OptionalType struct {
	Inner TypeExpr
}

// UnionType holds variants for T | U.
type UnionType struct {
	Variants []TypeExpr
}

// SequenceType holds the element type for [T].
type SequenceType struct {
	Elem TypeExpr
}

// MapType holds key and value types for map[K]V.
type MapType struct {
	Key   TypeExpr
	Value TypeExpr
}

// TupleField is a single element in a tuple type.
type TupleField struct {
	Name string // empty for positional
	Type TypeExpr
}

// TupleType holds fields for (T, U) or (x: T, y: U).
type TupleType struct {
	Fields []TupleField
}

// FuncType holds parameter and return types for T -> U.
type FuncType struct {
	Params []TypeExpr
	Return TypeExpr
}

// RefType holds the inner type for ref T — a reference (maps to Go *T).
type RefType struct {
	Inner TypeExpr
}

// ChannelType holds the element type for channel<T>.
type ChannelType struct {
	Elem TypeExpr
}

// GeneratorType holds the element type for gen T.
type GeneratorType struct {
	Elem TypeExpr
}

// TypeParam is a type parameter declaration like T or T: Comparable.
type TypeParam struct {
	Name       string
	Constraint string // empty if unconstrained
	Span       Span
}

// Param is a function parameter.
type Param struct {
	Name   string
	Type   TypeExpr
	IsMut  bool // true for `mut self`
	IsSelf bool
	Span   Span
}

// WhereClause constrains type variables.
// Single-type: where T: Integer (Variable="T", Constraint="Integer")
// Relational: where Graph<G, N, E> (Variable="", Constraint="Graph", TypeArgs set)
type WhereClause struct {
	Variable   string // empty for bare relational constraints
	Constraint string
	TypeArgs   []TypeExpr // populated for relational constraints: Graph<G, N, E>
	Span       Span
}

// Annotations holds all function/method annotations.
type Annotations struct {
	Why          string
	Concurrent   *bool // nil = not specified
	RequiresLock []string
	ExcludesLock []string
	Raises       []string
	Requires     []string // preconditions
	Ensures      []string // postconditions
	Spawns       bool
	Pure         bool
}

// FuncDecl is a function or method declaration.
type FuncDecl struct {
	Name         string
	IsPublic     bool   // true if declared with `pub`
	ReceiverType string // non-empty for multi-class interface methods: func T.method(self)
	TypeParams   []TypeParam
	Params       []Param
	ReturnType   *TypeExpr // nil for missing (error in .lyric)
	Where        []WhereClause
	Annotations  Annotations
	// Body is nil in .lyric files, holds the function body in .fg files.
	Body *Block
	Span Span
}

// Field is a struct or class field.
type Field struct {
	Name      string
	IsPublic  bool // true if declared with `pub`
	Type      TypeExpr
	Default   *Expr  // nil if no default value
	GuardedBy string // empty if not guarded
	Why       string
	Span      Span
}

// ClassDecl is a class declaration.
type ClassDecl struct {
	Name       string
	IsPublic   bool // true if declared with `pub`
	TypeParams []TypeParam
	Where      []WhereClause
	Implements []string
	Fields     []Field
	Methods    []FuncDecl
	Why        string
	Source     []string
	Span       Span
}

// StructDecl is a struct (named tuple) declaration.
type StructDecl struct {
	Name       string
	IsPublic   bool // true if declared with `pub`
	TypeParams []TypeParam
	Fields     []Field
	Why        string
	Span       Span
}

// EnumVariant is a single variant in an enum.
type EnumVariant struct {
	Name   string
	Fields []TupleField
	Span   Span
}

// EnumDecl is an enum (sum type) declaration.
type EnumDecl struct {
	Name       string
	IsPublic   bool // true if declared with `pub`
	TypeParams []TypeParam
	Variants   []EnumVariant
	Why        string
	Span       Span
}

// InterfaceFieldDecl is a default field declaration in an interface.
// Syntax: field T.name: Type
// These are injected into concrete classes when a relation is declared.
type InterfaceFieldDecl struct {
	TypeParam string   // "P", "C" — which type param this field belongs to
	Name      string   // field name (e.g. "first", "next", "parent")
	Type      TypeExpr // field type (may reference other type params)
	Span      Span
}

// InterfaceDecl is an interface declaration.
type InterfaceDecl struct {
	Name        string
	IsPublic    bool // true if declared with `pub`
	TypeParams  []TypeParam
	Implements  []string         // composed interfaces (legacy)
	Embeds      []InterfaceEmbed // embedded interfaces with type args
	Methods     []FuncDecl
	Fields      []InterfaceFieldDecl // default fields: field T.name: Type
	Destructors []DestructorBlock    // destructor T { ... } blocks
	Why         string
	Span        Span
}

// InterfaceEmbed represents an embedded interface: embed DoublyLinked<P, C>
type InterfaceEmbed struct {
	Name     string     // interface name being embedded
	TypeArgs []TypeExpr // type arguments (e.g. P, C)
	Span     Span
}

// DestructorBlock is a destructor code block in an interface, bound to a type parameter.
// The body is copied to the end of the concrete class's destroy method during desugaring.
type DestructorBlock struct {
	TypeParam string // which type parameter this destructor applies to (e.g. "P" or "C")
	Body      Block  // the code block to append to the concrete class's destroy method
	Span      Span
}

// ImplMappingKind distinguishes the three forms of impl mappings.
type ImplMappingKind int

const (
	ImplAlias     ImplMappingKind = iota // T.method = ConcreteClass.method
	ImplFieldBind                        // T.field <-> ConcreteClass.field
	ImplInline                           // T.method(self) -> RetType { body }
)

// ImplMapping is a single mapping inside an impl block.
type ImplMapping struct {
	TypeParam  string // "G", "N", "E" — the interface type param
	MethodName string // the method name on the interface side
	Kind       ImplMappingKind
	// For Alias/FieldBind:
	TargetClass  string // "CircuitGraph", "Component"
	TargetMember string // "components", "outWires"
	// For Inline:
	InlineFunc *FuncDecl // full function declaration
	Span       Span
}

// ImplBlock maps an interface to concrete types.
type ImplBlock struct {
	InterfaceName string     // "Graph", "DoublyLinked"
	ForType       string     // optional: "impl Printable for Dog" — the implementing type
	TypeArgs      []TypeExpr // concrete types: <CircuitGraph, Component, Wire>
	Label         string     // optional: "as byEmail" for disambiguation
	Mappings      []ImplMapping
	Span          Span
}

// RelationKind distinguishes owns from refs.
type RelationKind int

const (
	Owns RelationKind = iota
	Refs
)

// RelationSide is one side of a relation.
type RelationSide struct {
	TypeName string
	TypeArgs []string // type parameters, e.g. ["V"] from Dict<V>
	Label    string   // empty if unlabeled
}

// RelationDecl is a relation declaration.
type RelationDecl struct {
	Hint   string // DoublyLinked, ArrayList, Hashed, or empty
	Parent RelationSide
	Kind   RelationKind
	Child  RelationSide
	IsMany bool // [Child] vs Child
	Span   Span
}

// DocBlock is a named narrative section.
type DocBlock struct {
	Section string
	Content string
	Span    Span
}

// InvariantDecl is an invariant claim.
type InvariantDecl struct {
	Claim      string
	VerifiedAt string // commit hash, or empty
	Span       Span
}

// ImportDecl is an import statement.
type ImportDecl struct {
	Alias string
	Path  string
	Span  Span
}

// TypeAliasDecl is a type alias: type Name = OtherType
type TypeAliasDecl struct {
	Name     string
	IsPublic bool // true if declared with `pub`
	Type     TypeExpr
	Span     Span
}

// LyricBlock is the top-level lyric { ... } block.
type LyricBlock struct {
	Name        string
	Why         string
	Imports     []ImportDecl
	Docs        []DocBlock
	Invariants  []InvariantDecl
	Structs     []StructDecl
	Enums       []EnumDecl
	Interfaces  []InterfaceDecl
	Classes     []ClassDecl
	Functions   []FuncDecl
	Relations   []RelationDecl
	ImplBlocks  []ImplBlock
	TypeAliases []TypeAliasDecl
	Constants   []ConstDecl
	Source      []string
	Fake        string
	Span        Span
}

// ConstDecl represents a top-level constant: let [mut] NAME: Type = value
type ConstDecl struct {
	Name     string
	Type     *TypeExpr // nil if inferred
	Value    Expr
	IsPublic bool
	IsMut    bool
	Span     Span
}

// Comment represents a line comment in the source.
type Comment struct {
	Text string // full text including "// " prefix
	Pos  Pos    // position of the "//"
}

// File is the top-level AST node.
type File struct {
	Filename string
	Blocks   []LyricBlock
	Comments []Comment // all comments in the file, ordered by position
	Span     Span
}

// DocComment returns the contiguous block of comments immediately preceding
// the given line, or nil if there are none.
func (f *File) DocComment(line int) []Comment {
	// Find comments ending at line-1
	var result []Comment
	for i := len(f.Comments) - 1; i >= 0; i-- {
		c := f.Comments[i]
		if c.Pos.Line == line-1-len(result) {
			result = append([]Comment{c}, result...)
		} else if c.Pos.Line < line-1-len(result) {
			break
		}
	}
	return result
}
