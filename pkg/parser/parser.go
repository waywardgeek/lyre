package parser

import (
	"fmt"
	"strings"

	"github.com/waywardgeek/lyre/pkg/ast"
)

// ParseError is a syntax error with source position.
type ParseError struct {
	Message    string
	Span       ast.Span
	SourceLine string // the source line where the error occurred
}

func (e *ParseError) Error() string {
	msg := fmt.Sprintf("%s:%d:%d: %s", e.Span.Start.File, e.Span.Start.Line, e.Span.Start.Column, e.Message)
	if e.SourceLine != "" {
		msg += "\n  " + e.SourceLine
		if e.Span.Start.Column > 0 {
			msg += "\n  " + strings.Repeat(" ", e.Span.Start.Column-1) + "^"
		}
	}
	return msg
}

// Parser is a PEG parser for .lyric files.
type Parser struct {
	lex         *Lexer
	errors      []error
	exprDepth   int    // tracks nesting inside () and [] where { can't start a block
	noStructLit bool   // suppresses struct literal parsing (set in for/while/if/match conditions)
	pushed      *Token // pushed-back token (for >> splitting)
}

// ParseFile parses a .lyric or .fg file into an AST.
func ParseFile(source, filename string) (*ast.File, error) {
	lex := NewLexer(source, filename)
	p := &Parser{lex: lex}
	return p.parseFile()
}

// ParseString parses from a string, using "<string>" as the filename.
func ParseString(source string) (*ast.File, error) {
	return ParseFile(source, "<string>")
}

func (p *Parser) peek() Token {
	if p.pushed != nil {
		return *p.pushed
	}
	return p.lex.Peek()
}
func (p *Parser) next() Token {
	if p.pushed != nil {
		t := *p.pushed
		p.pushed = nil
		return t
	}
	return p.lex.Next()
}
func (p *Parser) pushBack(t Token) {
	p.pushed = &t
}

func (p *Parser) skipNewlines() {
	for p.peek().Kind == TNewline {
		p.next()
	}
}

// peekAnnotation checks if the current token is a TIdent that matches an annotation keyword.
// Returns the annotation TokenKind if it matches, or 0 if not.
func (p *Parser) peekAnnotation() TokenKind {
	tok := p.peek()
	if tok.Kind != TIdent {
		return 0
	}
	if kind, ok := annotationKeywords[tok.Text]; ok {
		return kind
	}
	return 0
}

// consumeAnnotation checks the current token for an annotation keyword match.
// If it matches, consumes the token (rewriting its Kind) and returns true.
func (p *Parser) consumeAnnotation(expected TokenKind) bool {
	tok := p.peek()
	if tok.Kind == TIdent {
		if kind, ok := annotationKeywords[tok.Text]; ok && kind == expected {
			p.next() // consume it
			return true
		}
	}
	return false
}

// isWhyAnnotation peeks ahead to determine if the current `why` token is an annotation
// (why: "string literal") vs a field name (why: TypeExpr). Returns true for annotation form.
func (p *Parser) isWhyAnnotation() bool {
	// Save lexer state
	saved := *p.lex
	p.next()         // consume 'why'
	p.next()         // consume ':'
	tok := p.peek()  // look at what follows
	*p.lex = saved   // restore
	return tok.Kind == TStringLit || tok.Kind == TTripleStringLit
}

// newError creates a ParseError with the source line from the lexer.
func (p *Parser) newError(span ast.Span, format string, args ...any) *ParseError {
	return &ParseError{
		Message:    fmt.Sprintf(format, args...),
		Span:       span,
		SourceLine: p.getSourceLine(span.Start.Line),
	}
}

// getSourceLine extracts the source line at the given 1-based line number.
func (p *Parser) getSourceLine(line int) string {
	src := p.lex.source
	for i, cur := 0, 1; i < len(src); i++ {
		if cur == line {
			end := strings.Index(src[i:], "\n")
			if end < 0 {
				return src[i:]
			}
			return src[i : i+end]
		}
		if src[i] == '\n' {
			cur++
		}
	}
	return ""
}

func (p *Parser) expect(kind TokenKind) (Token, error) {
	tok := p.next()
	if tok.Kind != kind {
		return tok, p.newError(tok.Span, "expected %s, got %s (%q)", tokenNames[kind], tokenNames[tok.Kind], tok.Text)
	}
	return tok, nil
}

// expectIdentLike accepts TIdent or any annotation keyword that could be used as a name.
func (p *Parser) expectIdentLike() (Token, error) {
	tok := p.next()
	if tok.Kind == TIdent {
		return tok, nil
	}
	// Keywords and annotation keywords can be used as field/param names
	if _, ok := annotationKeywords[tok.Text]; ok {
		tok.Kind = TIdent
		return tok, nil
	}
	if _, ok := keywords[tok.Text]; ok {
		tok.Kind = TIdent
		return tok, nil
	}
	return tok, p.newError(tok.Span, "expected identifier, got %s (%q)", tokenNames[tok.Kind], tok.Text)
}

func (p *Parser) parseFile() (*ast.File, error) {
	file := &ast.File{Filename: p.lex.filename}
	start := p.peek().Span.Start
	p.skipNewlines()

	for p.peek().Kind != TEOF {
		p.skipNewlines()
		if p.peek().Kind == TEOF {
			break
		}
		block, err := p.parseLyricBlock()
		if err != nil {
			return nil, err
		}
		file.Blocks = append(file.Blocks, *block)
		p.skipNewlines()
	}

	file.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: p.peek().Span.End}
	file.Comments = p.lex.Comments
	return file, nil
}

func (p *Parser) parseLyricBlock() (*ast.LyricBlock, error) {
	start := p.peek().Span.Start

	// Optional: if the file doesn't start with `lyric`, create an implicit block
	// using the filename as the module name.
	implicit := p.peek().Kind != TLyric
	var block *ast.LyricBlock

	if implicit {
		// Derive module name from filename: "/path/foo.fg" → "foo"
		name := p.lex.filename
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if i := strings.Index(name, "."); i >= 0 {
			name = name[:i]
		}
		if name == "" || name == "<string>" {
			name = "main"
		}
		block = &ast.LyricBlock{Name: name}
	} else {
		if _, err := p.expect(TLyric); err != nil {
			return nil, err
		}
		nameTok, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		block = &ast.LyricBlock{Name: nameTok.Text}
		if _, err := p.expect(TLBrace); err != nil {
			return nil, err
		}
	}
	p.skipNewlines()

	for p.peek().Kind != TRBrace && p.peek().Kind != TEOF {
		if err := p.parseForgeItem(block); err != nil {
			return nil, err
		}
		p.skipNewlines()
	}

	if !implicit {
		if _, err := p.expect(TRBrace); err != nil {
			return nil, err
		}
	}

	block.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: p.peek().Span.End}
	return block, nil
}

func (p *Parser) parseForgeItem(block *ast.LyricBlock) error {
	tok := p.peek()

	// Handle `pub` visibility modifier
	isPub := false
	if tok.Kind == TPub {
		isPub = true
		p.next() // consume 'pub'
		tok = p.peek()
	}

	// Annotation keywords lex as TIdent — rewrite to their token kind for the switch below.
	if tok.Kind == TIdent {
		if kind, ok := annotationKeywords[tok.Text]; ok {
			tok.Kind = kind
		}
	}

	switch tok.Kind {
	case TWhy:
		if isPub {
			return &ParseError{Message: "pub cannot be applied to why", Span: tok.Span}
		}
		why, err := p.parseWhy()
		if err != nil {
			return err
		}
		block.Why = why
	case TDoc:
		if isPub {
			return &ParseError{Message: "pub cannot be applied to doc", Span: tok.Span}
		}
		doc, err := p.parseDoc()
		if err != nil {
			return err
		}
		block.Docs = append(block.Docs, *doc)
	case TInvariant:
		if isPub {
			return &ParseError{Message: "pub cannot be applied to invariant", Span: tok.Span}
		}
		inv, err := p.parseInvariant()
		if err != nil {
			return err
		}
		block.Invariants = append(block.Invariants, *inv)
	case TImport:
		if isPub {
			return &ParseError{Message: "pub cannot be applied to import", Span: tok.Span}
		}
		imp, err := p.parseImport()
		if err != nil {
			return err
		}
		block.Imports = append(block.Imports, *imp)
	case TStruct:
		s, err := p.parseStruct()
		if err != nil {
			return err
		}
		s.IsPublic = isPub
		block.Structs = append(block.Structs, *s)
	case TEnum:
		e, err := p.parseEnum()
		if err != nil {
			return err
		}
		e.IsPublic = isPub
		block.Enums = append(block.Enums, *e)
	case TInterface:
		iface, err := p.parseInterface()
		if err != nil {
			return err
		}
		iface.IsPublic = isPub
		block.Interfaces = append(block.Interfaces, *iface)
	case TClass:
		cls, err := p.parseClass()
		if err != nil {
			return err
		}
		cls.IsPublic = isPub
		block.Classes = append(block.Classes, *cls)
	case TFunc:
		fn, err := p.parseFunc()
		if err != nil {
			return err
		}
		fn.IsPublic = isPub
		block.Functions = append(block.Functions, *fn)
	case TRelation:
		if isPub {
			return &ParseError{Message: "pub cannot be applied to relation", Span: tok.Span}
		}
		rel, err := p.parseRelation()
		if err != nil {
			return err
		}
		block.Relations = append(block.Relations, *rel)
	case TType:
		ta, err := p.parseTypeAlias()
		if err != nil {
			return err
		}
		ta.IsPublic = isPub
		block.TypeAliases = append(block.TypeAliases, *ta)
	case TImpl:
		if isPub {
			return &ParseError{Message: "pub cannot be applied to impl", Span: tok.Span}
		}
		impl, err := p.parseImpl()
		if err != nil {
			return err
		}
		block.ImplBlocks = append(block.ImplBlocks, *impl)
	case TSource:
		if isPub {
			return &ParseError{Message: "pub cannot be applied to source", Span: tok.Span}
		}
		src, err := p.parseSource()
		if err != nil {
			return err
		}
		block.Source = src
	case TFake:
		if isPub {
			return &ParseError{Message: "pub cannot be applied to fake", Span: tok.Span}
		}
		fake, err := p.parseFake()
		if err != nil {
			return err
		}
		block.Fake = fake
	case TLet:
		c, err := p.parseConstDecl()
		if err != nil {
			return err
		}
		c.IsPublic = isPub
		block.Constants = append(block.Constants, *c)

	default:
		return &ParseError{
			Message: fmt.Sprintf("unexpected token %s (%q) in lyric block", tokenNames[tok.Kind], tok.Text),
			Span:    tok.Span,
		}
	}
	return nil
}

// parseWhy parses: why: "..."
func (p *Parser) parseWhy() (string, error) {
	p.next() // consume 'why'
	if _, err := p.expect(TColon); err != nil {
		return "", err
	}
	tok, err := p.expect(TStringLit)
	if err != nil {
		return "", err
	}
	return tok.Text, nil
}

// parseDoc parses: doc "Section": """..."""
func (p *Parser) parseDoc() (*ast.DocBlock, error) {
	start := p.peek().Span.Start
	p.next() // consume 'doc'
	section, err := p.expect(TStringLit)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TColon); err != nil {
		return nil, err
	}
	content, err := p.expect(TTripleStringLit)
	if err != nil {
		return nil, err
	}
	return &ast.DocBlock{
		Section: section.Text,
		Content: content.Text,
		Span:    ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: content.Span.End},
	}, nil
}

// parseInvariant parses: invariant: "claim" [verified_at: "hash"]
func (p *Parser) parseInvariant() (*ast.InvariantDecl, error) {
	start := p.peek().Span.Start
	p.next() // consume 'invariant'
	if _, err := p.expect(TColon); err != nil {
		return nil, err
	}
	claim, err := p.expect(TStringLit)
	if err != nil {
		return nil, err
	}
	inv := &ast.InvariantDecl{
		Claim: claim.Text,
		Span:  ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: claim.Span.End},
	}
	// Check for optional verified_at
	p.skipNewlines()
	if p.peekAnnotation() == TVerifiedAt {
		p.next()
		if _, err := p.expect(TColon); err != nil {
			return nil, err
		}
		hash, err := p.expect(TStringLit)
		if err != nil {
			return nil, err
		}
		inv.VerifiedAt = hash.Text
		inv.Span.End = hash.Span.End
	}
	return inv, nil
}

// parseImport parses: import <ident> [from "path"]
// Simple form: import ast        → alias="ast", path="ast"
// Extended:    import v2 from "parser/v2" → alias="v2", path="parser/v2"
func (p *Parser) parseImport() (*ast.ImportDecl, error) {
	start := p.peek().Span.Start
	p.next() // consume 'import'
	alias, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	// Check for optional 'from "path"'
	path := alias.Text // default: alias IS the path
	end := alias.Span.End
	if p.peek().Kind == TFrom {
		p.next() // consume 'from'
		pathTok, err := p.expect(TStringLit)
		if err != nil {
			return nil, err
		}
		path = pathTok.Text
		end = pathTok.Span.End
	}
	return &ast.ImportDecl{
		Alias: alias.Text,
		Path:  path,
		Span:  ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end},
	}, nil
}

// parseTypeAlias parses: type Name = TypeExpr
func (p *Parser) parseTypeAlias() (*ast.TypeAliasDecl, error) {
	start := p.peek().Span.Start
	p.next() // consume 'type'
	name, err := p.expectIdentLike()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TAssign); err != nil {
		return nil, err
	}
	typeExpr, err := p.parseTypeExpr()
	if err != nil {
		return nil, err
	}
	return &ast.TypeAliasDecl{
		Name: name.Text,
		Type: *typeExpr,
		Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: typeExpr.Span.End},
	}, nil
}

// parseStruct parses: struct Name[<TypeParams>] { fields }
func (p *Parser) parseStruct() (*ast.StructDecl, error) {
	start := p.peek().Span.Start
	p.next() // consume 'struct'
	name, err := p.expectIdentLike()
	if err != nil {
		return nil, err
	}
	s := &ast.StructDecl{Name: name.Text}

	// Optional type params
	if p.peek().Kind == TLt {
		params, err := p.parseTypeParams()
		if err != nil {
			return nil, err
		}
		s.TypeParams = params
	}

	if _, err := p.expect(TLBrace); err != nil {
		return nil, err
	}
	p.skipNewlines()

	for p.peek().Kind != TRBrace && p.peek().Kind != TEOF {
		if p.peekAnnotation() == TWhy && p.isWhyAnnotation() {
			why, err := p.parseWhy()
			if err != nil {
				return nil, err
			}
			s.Why = why
		} else {
			field, err := p.parseField()
			if err != nil {
				return nil, err
			}
			s.Fields = append(s.Fields, *field)
		}
		p.skipNewlines()
	}

	end, err := p.expect(TRBrace)
	if err != nil {
		return nil, err
	}
	s.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end.Span.End}
	return s, nil
}

// parseEnum parses: enum Name[<TypeParams>] { Variants }
func (p *Parser) parseEnum() (*ast.EnumDecl, error) {
	start := p.peek().Span.Start
	p.next() // consume 'enum'
	name, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	e := &ast.EnumDecl{Name: name.Text}

	if p.peek().Kind == TLt {
		params, err := p.parseTypeParams()
		if err != nil {
			return nil, err
		}
		e.TypeParams = params
	}

	if _, err := p.expect(TLBrace); err != nil {
		return nil, err
	}
	p.skipNewlines()

	for p.peek().Kind != TRBrace && p.peek().Kind != TEOF {
		if p.peekAnnotation() == TWhy && p.isWhyAnnotation() {
			why, err := p.parseWhy()
			if err != nil {
				return nil, err
			}
			e.Why = why
		} else {
			variant, err := p.parseEnumVariant()
			if err != nil {
				return nil, err
			}
			e.Variants = append(e.Variants, *variant)
		}
		p.skipNewlines()
	}

	end, err := p.expect(TRBrace)
	if err != nil {
		return nil, err
	}
	e.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end.Span.End}
	return e, nil
}

func (p *Parser) parseEnumVariant() (*ast.EnumVariant, error) {
	start := p.peek().Span.Start
	name, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	v := &ast.EnumVariant{Name: name.Text}

	// Optional payload
	if p.peek().Kind == TLParen {
		p.next()
		p.skipNewlines()
		for p.peek().Kind != TRParen && p.peek().Kind != TEOF {
			field, err := p.parseTupleField()
			if err != nil {
				return nil, err
			}
			v.Fields = append(v.Fields, *field)
			if p.peek().Kind == TComma {
				p.next()
			}
			p.skipNewlines()
		}
		if _, err := p.expect(TRParen); err != nil {
			return nil, err
		}
	}

	v.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: p.peek().Span.End}
	return v, nil
}

func (p *Parser) parseTupleField() (*ast.TupleField, error) {
	// Could be "name: Type" or just "Type"
	// Try name: Type first
	if p.peek().Kind == TIdent {
		saved := *p.lex // save state
		name := p.next()
		if p.peek().Kind == TColon {
			p.next()
			typ, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			return &ast.TupleField{Name: name.Text, Type: *typ}, nil
		}
		// Restore — it was just a type name
		*p.lex = saved
	}
	typ, err := p.parseTypeExpr()
	if err != nil {
		return nil, err
	}
	return &ast.TupleField{Type: *typ}, nil
}

// parseInterface parses: interface Name[<TypeParams>] { methods }
func (p *Parser) parseInterface() (*ast.InterfaceDecl, error) {
	start := p.peek().Span.Start
	p.next() // consume 'interface'
	name, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	iface := &ast.InterfaceDecl{Name: name.Text}

	if p.peek().Kind == TLt {
		params, err := p.parseTypeParams()
		if err != nil {
			return nil, err
		}
		iface.TypeParams = params
	}

	if _, err := p.expect(TLBrace); err != nil {
		return nil, err
	}
	p.skipNewlines()

	for p.peek().Kind != TRBrace && p.peek().Kind != TEOF {
		if p.peekAnnotation() == TWhy && p.isWhyAnnotation() {
			why, err := p.parseWhy()
			if err != nil {
				return nil, err
			}
			iface.Why = why
		} else if p.peek().Kind == TImplements || p.peekAnnotation() == TImplements {
			p.next()
			imp, err := p.expect(TIdent)
			if err != nil {
				return nil, err
			}
			iface.Implements = append(iface.Implements, imp.Text)
		} else if p.peek().Kind == TFunc || p.peekAnnotation() == TFunc || p.peek().Kind == TPub {
			isPub := false
			if p.peek().Kind == TPub {
				isPub = true
				p.next() // consume 'pub'
				if p.peek().Kind != TFunc && p.peekAnnotation() != TFunc {
					return nil, &ParseError{Message: "expected func after pub in interface body", Span: p.peek().Span}
				}
			}
			fn, err := p.parseFunc()
			if err != nil {
				return nil, err
			}
			fn.IsPublic = isPub
			iface.Methods = append(iface.Methods, *fn)
		} else if p.peek().Kind == TEmbed {
			emb, err := p.parseInterfaceEmbed()
			if err != nil {
				return nil, err
			}
			iface.Embeds = append(iface.Embeds, *emb)
		} else if p.peek().Kind == TField || p.peekAnnotation() == TField {
			fd, err := p.parseInterfaceField()
			if err != nil {
				return nil, err
			}
			iface.Fields = append(iface.Fields, *fd)
		} else if p.peek().Kind == TDestructor {
			db, err := p.parseDestructorBlock()
			if err != nil {
				return nil, err
			}
			iface.Destructors = append(iface.Destructors, *db)
		} else {
			return nil, &ParseError{
				Message: fmt.Sprintf("unexpected %s in interface body", tokenNames[p.peek().Kind]),
				Span:    p.peek().Span,
			}
		}
		p.skipNewlines()
	}

	end, err := p.expect(TRBrace)
	if err != nil {
		return nil, err
	}
	iface.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end.Span.End}
	return iface, nil
}

// parseInterfaceField parses: field T.name: Type
func (p *Parser) parseInterfaceField() (*ast.InterfaceFieldDecl, error) {
	start := p.peek().Span.Start
	p.next() // consume 'field'

	typeName, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TDot); err != nil {
		return nil, &ParseError{Message: "expected '.' in field T.name", Span: p.peek().Span}
	}
	fieldName, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TColon); err != nil {
		return nil, err
	}
	te, err := p.parseTypeExpr()
	if err != nil {
		return nil, err
	}
	return &ast.InterfaceFieldDecl{
		TypeParam: typeName.Text,
		Name:      fieldName.Text,
		Type:      *te,
		Span:      ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: te.Span.End},
	}, nil
}

// parseInterfaceEmbed parses: embed InterfaceName<TypeArg1, TypeArg2>
func (p *Parser) parseInterfaceEmbed() (*ast.InterfaceEmbed, error) {
	start := p.peek().Span.Start
	p.next() // consume 'embed'

	name, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	emb := &ast.InterfaceEmbed{Name: name.Text}
	end := name.Span.End

	// Parse optional type arguments: <T1, T2>
	if p.peek().Kind == TLt {
		p.next() // consume '<'
		for {
			te, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			emb.TypeArgs = append(emb.TypeArgs, *te)
			if p.peek().Kind == TComma {
				p.next()
			} else {
				break
			}
		}
		gt, err := p.expect(TGt)
		if err != nil {
			return nil, err
		}
		end = gt.Span.End
	}

	emb.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end}
	return emb, nil
}

// parseDestructorBlock parses: destructor T { body }
func (p *Parser) parseDestructorBlock() (*ast.DestructorBlock, error) {
	start := p.peek().Span.Start
	p.next() // consume 'destructor'

	typeName, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.DestructorBlock{
		TypeParam: typeName.Text,
		Body:      *body,
		Span:      ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: body.Span.End},
	}, nil
}

// parseImpl parses: impl Interface<T1, T2, ...> [as label] { mappings }
func (p *Parser) parseImpl() (*ast.ImplBlock, error) {
	start := p.peek().Span.Start
	p.next() // consume 'impl'

	name, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	impl := &ast.ImplBlock{InterfaceName: name.Text}

	// Type arguments: <ConcreteType1, ConcreteType2, ...>
	if p.peek().Kind == TLt {
		p.next() // consume '<'
		for p.peek().Kind != TGt && p.peek().Kind != TEOF {
			te, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			impl.TypeArgs = append(impl.TypeArgs, *te)
			if p.peek().Kind == TComma {
				p.next()
			}
		}
		if _, err := p.expect(TGt); err != nil {
			return nil, err
		}
	}

	// Optional: for ConcreteType (ergonomic impl syntax)
	if p.peek().Kind == TFor {
		p.next()
		forType, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		impl.ForType = forType.Text
	}

	// Optional: as label
	if p.peek().Kind == TAs {
		p.next()
		label, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		impl.Label = label.Text
	}

	if _, err := p.expect(TLBrace); err != nil {
		return nil, err
	}
	p.skipNewlines()

	for p.peek().Kind != TRBrace && p.peek().Kind != TEOF {
		var mapping *ast.ImplMapping
		var err error
		if impl.ForType != "" {
			mapping, err = p.parseImplMappingShort(impl.ForType)
		} else {
			mapping, err = p.parseImplMapping()
		}
		if err != nil {
			return nil, err
		}
		impl.Mappings = append(impl.Mappings, *mapping)
		p.skipNewlines()
	}

	end, err := p.expect(TRBrace)
	if err != nil {
		return nil, err
	}
	impl.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end.Span.End}
	return impl, nil
}

// parseImplMappingShort parses a short-form mapping: method = member
// Used when impl has "for Type" — the type param and target class are implicit.
func (p *Parser) parseImplMappingShort(forType string) (*ast.ImplMapping, error) {
	start := p.peek().Span.Start
	methodName, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	m := &ast.ImplMapping{
		TypeParam:  forType,
		MethodName: methodName.Text,
	}
	if p.peek().Kind == TAssign {
		p.next()
		member, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		m.Kind = ast.ImplAlias
		m.TargetClass = forType
		m.TargetMember = member.Text
	} else if p.peek().Kind == TBiArrow {
		p.next()
		member, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		m.Kind = ast.ImplFieldBind
		m.TargetClass = forType
		m.TargetMember = member.Text
	} else if p.peek().Kind == TLParen {
		// Inline method: method(params) -> RetType { body }
		fn := &ast.FuncDecl{
			Name:         methodName.Text,
			ReceiverType: forType,
		}
		p.next() // consume '('
		p.skipNewlines()
		for p.peek().Kind != TRParen && p.peek().Kind != TEOF {
			param, err := p.parseParam()
			if err != nil {
				return nil, err
			}
			fn.Params = append(fn.Params, *param)
			if p.peek().Kind == TComma {
				p.next()
			}
			p.skipNewlines()
		}
		if _, err := p.expect(TRParen); err != nil {
			return nil, err
		}
		if p.peek().Kind == TArrow {
			p.next()
			ret, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			fn.ReturnType = ret
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		fn.Body = body
		m.Kind = ast.ImplInline
		m.InlineFunc = fn
	} else {
		return nil, &ParseError{Message: fmt.Sprintf("expected '=', '<->', or '(' in impl mapping, got %s", tokenNames[p.peek().Kind]), Span: p.peek().Span}
	}
	m.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: p.peek().Span.Start}
	return m, nil
}

// parseImplMapping parses one mapping inside an impl block.
// Forms: T.method = Class.method | T.field <-> Class.field | T.method(params) -> RetType { body }
func (p *Parser) parseImplMapping() (*ast.ImplMapping, error) {
	start := p.peek().Span.Start

	// Parse T.name
	typeParam, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TDot); err != nil {
		return nil, err
	}
	methodName, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}

	m := &ast.ImplMapping{
		TypeParam:  typeParam.Text,
		MethodName: methodName.Text,
	}

	switch p.peek().Kind {
	case TAssign: // T.method = Class.method
		p.next()
		cls, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TDot); err != nil {
			return nil, err
		}
		member, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		m.Kind = ast.ImplAlias
		m.TargetClass = cls.Text
		m.TargetMember = member.Text

	case TBiArrow: // T.field <-> Class.field
		p.next()
		cls, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TDot); err != nil {
			return nil, err
		}
		member, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		m.Kind = ast.ImplFieldBind
		m.TargetClass = cls.Text
		m.TargetMember = member.Text

	case TLParen: // T.method(params) -> RetType { body } — inline implementation
		// Reconstruct as a FuncDecl with ReceiverType
		fn := &ast.FuncDecl{
			ReceiverType: typeParam.Text,
			Name:         methodName.Text,
		}
		// Parse params
		p.next() // consume '('
		p.skipNewlines()
		for p.peek().Kind != TRParen && p.peek().Kind != TEOF {
			param, err := p.parseParam()
			if err != nil {
				return nil, err
			}
			fn.Params = append(fn.Params, *param)
			if p.peek().Kind == TComma {
				p.next()
			}
			p.skipNewlines()
		}
		if _, err := p.expect(TRParen); err != nil {
			return nil, err
		}
		// Optional return type
		if p.peek().Kind == TArrow {
			p.next()
			ret, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			fn.ReturnType = ret
		}
		// Body
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		fn.Body = body
		m.Kind = ast.ImplInline
		m.InlineFunc = fn

	default:
		return nil, &ParseError{
			Message: fmt.Sprintf("expected '=', '<->', or '(' in impl mapping, got %s", tokenNames[p.peek().Kind]),
			Span:    p.peek().Span,
		}
	}

	m.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: p.peek().Span.End}
	return m, nil
}

// parseClass parses: class Name[<TypeParams>](ctor_params) [implements I1, I2] { fields, methods }
func (p *Parser) parseClass() (*ast.ClassDecl, error) {
	start := p.peek().Span.Start
	p.next() // consume 'class'
	name, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	cls := &ast.ClassDecl{Name: name.Text}

	// Optional type params
	if p.peek().Kind == TLt {
		params, err := p.parseTypeParams()
		if err != nil {
			return nil, err
		}
		cls.TypeParams = params
	}

	// Optional where clause
	p.skipNewlines()
	for p.peek().Kind == TWhere {
		p.next()
		for {
			wc, err := p.parseWhereClause()
			if err != nil {
				return nil, err
			}
			cls.Where = append(cls.Where, *wc)
			if p.peek().Kind == TComma {
				p.next()
			} else {
				break
			}
		}
		p.skipNewlines()
	}

	// Optional implements
	if p.peek().Kind == TImplements || p.peekAnnotation() == TImplements {
		p.next()
		for {
			imp, err := p.expect(TIdent)
			if err != nil {
				return nil, err
			}
			cls.Implements = append(cls.Implements, imp.Text)
			if p.peek().Kind == TComma {
				p.next()
			} else {
				break
			}
		}
	}

	if _, err := p.expect(TLBrace); err != nil {
		return nil, err
	}
	p.skipNewlines()

	for p.peek().Kind != TRBrace && p.peek().Kind != TEOF {
		if p.peekAnnotation() == TWhy && p.isWhyAnnotation() {
			why, err := p.parseWhy()
			if err != nil {
				return nil, err
			}
			cls.Why = why
		} else if p.peekAnnotation() == TSource {
			src, err := p.parseSource()
			if err != nil {
				return nil, err
			}
			cls.Source = src
		} else if p.peek().Kind == TFunc || p.peekAnnotation() == TFunc || p.peek().Kind == TPub {
			isPub := false
			if p.peek().Kind == TPub {
				isPub = true
				p.next() // consume 'pub'
				// After pub, could be func or a field
				if p.peek().Kind != TFunc && p.peekAnnotation() != TFunc {
					// pub field: pub name: Type [= default]
					field, err := p.parseField()
					if err != nil {
						return nil, err
					}
					field.IsPublic = true
					cls.Fields = append(cls.Fields, *field)
					p.skipNewlines()
					continue
				}
			}
			fn, err := p.parseFunc()
			if err != nil {
				return nil, err
			}
			fn.IsPublic = isPub
			cls.Methods = append(cls.Methods, *fn)
		} else {
			// Must be a field
			field, err := p.parseField()
			if err != nil {
				return nil, err
			}
			cls.Fields = append(cls.Fields, *field)
		}
		p.skipNewlines()
	}

	end, err := p.expect(TRBrace)
	if err != nil {
		return nil, err
	}
	cls.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end.Span.End}
	return cls, nil
}


// parseFunc parses a function/method declaration with annotations.
func (p *Parser) parseFunc() (*ast.FuncDecl, error) {
	start := p.peek().Span.Start
	p.next() // consume 'func'
	name, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	fn := &ast.FuncDecl{Name: name.Text}

	// Check for T.method syntax (multi-class interface methods)
	if p.peek().Kind == TDot {
		p.next() // consume '.'
		methodName, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		fn.ReceiverType = name.Text
		fn.Name = methodName.Text
	}

	// Optional type parameters: func name<T, U: Constraint>(...)
	if p.peek().Kind == TLt {
		params, err := p.parseTypeParams()
		if err != nil {
			return nil, err
		}
		fn.TypeParams = params
	}

	// Parameters
	if _, err := p.expect(TLParen); err != nil {
		return nil, err
	}
	p.skipNewlines()
	for p.peek().Kind != TRParen && p.peek().Kind != TEOF {
		param, err := p.parseParam()
		if err != nil {
			return nil, err
		}
		fn.Params = append(fn.Params, *param)
		if p.peek().Kind == TComma {
			p.next()
		}
		p.skipNewlines()
	}
	if _, err := p.expect(TRParen); err != nil {
		return nil, err
	}

	// Optional return type
	if p.peek().Kind == TArrow {
		p.next()
		ret, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		fn.ReturnType = ret
	}

	// Optional where clause
	p.skipNewlines()
	for p.peek().Kind == TWhere {
		p.next()
		for {
			wc, err := p.parseWhereClause()
			if err != nil {
				return nil, err
			}
			fn.Where = append(fn.Where, *wc)
			if p.peek().Kind == TComma {
				p.next()
			} else {
				break
			}
		}
		p.skipNewlines()
	}

	// Annotations
	fn.Annotations = p.parseAnnotations()

	// Optional body for .fg files
	if p.peek().Kind == TLBrace {
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		fn.Body = body
	}

	fn.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: p.peek().Span.End}
	return fn, nil
}

func (p *Parser) parseWhereClause() (*ast.WhereClause, error) {
	start := p.peek().Span.Start
	name, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}

	// Bare relational constraint: Graph<G, N, E>
	if p.peek().Kind == TLt {
		p.next() // consume '<'
		var typeArgs []ast.TypeExpr
		for p.peek().Kind != TGt && p.peek().Kind != TEOF {
			te, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			typeArgs = append(typeArgs, *te)
			if p.peek().Kind == TComma {
				p.next()
			}
		}
		end, err := p.expect(TGt)
		if err != nil {
			return nil, err
		}
		return &ast.WhereClause{
			Constraint: name.Text,
			TypeArgs:   typeArgs,
			Span:       ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end.Span.End},
		}, nil
	}

	// Single-type constraint: T: Integer
	if _, err := p.expect(TColon); err != nil {
		return nil, err
	}
	constraint, err := p.expect(TIdent)
	if err != nil {
		return nil, err
	}
	return &ast.WhereClause{
		Variable:   name.Text,
		Constraint: constraint.Text,
		Span:       ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: constraint.Span.End},
	}, nil
}

func (p *Parser) parseAnnotations() ast.Annotations {
	var ann ast.Annotations
	for {
		tok := p.peek()
		// Annotation keywords lex as TIdent — rewrite for the switch
		if tok.Kind == TIdent {
			if kind, ok := annotationKeywords[tok.Text]; ok {
				tok.Kind = kind
			}
		}
		switch tok.Kind {
		case TWhy:
			why, _ := p.parseWhy()
			ann.Why = why
		case TConcurrent:
			p.next()
			if _, err := p.expect(TColon); err == nil {
				val := p.next()
				b := val.Text == "true"
				ann.Concurrent = &b
			}
		case TRequiresLock:
			p.next()
			if _, err := p.expect(TLParen); err == nil {
				name, _ := p.expect(TIdent)
				ann.RequiresLock = append(ann.RequiresLock, name.Text)
				p.expect(TRParen)
			}
		case TExcludesLock:
			p.next()
			if _, err := p.expect(TLParen); err == nil {
				name, _ := p.expect(TIdent)
				ann.ExcludesLock = append(ann.ExcludesLock, name.Text)
				p.expect(TRParen)
			}
		case TRaises:
			p.next()
			if _, err := p.expect(TColon); err == nil {
				for {
					name, err := p.expect(TIdent)
					if err != nil {
						break
					}
					ann.Raises = append(ann.Raises, name.Text)
					if p.peek().Kind == TComma {
						p.next()
					} else {
						break
					}
				}
			}
		case TRequires:
			p.next()
			if _, err := p.expect(TColon); err == nil {
				ann.Requires = append(ann.Requires, p.parseExprText())
			}
		case TEnsures:
			p.next()
			if _, err := p.expect(TColon); err == nil {
				ann.Ensures = append(ann.Ensures, p.parseExprText())
			}
		case TSpawns:
			p.next()
			if _, err := p.expect(TColon); err == nil {
				// spawns: has no value — it's just a marker
			}
			ann.Spawns = true
		case TPure:
			p.next()
			if _, err := p.expect(TColon); err == nil {
				// pure: has no value — it's just a marker
			}
			ann.Pure = true
		default:
			return ann
		}
		p.skipNewlines()
	}
}

// parseExprText reads tokens until newline/EOF and returns them as text.
// Used for requires:/ensures: expressions.
func (p *Parser) parseExprText() string {
	var parts []string
	for p.peek().Kind != TNewline && p.peek().Kind != TEOF {
		tok := p.next()
		parts = append(parts, tok.Text)
	}
	return strings.Join(parts, " ")
}

// parseField parses: name: Type [guarded_by(lock)] [// why: "..."]
func (p *Parser) parseField() (*ast.Field, error) {
	start := p.peek().Span.Start
	name, err := p.expectIdentLike()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TColon); err != nil {
		return nil, err
	}
	typ, err := p.parseTypeExpr()
	if err != nil {
		return nil, err
	}
	field := &ast.Field{
		Name: name.Text,
		Type: *typ,
		Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: typ.Span.End},
	}

	// Optional guarded_by
	if p.peekAnnotation() == TGuardedBy {
		p.next()
		if _, err := p.expect(TLParen); err != nil {
			return nil, err
		}
		lock, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		field.GuardedBy = lock.Text
		if _, err := p.expect(TRParen); err != nil {
			return nil, err
		}
	}

	// Optional default value: = expr
	if p.peek().Kind == TAssign {
		p.next() // consume '='
		defExpr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		field.Default = defExpr
	}

	return field, nil
}

// parseParam parses a function parameter: [mut] name: Type or [mut] self
func (p *Parser) parseParam() (*ast.Param, error) {
	start := p.peek().Span.Start
	isMut := false
	if p.peek().Kind == TMut {
		isMut = true
		p.next()
	}

	// Check for self first
	if p.peek().Kind == TSelf {
		tok := p.next()
		return &ast.Param{
			Name:   "self",
			IsMut:  isMut,
			IsSelf: true,
			Span:   ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: tok.Span.End},
		}, nil
	}

	name, err := p.expectIdentLike()
	if err != nil {
		return nil, err
	}

	if name.Text == "self" {
		return &ast.Param{
			Name:   "self",
			IsMut:  isMut,
			IsSelf: true,
			Span:   ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: name.Span.End},
		}, nil
	}

	if _, err := p.expect(TColon); err != nil {
		return nil, err
	}
	typ, err := p.parseTypeExpr()
	if err != nil {
		return nil, err
	}
	return &ast.Param{
		Name:  name.Text,
		Type:  *typ,
		IsMut: isMut,
		Span:  ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: typ.Span.End},
	}, nil
}

// parseTypeExpr parses a type expression.
func (p *Parser) parseTypeExpr() (*ast.TypeExpr, error) {
	start := p.peek().Span.Start
	left, err := p.parseBaseType()
	if err != nil {
		return nil, err
	}

	// Check for optional (?)
	if p.peek().Kind == TQuestion {
		p.next()
		return &ast.TypeExpr{
			Kind: ast.TypeOptional,
			Data: ast.OptionalType{Inner: *left},
			Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: p.peek().Span.End},
		}, nil
	}

	// Check for union (|)
	if p.peek().Kind == TPipe {
		variants := []ast.TypeExpr{*left}
		for p.peek().Kind == TPipe {
			p.next()
			right, err := p.parseBaseType()
			if err != nil {
				return nil, err
			}
			variants = append(variants, *right)
		}
		return &ast.TypeExpr{
			Kind: ast.TypeUnion,
			Data: ast.UnionType{Variants: variants},
			Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: p.peek().Span.End},
		}, nil
	}

	// Check for function type (->)
	// If left is a tuple like (T, U), unwrap fields as params: (T, U) -> V
	// If left is a single type like T, treat as single param: T -> U
	if p.peek().Kind == TArrow {
		p.next()
		ret, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		var params []ast.TypeExpr
		if left.Kind == ast.TypeTuple {
			tt := left.Data.(ast.TupleType)
			for _, f := range tt.Fields {
				params = append(params, f.Type)
			}
		} else {
			params = []ast.TypeExpr{*left}
		}
		return &ast.TypeExpr{
			Kind: ast.TypeFunc,
			Data: ast.FuncType{Params: params, Return: *ret},
			Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: ret.Span.End},
		}, nil
	}

	return left, nil
}

func (p *Parser) parseBaseType() (*ast.TypeExpr, error) {
	start := p.peek().Span.Start
	tok := p.peek()

	// Handle contextual keywords (fn → TFunc)
	if tok.Kind == TIdent {
		if kind, ok := annotationKeywords[tok.Text]; ok {
			tok.Kind = kind
		}
	}

	switch tok.Kind {
	case TFunc:
		// fn(T, U) -> V — function type
		p.next()
		if _, err := p.expect(TLParen); err != nil {
			return nil, err
		}
		var params []ast.TypeExpr
		p.skipNewlines()
		for p.peek().Kind != TRParen && p.peek().Kind != TEOF {
			param, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			params = append(params, *param)
			if p.peek().Kind == TComma {
				p.next()
			}
			p.skipNewlines()
		}
		if _, err := p.expect(TRParen); err != nil {
			return nil, err
		}
		if _, err := p.expect(TArrow); err != nil {
			return nil, err
		}
		ret, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		return &ast.TypeExpr{
			Kind: ast.TypeFunc,
			Data: ast.FuncType{Params: params, Return: *ret},
			Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: ret.Span.End},
		}, nil

	case TLBracket:
		// [T] — sequence
		p.next()
		elem, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		end, err := p.expect(TRBracket)
		if err != nil {
			return nil, err
		}
		return &ast.TypeExpr{
			Kind: ast.TypeSequence,
			Data: ast.SequenceType{Elem: *elem},
			Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end.Span.End},
		}, nil

	case TLParen:
		// Tuple: (T, U) or (x: T, y: U)
		p.next()
		var fields []ast.TupleField
		p.skipNewlines()
		for p.peek().Kind != TRParen && p.peek().Kind != TEOF {
			field, err := p.parseTupleField()
			if err != nil {
				return nil, err
			}
			fields = append(fields, *field)
			if p.peek().Kind == TComma {
				p.next()
			}
			p.skipNewlines()
		}
		end, err := p.expect(TRParen)
		if err != nil {
			return nil, err
		}
		// Empty () in type position is unit, not an empty tuple
		if len(fields) == 0 {
			return &ast.TypeExpr{
				Kind: ast.TypeUnit,
				Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end.Span.End},
			}, nil
		}
		return &ast.TypeExpr{
			Kind: ast.TypeTuple,
			Data: ast.TupleType{Fields: fields},
			Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end.Span.End},
		}, nil

	case TIdent:
		name := p.next()

		// Special case: map[K]V
		if name.Text == "map" && p.peek().Kind == TLBracket {
			p.next() // [
			key, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(TRBracket); err != nil {
				return nil, err
			}
			val, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			return &ast.TypeExpr{
				Kind: ast.TypeMap,
				Data: ast.MapType{Key: *key, Value: *val},
				Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: val.Span.End},
			}, nil
		}

		// Special case: gen T (generator type)
		if name.Text == "gen" {
			elem, err := p.parseBaseType()
			if err != nil {
				return nil, err
			}
			return &ast.TypeExpr{
				Kind: ast.TypeGenerator,
				Data: ast.GeneratorType{Elem: *elem},
				Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: elem.Span.End},
			}, nil
		}

		// Special case: channel<T>
		if name.Text == "channel" && p.peek().Kind == TLt {
			p.next() // <
			elem, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			end, err := p.expect(TGt)
			if err != nil {
				return nil, err
			}
			return &ast.TypeExpr{
				Kind: ast.TypeChannel,
				Data: ast.ChannelType{Elem: *elem},
				Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: end.Span.End},
			}, nil
		}

		// Special case: unit
		if name.Text == "unit" {
			return &ast.TypeExpr{
				Kind: ast.TypeUnit,
				Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: name.Span.End},
			}, nil
		}

		// Named type with optional type args: Foo<T, U>
		// Also handle dotted names: database.ToolUse
		typeName := name.Text
		if p.peek().Kind == TDot {
			p.next()
			sub, err := p.expect(TIdent)
			if err != nil {
				return nil, err
			}
			typeName = typeName + "." + sub.Text
		}

		var args []ast.TypeExpr
		if p.peek().Kind == TLt {
			p.next()
			for p.peek().Kind != TGt && p.peek().Kind != TShr && p.peek().Kind != TEOF {
				arg, err := p.parseTypeExpr()
				if err != nil {
					return nil, err
				}
				args = append(args, *arg)
				if p.peek().Kind == TComma {
					p.next()
				}
			}
			if p.peek().Kind == TShr {
				// >> is two > tokens — consume one, push back the other
				tok := p.next()
				p.pushBack(Token{Kind: TGt, Text: ">", Span: ast.Span{
					Start: ast.Pos{File: tok.Span.Start.File, Line: tok.Span.Start.Line, Column: tok.Span.Start.Column + 1},
					End:   tok.Span.End,
				}})
			} else if _, err := p.expect(TGt); err != nil {
				return nil, err
			}
		}

		return &ast.TypeExpr{
			Kind: ast.TypeNamed,
			Data: ast.NamedType{Name: typeName, Args: args},
			Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: p.peek().Span.End},
		}, nil

	case TLock:
		p.next()
		return &ast.TypeExpr{
			Kind: ast.TypeLock,
			Span: ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: tok.Span.End},
		}, nil

	default:
		return nil, &ParseError{
			Message: fmt.Sprintf("expected type, got %s (%q)", tokenNames[tok.Kind], tok.Text),
			Span:    tok.Span,
		}
	}
}

// parseTypeParams parses: <T> or <T: Comparable> or <T, U> etc.
func (p *Parser) parseTypeParams() ([]ast.TypeParam, error) {
	if _, err := p.expect(TLt); err != nil {
		return nil, err
	}
	var params []ast.TypeParam
	for p.peek().Kind != TGt && p.peek().Kind != TEOF {
		start := p.peek().Span.Start
		name, err := p.expect(TIdent)
		if err != nil {
			return nil, err
		}
		tp := ast.TypeParam{Name: name.Text}
		if p.peek().Kind == TColon {
			p.next()
			constraint, err := p.expect(TIdent)
			if err != nil {
				return nil, err
			}
			tp.Constraint = constraint.Text
		}
		tp.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: p.peek().Span.End}
		params = append(params, tp)
		if p.peek().Kind == TComma {
			p.next()
		}
	}
	if _, err := p.expect(TGt); err != nil {
		return nil, err
	}
	return params, nil
}

// parseRelation parses: relation [hint] Parent[:label] owns|refs [Child[:label]]
func (p *Parser) parseRelation() (*ast.RelationDecl, error) {
	start := p.peek().Span.Start
	p.next() // consume 'relation'

	rel := &ast.RelationDecl{}

	// First token could be hint or parent name
	// If it's a known hint (DoublyLinked, ArrayList, Hashed) or if next-next is a known relation keyword
	first := p.next()

	// Look ahead: if the next meaningful token is owns/refs, first is parent
	// Otherwise first is hint and next is parent
	if p.peek().Kind == TOwns || p.peek().Kind == TRefs || p.peek().Kind == TColon {
		// first is parent
		rel.Parent = p.parseRelationSideFrom(first)
	} else {
		// first is hint
		rel.Hint = first.Text
		parent := p.next()
		rel.Parent = p.parseRelationSideFrom(parent)
	}

	// owns or refs
	kindTok := p.next()
	switch kindTok.Kind {
	case TOwns:
		rel.Kind = ast.Owns
	case TRefs:
		rel.Kind = ast.Refs
	default:
		return nil, &ParseError{
			Message: fmt.Sprintf("expected 'owns' or 'refs', got %s", tokenNames[kindTok.Kind]),
			Span:    kindTok.Span,
		}
	}

	// Child — may be [Child] for one-to-many
	if p.peek().Kind == TLBracket {
		p.next()
		rel.IsMany = true
		child := p.next()
		rel.Child = p.parseRelationSideFrom(child)
		if _, err := p.expect(TRBracket); err != nil {
			return nil, err
		}
	} else {
		child := p.next()
		rel.Child = p.parseRelationSideFrom(child)
	}

	rel.Span = ast.Span{Start: ast.Pos{File: p.lex.filename, Line: start.Line, Column: start.Column}, End: p.peek().Span.End}
	return rel, nil
}

func (p *Parser) parseRelationSideFrom(nameTok Token) ast.RelationSide {
	side := ast.RelationSide{TypeName: nameTok.Text}
	// Collect optional type parameters: <V>, <K, V>, etc.
	if p.peek().Kind == TLt {
		p.next() // consume '<'
		for p.peek().Kind != TGt && p.peek().Kind != TEOF {
			tok := p.next()
			if tok.Kind == TIdent {
				side.TypeArgs = append(side.TypeArgs, tok.Text)
			}
			// skip commas and other tokens
		}
		if p.peek().Kind == TGt {
			p.next() // consume '>'
		}
	}
	// Check for :label — label can be an ident or a contextual keyword used as name
	if p.peek().Kind == TColon {
		p.next()
		tok := p.peek()
		if tok.Kind == TIdent || (tok.Text != "" && tok.Kind != TLBrace && tok.Kind != TRBrace &&
			tok.Kind != TLBracket && tok.Kind != TRBracket && tok.Kind != TEOF && tok.Kind != TNewline) {
			label := p.next()
			side.Label = label.Text
		}
	}
	return side
}

// parseSource parses: source: ["file1.go", "file2.go"]
func (p *Parser) parseSource() ([]string, error) {
	p.next() // consume 'source'
	if _, err := p.expect(TColon); err != nil {
		return nil, err
	}
	if _, err := p.expect(TLBracket); err != nil {
		return nil, err
	}
	var files []string
	for p.peek().Kind != TRBracket && p.peek().Kind != TEOF {
		f, err := p.expect(TStringLit)
		if err != nil {
			return nil, err
		}
		files = append(files, f.Text)
		if p.peek().Kind == TComma {
			p.next()
		}
	}
	if _, err := p.expect(TRBracket); err != nil {
		return nil, err
	}
	return files, nil
}

// parseFake parses: fake: "file.go"
func (p *Parser) parseFake() (string, error) {
	p.next() // consume 'fake'
	if _, err := p.expect(TColon); err != nil {
		return "", err
	}
	tok, err := p.expect(TStringLit)
	if err != nil {
		return "", err
	}
	return tok.Text, nil
}

// parseConstDecl parses a top-level constant: let [mut] NAME: Type = value
func (p *Parser) parseConstDecl() (*ast.ConstDecl, error) {
	start := p.peek().Span.Start
	p.next() // consume 'let'

	// Optional 'mut' modifier
	isMut := false
	if p.peek().Kind == TMut {
		isMut = true
		p.next()
	}

	name, err := p.expectIdentLike()
	if err != nil {
		return nil, err
	}

	decl := &ast.ConstDecl{Name: name.Text, IsMut: isMut}

	// Optional type annotation
	if p.peek().Kind == TColon {
		p.next()
		typ, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		decl.Type = typ
	}

	// Required initializer
	if _, err := p.expect(TAssign); err != nil {
		return nil, err
	}
	val, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	decl.Value = *val
	decl.Span = ast.Span{Start: start, End: p.peek().Span.Start}

	return decl, nil
}
