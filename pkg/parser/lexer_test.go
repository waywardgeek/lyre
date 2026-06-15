package parser

import (
	"testing"
)

func TestLexerKeywords(t *testing.T) {
	input := "lyric func class struct enum interface relation import where owns refs mut self"
	lex := NewLexer(input, "test.lyric")

	expected := []TokenKind{
		TLyric, TFunc, TClass, TStruct, TEnum, TInterface,
		TRelation, TImport, TWhere, TOwns, TRefs, TMut, TSelf,
		TEOF,
	}

	for _, want := range expected {
		got := lex.Next()
		if got.Kind != want {
			t.Errorf("expected %v, got %v (%q)", tokenNames[want], tokenNames[got.Kind], got.Text)
		}
	}
}

func TestLexerAnnotations(t *testing.T) {
	// Annotation keywords are now contextual — they lex as TIdent
	// and the parser resolves them via peekAnnotation()
	input := "concurrent requires ensures raises requires_lock excludes_lock guarded_by spawns pure source fake verified_at"
	lex := NewLexer(input, "test.lyric")

	expectedTexts := []string{
		"concurrent", "requires", "ensures", "raises",
		"requires_lock", "excludes_lock", "guarded_by",
		"spawns", "pure", "source", "fake", "verified_at",
	}

	for _, want := range expectedTexts {
		got := lex.Next()
		if got.Kind != TIdent || got.Text != want {
			t.Errorf("expected ident %q, got %v %q", want, tokenNames[got.Kind], got.Text)
		}
		// Verify it maps back to an annotation keyword
		if _, ok := annotationKeywords[got.Text]; !ok {
			t.Errorf("%q not found in annotationKeywords map", got.Text)
		}
	}
	eof := lex.Next()
	if eof.Kind != TEOF {
		t.Errorf("expected EOF, got %v", tokenNames[eof.Kind])
	}
}

func TestLexerPunctuation(t *testing.T) {
	input := "( ) { } [ ] , : . | ? < > -> =>"
	lex := NewLexer(input, "test.lyric")

	expected := []TokenKind{
		TLParen, TRParen, TLBrace, TRBrace, TLBracket, TRBracket,
		TComma, TColon, TDot, TPipe, TQuestion, TLt, TGt,
		TArrow, TFatArrow, TEOF,
	}

	for _, want := range expected {
		got := lex.Next()
		if got.Kind != want {
			t.Errorf("expected %v, got %v (%q)", tokenNames[want], tokenNames[got.Kind], got.Text)
		}
	}
}

func TestLexerStrings(t *testing.T) {
	input := `"hello" "with \"escape\""`
	lex := NewLexer(input, "test.lyric")

	tok := lex.Next()
	if tok.Kind != TStringLit || tok.Text != "hello" {
		t.Errorf("expected string 'hello', got %v %q", tokenNames[tok.Kind], tok.Text)
	}

	tok = lex.Next()
	if tok.Kind != TStringLit || tok.Text != `with "escape"` {
		t.Errorf("expected string with escapes, got %v %q", tokenNames[tok.Kind], tok.Text)
	}
}

func TestLexerTripleString(t *testing.T) {
	input := `doc "Arch": """
  This is a triple-quoted string.
  It spans multiple lines.
"""`
	lex := NewLexer(input, "test.lyric")

	tok := lex.Next() // doc (now lexes as TIdent since annotation keywords are contextual)
	if tok.Kind != TIdent || tok.Text != "doc" {
		t.Errorf("expected ident 'doc', got %v %q", tokenNames[tok.Kind], tok.Text)
	}

	tok = lex.Next() // "Arch"
	if tok.Kind != TStringLit || tok.Text != "Arch" {
		t.Errorf("expected string 'Arch', got %v %q", tokenNames[tok.Kind], tok.Text)
	}

	tok = lex.Next() // :
	if tok.Kind != TColon {
		t.Errorf("expected colon, got %v", tokenNames[tok.Kind])
	}

	tok = lex.Next() // """..."""
	if tok.Kind != TTripleStringLit {
		t.Errorf("expected triple string, got %v %q", tokenNames[tok.Kind], tok.Text)
	}
	if tok.Text != "This is a triple-quoted string.\n  It spans multiple lines." {
		t.Errorf("unexpected triple string content: %q", tok.Text)
	}
}

func TestLexerNumbers(t *testing.T) {
	input := "42 3.14 1_000_000"
	lex := NewLexer(input, "test.lyric")

	tok := lex.Next()
	if tok.Kind != TIntLit || tok.Text != "42" {
		t.Errorf("expected int 42, got %v %q", tokenNames[tok.Kind], tok.Text)
	}

	tok = lex.Next()
	if tok.Kind != TFloatLit || tok.Text != "3.14" {
		t.Errorf("expected float 3.14, got %v %q", tokenNames[tok.Kind], tok.Text)
	}

	tok = lex.Next()
	if tok.Kind != TIntLit || tok.Text != "1000000" {
		t.Errorf("expected int 1000000, got %v %q", tokenNames[tok.Kind], tok.Text)
	}
}

func TestLexerComments(t *testing.T) {
	input := `func foo // this is a comment
class bar`
	lex := NewLexer(input, "test.lyric")

	tok := lex.Next()
	if tok.Kind != TFunc {
		t.Errorf("expected func, got %v", tokenNames[tok.Kind])
	}

	tok = lex.Next()
	if tok.Kind != TIdent || tok.Text != "foo" {
		t.Errorf("expected ident foo, got %v %q", tokenNames[tok.Kind], tok.Text)
	}

	tok = lex.Next() // newline
	if tok.Kind != TNewline {
		t.Errorf("expected newline, got %v", tokenNames[tok.Kind])
	}

	tok = lex.Next()
	if tok.Kind != TClass {
		t.Errorf("expected class, got %v", tokenNames[tok.Kind])
	}
}

func TestLexerPositions(t *testing.T) {
	input := "func\nclass"
	lex := NewLexer(input, "test.lyric")

	tok := lex.Next()
	if tok.Span.Start.Line != 1 || tok.Span.Start.Column != 1 {
		t.Errorf("expected line 1 col 1, got line %d col %d", tok.Span.Start.Line, tok.Span.Start.Column)
	}

	lex.Next() // newline

	tok = lex.Next()
	if tok.Span.Start.Line != 2 || tok.Span.Start.Column != 1 {
		t.Errorf("expected line 2 col 1, got line %d col %d", tok.Span.Start.Line, tok.Span.Start.Column)
	}
}

func TestLexerPeek(t *testing.T) {
	input := "func class"
	lex := NewLexer(input, "test.lyric")

	peeked := lex.Peek()
	if peeked.Kind != TFunc {
		t.Errorf("peek expected func, got %v", tokenNames[peeked.Kind])
	}

	// Peek again should return same token
	peeked2 := lex.Peek()
	if peeked2.Kind != TFunc {
		t.Errorf("second peek expected func, got %v", tokenNames[peeked2.Kind])
	}

	// Next should consume it
	tok := lex.Next()
	if tok.Kind != TFunc {
		t.Errorf("next expected func, got %v", tokenNames[tok.Kind])
	}

	// Next token should be class
	tok = lex.Next()
	if tok.Kind != TClass {
		t.Errorf("next expected class, got %v", tokenNames[tok.Kind])
	}
}

func TestLexerForgeSnippet(t *testing.T) {
	input := `lyric Parser {
  why: "PEG parser for .lyric files."

  struct Pos {
    file: string
    line: u32
  }

  func parse(mut self) -> (File, error)
    raises: ParseError
}`
	lex := NewLexer(input, "test.lyric")

	// Just verify it tokenizes without panicking and produces the expected stream
	var tokens []Token
	for {
		tok := lex.Next()
		tokens = append(tokens, tok)
		if tok.Kind == TEOF {
			break
		}
	}

	// Verify first few tokens: forge Ident LBrace
	if tokens[0].Kind != TLyric {
		t.Errorf("expected forge, got %v", tokenNames[tokens[0].Kind])
	}
	if tokens[1].Kind != TIdent || tokens[1].Text != "Parser" {
		t.Errorf("expected ident Parser, got %v %q", tokenNames[tokens[1].Kind], tokens[1].Text)
	}
	if tokens[2].Kind != TLBrace {
		t.Errorf("expected {, got %v", tokenNames[tokens[2].Kind])
	}
}
