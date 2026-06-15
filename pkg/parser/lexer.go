// Package parser implements the Forge lexer and PEG parser.
package parser

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/waywardgeek/lyre/pkg/ast"
)

// TokenKind identifies the type of a lexer token.
type TokenKind int

const (
	// Keywords
	TLyric TokenKind = iota
	TFunc
	TClass
	TStruct
	TEnum
	TInterface
	TRelation
	TField
	TDestructor
	TEmbed
	TImport
	TImplements
	TImpl
	TAs
	TIs
	TType
	TWhere
	TOwns
	TRefs
	TMut
	TSelf
	TFrom
	TTrue
	TFalse
	TNil

	// .fg keywords
	TLet
	TIf
	TElse
	TFor
	TIn
	TWhile
	TMatch
	TReturn
	TBreak
	TContinue
	TCascadeKw // cascade (keyword, not annotation)
	TSpawn     // spawn
	TSelect    // select
	TCase      // case
	TLock      // lock (statement keyword)
	TPub       // pub (visibility modifier)
	TYield     // yield

	// Literals
	TIdent
	TIntLit
	TFloatLit
	TStringLit
	TTripleStringLit // """..."""
	TFStringLit      // f"...{expr}..." — interpolated string (raw content stored in Text)
	TCharLit         // 'c' — character literal (u8 value)
	TBacktickSym     // `name` — symbol literal, desugars to sym("name")

	// Punctuation
	TLParen    // (
	TRParen    // )
	TLBrace    // {
	TRBrace    // }
	TLBracket  // [
	TRBracket  // ]
	TComma     // ,
	TColon     // :
	TDot       // .
	TArrow     // ->
	TFatArrow  // =>
	TBiArrow   // <->
	TPipe      // |
	TQuestion  // ?
	TLt        // <
	TGt        // >

	// Operators (.fg)
	TAssign    // =
	TPlus      // +
	TMinus     // -
	TStar      // *
	TSlash     // /
	TPercent   // %
	TEqEq      // ==
	TBangEq    // !=
	TLtEq      // <=
	TGtEq      // >=
	TAmpAmp    // &&
	TPipePipe  // ||
	TBang      // !
	TAmp       // &
	TCaret     // ^
	TShl       // <<
	TShr       // >>
	TPlusEq    // +=
	TMinusEq   // -=
	TStarEq    // *=
	TSlashEq   // /=

	// Annotations (contextual keywords)
	TWhy
	TDoc
	TInvariant
	TRequires
	TEnsures
	TRaises
	TConcurrent
	TRequiresLock
	TExcludesLock
	TGuardedBy
	TSpawns
	TPure
	TSource
	TFake
	TVerifiedAt

	// Special
	TNewline
	TEOF
)

var keywords = map[string]TokenKind{
	"lyric":       TLyric,
	"func":       TFunc,
	"class":      TClass,
	"struct":     TStruct,
	"enum":       TEnum,
	"interface":  TInterface,
	"relation":   TRelation,
	// "field" is contextual — moved to annotationKeywords
	"destructor": TDestructor,
	"embed":      TEmbed,
	"import":     TImport,
	// "implements" is contextual — moved to annotationKeywords
	"impl":       TImpl,
	"as":         TAs,
	"is":         TIs,
	"type":       TType,
	"where":      TWhere,
	"owns":       TOwns,
	"refs":       TRefs,
	"mut":        TMut,
	"self":       TSelf,
	"from":       TFrom,
	"true":       TTrue,
	"false":      TFalse,
	"nil":        TNil,
	"null":       TNil,
	"let":        TLet,
	"if":         TIf,
	"else":       TElse,
	"for":        TFor,
	"in":         TIn,
	"while":      TWhile,
	"match":      TMatch,
	"return":     TReturn,
	"break":      TBreak,
	"continue":   TContinue,
	"cascade":    TCascadeKw,
	"spawn":      TSpawn,
	"select":     TSelect,
	"case":       TCase,
	// "lock" and "implements" are contextual — moved to annotationKeywords
	"yield":      TYield,
	"pub":        TPub,
}

// Annotation keywords are only recognized in annotation position (after newline + indent).
// annotationKeywords maps annotation names to token kinds.
// These are NOT lexed as keywords — the parser checks for them contextually
// via peekAnnotation(), so they can be used as variable names.
var annotationKeywords = map[string]TokenKind{
	"field":         TField,
	"lock":          TLock,
	"implements":    TImplements,
	"why":           TWhy,
	"doc":           TDoc,
	"invariant":     TInvariant,
	"requires":      TRequires,
	"ensures":       TEnsures,
	"raises":        TRaises,
	"concurrent":    TConcurrent,
	"requires_lock": TRequiresLock,
	"excludes_lock": TExcludesLock,
	"guarded_by":    TGuardedBy,
	"spawns":        TSpawns,
	"pure":          TPure,
	"source":        TSource,
	"fake":          TFake,
	"verified_at":   TVerifiedAt,
}

// Token is a single lexer token.
type Token struct {
	Kind TokenKind
	Text string
	Span ast.Span
}

func (t Token) String() string {
	return fmt.Sprintf("%s(%q)", tokenNames[t.Kind], t.Text)
}

var tokenNames = map[TokenKind]string{
	TLyric: "lyric", TFunc: "func", TClass: "class", TStruct: "struct",
	TEnum: "enum", TInterface: "interface", TRelation: "relation", TField: "field",
	TDestructor: "destructor", TEmbed: "embed",
	TImport: "import", TImplements: "implements", TImpl: "impl", TAs: "as", TIs: "is", TWhere: "where",
	TOwns: "owns", TRefs: "refs", TMut: "mut", TSelf: "self",
	TFrom: "from", TTrue: "true", TFalse: "false", TNil: "nil",
	TLet: "let", TIf: "if", TElse: "else", TFor: "for", TIn: "in",
	TWhile: "while", TMatch: "match", TReturn: "return",
	TBreak: "break", TContinue: "continue", TCascadeKw: "cascade",
	TSpawn: "spawn", TSelect: "select", TCase: "case", TLock: "lock", TPub: "pub", TYield: "yield",
	TIdent: "ident", TIntLit: "int", TFloatLit: "float",
	TStringLit: "string", TTripleStringLit: "triple_string", TFStringLit: "fstring", TCharLit: "char", TBacktickSym: "backtick_sym",
	TLParen: "(", TRParen: ")", TLBrace: "{", TRBrace: "}",
	TLBracket: "[", TRBracket: "]", TComma: ",", TColon: ":",
	TDot: ".", TArrow: "->", TFatArrow: "=>", TBiArrow: "<->", TPipe: "|",
	TQuestion: "?", TLt: "<", TGt: ">",
	TAssign: "=", TPlus: "+", TMinus: "-", TStar: "*", TSlash: "/",
	TPercent: "%", TEqEq: "==", TBangEq: "!=", TLtEq: "<=", TGtEq: ">=",
	TAmpAmp: "&&", TPipePipe: "||", TBang: "!", TAmp: "&", TCaret: "^",
	TShl: "<<", TShr: ">>", TPlusEq: "+=", TMinusEq: "-=",
	TStarEq: "*=", TSlashEq: "/=",
	TWhy: "why", TDoc: "doc", TInvariant: "invariant",
	TRequires: "requires", TEnsures: "ensures", TRaises: "raises",
	TConcurrent: "concurrent", TRequiresLock: "requires_lock",
	TExcludesLock: "excludes_lock", TGuardedBy: "guarded_by",
	TSpawns: "spawns", TPure: "pure", TSource: "source", TFake: "fake",
	TVerifiedAt: "verified_at",
	TNewline: "newline", TEOF: "EOF",
}

// Lexer tokenizes Forge source code.
type Lexer struct {
	source       string
	filename     string
	pos          int
	line         int
	column       int
	peeked       *Token
	bracketDepth int           // tracks (), [], {} nesting — newlines suppressed when > 0
	Comments     []ast.Comment // collected during scanning
}

// NewLexer creates a new lexer for the given source.
func NewLexer(source, filename string) *Lexer {
	return &Lexer{
		source:   source,
		filename: filename,
		line:     1,
		column:   1,
	}
}

func (l *Lexer) currentPos() ast.Pos {
	return ast.Pos{File: l.filename, Line: l.line, Column: l.column}
}

func (l *Lexer) advance() rune {
	if l.pos >= len(l.source) {
		return 0
	}
	r, size := utf8.DecodeRuneInString(l.source[l.pos:])
	l.pos += size
	if r == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
	}
	return r
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.source) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.source[l.pos:])
	return r
}

func (l *Lexer) peekAt(offset int) rune {
	p := l.pos + offset
	if p >= len(l.source) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.source[p:])
	return r
}

// Peek returns the next token without consuming it.
func (l *Lexer) Peek() Token {
	if l.peeked != nil {
		return *l.peeked
	}
	tok := l.Next()
	l.peeked = &tok
	return tok
}

// Next returns the next token.
func (l *Lexer) Next() Token {
	if l.peeked != nil {
		tok := *l.peeked
		l.peeked = nil
		return tok
	}
	return l.scan()
}

func (l *Lexer) scan() Token {
	// Skip whitespace (not newlines — they're significant)
	for l.pos < len(l.source) {
		r := l.peek()
		if r == ' ' || r == '\t' || r == '\r' {
			l.advance()
		} else if r == '/' && l.peekAt(1) == '/' {
			// Line comment — collect and skip to end of line
			commentPos := l.currentPos()
			var buf strings.Builder
			for l.pos < len(l.source) && l.peek() != '\n' {
				buf.WriteRune(l.advance())
			}
			l.Comments = append(l.Comments, ast.Comment{
				Text: buf.String(),
				Pos:  commentPos,
			})
		} else {
			break
		}
	}

	if l.pos >= len(l.source) {
		return Token{Kind: TEOF, Span: ast.Span{Start: l.currentPos(), End: l.currentPos()}}
	}

	start := l.currentPos()
	r := l.peek()

	// Newline — suppress inside brackets
	if r == '\n' {
		l.advance()
		// Collapse multiple newlines
		for l.pos < len(l.source) && (l.peek() == '\n' || l.peek() == ' ' || l.peek() == '\t' || l.peek() == '\r') {
			if l.peek() == '\n' {
				l.advance()
			} else {
				// Only skip whitespace if followed by another newline
				saved := l.pos
				for l.pos < len(l.source) && (l.peek() == ' ' || l.peek() == '\t' || l.peek() == '\r') {
					l.advance()
				}
				if l.pos < len(l.source) && l.peek() == '\n' {
					l.advance()
				} else {
					l.pos = saved
					break
				}
			}
		}
		if l.bracketDepth > 0 {
			return l.scan() // skip newline inside brackets
		}
		return Token{Kind: TNewline, Text: "\n", Span: ast.Span{Start: start, End: l.currentPos()}}
	}

	// String literal
	if r == '"' {
		return l.scanString(start)
	}

	// Character literal
	if r == '\'' {
		return l.scanCharLit(start)
	}

	// Backtick symbol literal: `name` → sym("name")
	if r == '`' {
		return l.scanBacktickSym(start)
	}

	// Number
	if r >= '0' && r <= '9' {
		return l.scanNumber(start)
	}

	// Identifier or keyword
	if r == '_' || unicode.IsLetter(r) {
		return l.scanIdent(start)
	}

	// Punctuation
	l.advance()
	switch r {
	case '(':
		l.bracketDepth++
		return Token{Kind: TLParen, Text: "(", Span: ast.Span{Start: start, End: l.currentPos()}}
	case ')':
		l.bracketDepth--
		return Token{Kind: TRParen, Text: ")", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '{':
		return Token{Kind: TLBrace, Text: "{", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '}':
		return Token{Kind: TRBrace, Text: "}", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '[':
		l.bracketDepth++
		return Token{Kind: TLBracket, Text: "[", Span: ast.Span{Start: start, End: l.currentPos()}}
	case ']':
		l.bracketDepth--
		return Token{Kind: TRBracket, Text: "]", Span: ast.Span{Start: start, End: l.currentPos()}}
	case ',':
		return Token{Kind: TComma, Text: ",", Span: ast.Span{Start: start, End: l.currentPos()}}
	case ':':
		return Token{Kind: TColon, Text: ":", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '.':
		return Token{Kind: TDot, Text: ".", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '?':
		return Token{Kind: TQuestion, Text: "?", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '^':
		return Token{Kind: TCaret, Text: "^", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '%':
		return Token{Kind: TPercent, Text: "%", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '!':
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: TBangEq, Text: "!=", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		return Token{Kind: TBang, Text: "!", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '|':
		if l.peek() == '|' {
			l.advance()
			return Token{Kind: TPipePipe, Text: "||", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		return Token{Kind: TPipe, Text: "|", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '&':
		if l.peek() == '&' {
			l.advance()
			return Token{Kind: TAmpAmp, Text: "&&", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		return Token{Kind: TAmp, Text: "&", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '<':
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: TLtEq, Text: "<=", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		if l.peek() == '<' {
			l.advance()
			return Token{Kind: TShl, Text: "<<", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		if l.peek() == '-' && l.peekAt(1) == '>' {
			l.advance()
			l.advance()
			return Token{Kind: TBiArrow, Text: "<->", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		return Token{Kind: TLt, Text: "<", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '>':
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: TGtEq, Text: ">=", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		if l.peek() == '>' {
			l.advance()
			return Token{Kind: TShr, Text: ">>", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		return Token{Kind: TGt, Text: ">", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '-':
		if l.peek() == '>' {
			l.advance()
			return Token{Kind: TArrow, Text: "->", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: TMinusEq, Text: "-=", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		return Token{Kind: TMinus, Text: "-", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '+':
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: TPlusEq, Text: "+=", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		return Token{Kind: TPlus, Text: "+", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '*':
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: TStarEq, Text: "*=", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		return Token{Kind: TStar, Text: "*", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '/':
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: TSlashEq, Text: "/=", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		return Token{Kind: TSlash, Text: "/", Span: ast.Span{Start: start, End: l.currentPos()}}
	case '=':
		if l.peek() == '=' {
			l.advance()
			return Token{Kind: TEqEq, Text: "==", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		if l.peek() == '>' {
			l.advance()
			return Token{Kind: TFatArrow, Text: "=>", Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		return Token{Kind: TAssign, Text: "=", Span: ast.Span{Start: start, End: l.currentPos()}}
	}

	return Token{Kind: TIdent, Text: string(r), Span: ast.Span{Start: start, End: l.currentPos()}}
}

// scanEscape handles a backslash escape sequence, writing the result to buf.
// The backslash has already been consumed. Returns false on invalid escape.
func (l *Lexer) scanEscape(buf *strings.Builder) bool {
	next := l.advance()
	switch next {
	case 'n':
		buf.WriteByte('\n')
	case 'r':
		buf.WriteByte('\r')
	case 't':
		buf.WriteByte('\t')
	case '\\':
		buf.WriteByte('\\')
	case '\'':
		buf.WriteByte('\'')
	case '"':
		buf.WriteByte('"')
	case '0':
		buf.WriteByte(0)
	case 'x':
		// Two hex digits
		hi := l.advance()
		lo := l.advance()
		val := hexVal(hi)<<4 | hexVal(lo)
		buf.WriteByte(byte(val))
	default:
		buf.WriteByte('\\')
		buf.WriteRune(next)
		return false
	}
	return true
}

func hexVal(r rune) byte {
	switch {
	case r >= '0' && r <= '9':
		return byte(r - '0')
	case r >= 'a' && r <= 'f':
		return byte(r - 'a' + 10)
	case r >= 'A' && r <= 'F':
		return byte(r - 'A' + 10)
	default:
		return 0
	}
}

func (l *Lexer) scanCharLit(start ast.Pos) Token {
	l.advance() // opening '
	var buf strings.Builder
	r := l.peek()
	if r == '\\' {
		l.advance()
		l.scanEscape(&buf)
	} else {
		buf.WriteRune(l.advance())
	}
	if l.peek() == '\'' {
		l.advance() // closing '
	}
	// Store the byte value as the text
	return Token{Kind: TCharLit, Text: buf.String(), Span: ast.Span{Start: start, End: l.currentPos()}}
}

func (l *Lexer) scanBacktickSym(start ast.Pos) Token {
	l.advance() // opening `
	var buf strings.Builder
	for l.pos < len(l.source) {
		r := l.peek()
		if r == '`' {
			l.advance() // closing `
			break
		}
		if r == '\n' || r == 0 {
			break // unterminated
		}
		buf.WriteRune(l.advance())
	}
	return Token{Kind: TBacktickSym, Text: buf.String(), Span: ast.Span{Start: start, End: l.currentPos()}}
}

func (l *Lexer) scanString(start ast.Pos) Token {
	l.advance() // opening "

	// Check for triple-quoted string
	if l.peek() == '"' && l.peekAt(1) == '"' {
		l.advance() // second "
		l.advance() // third "
		return l.scanTripleString(start)
	}

	var buf strings.Builder
	for l.pos < len(l.source) {
		r := l.advance()
		if r == '"' {
			return Token{Kind: TStringLit, Text: buf.String(), Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		if r == '\\' {
			l.scanEscape(&buf)
		} else {
			buf.WriteRune(r)
		}
	}
	// Unterminated string
	return Token{Kind: TStringLit, Text: buf.String(), Span: ast.Span{Start: start, End: l.currentPos()}}
}

// scanFString scans f"..." and stores the raw content between quotes.
// The parser will split on {expr} boundaries for interpolation.
func (l *Lexer) scanFString(start ast.Pos) Token {
	l.advance() // opening "
	var buf strings.Builder
	depth := 0
	for l.pos < len(l.source) {
		r := l.advance()
		if r == '{' {
			// {{ is an escaped literal brace — store as sentinel \x01
			if depth == 0 && l.pos < len(l.source) && l.peek() == '{' {
				l.advance() // consume second {
				buf.WriteByte(0x01)
			} else {
				depth++
				buf.WriteRune(r)
			}
		} else if r == '}' {
			// }} is an escaped literal brace — store as sentinel \x02
			if depth == 0 && l.pos < len(l.source) && l.peek() == '}' {
				l.advance() // consume second }
				buf.WriteByte(0x02)
			} else {
				depth--
				buf.WriteRune(r)
			}
		} else if r == '"' && depth == 0 {
			return Token{Kind: TFStringLit, Text: buf.String(), Span: ast.Span{Start: start, End: l.currentPos()}}
		} else if r == '\\' && depth == 0 {
			// Check for escaped braces first
			if l.peek() == '{' {
				buf.WriteByte(0x01) // sentinel for literal {
				l.advance()
			} else if l.peek() == '}' {
				buf.WriteByte(0x02) // sentinel for literal }
				l.advance()
			} else {
				l.scanEscape(&buf)
			}
		} else {
			buf.WriteRune(r)
		}
	}
	return Token{Kind: TFStringLit, Text: buf.String(), Span: ast.Span{Start: start, End: l.currentPos()}}
}

func (l *Lexer) scanTripleString(start ast.Pos) Token {
	var buf strings.Builder
	for l.pos < len(l.source) {
		r := l.advance()
		if r == '"' && l.peek() == '"' && l.peekAt(1) == '"' {
			l.advance()
			l.advance()
			// Trim leading/trailing whitespace lines
			text := strings.TrimSpace(buf.String())
			return Token{Kind: TTripleStringLit, Text: text, Span: ast.Span{Start: start, End: l.currentPos()}}
		}
		buf.WriteRune(r)
	}
	return Token{Kind: TTripleStringLit, Text: buf.String(), Span: ast.Span{Start: start, End: l.currentPos()}}
}

func (l *Lexer) scanNumber(start ast.Pos) Token {
	var buf strings.Builder
	isFloat := false
	for l.pos < len(l.source) {
		r := l.peek()
		if r >= '0' && r <= '9' {
			buf.WriteRune(r)
			l.advance()
		} else if r == '.' && !isFloat {
			// Check it's not a method call
			if next := l.peekAt(1); next >= '0' && next <= '9' {
				isFloat = true
				buf.WriteRune(r)
				l.advance()
			} else {
				break
			}
		} else if r == '_' {
			l.advance() // skip underscores in number literals
		} else {
			break
		}
	}
	kind := TIntLit
	if isFloat {
		kind = TFloatLit
	}
	return Token{Kind: kind, Text: buf.String(), Span: ast.Span{Start: start, End: l.currentPos()}}
}

func (l *Lexer) scanIdent(start ast.Pos) Token {
	var buf strings.Builder
	for l.pos < len(l.source) {
		r := l.peek()
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf.WriteRune(r)
			l.advance()
		} else {
			break
		}
	}
	text := buf.String()

	// f-string: f"..." triggers interpolated string scanning
	if text == "f" && l.pos < len(l.source) && l.peek() == '"' {
		return l.scanFString(start)
	}

	// Check keywords first
	if kind, ok := keywords[text]; ok {
		return Token{Kind: kind, Text: text, Span: ast.Span{Start: start, End: l.currentPos()}}
	}

	// Annotation keywords are NOT lexed — they stay as TIdent.
	// The parser handles them contextually via peekAnnotation().

	return Token{Kind: TIdent, Text: text, Span: ast.Span{Start: start, End: l.currentPos()}}
}
