package udd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/waywardgeek/lyre/pkg/extract"
)

// Parse reads a `.lyric` v2 source text and returns the corresponding
// PackageInfo. `filename` is used for error messages and (post-parse) for
// language detection by the caller; it does not affect parsing.
func Parse(text, filename string) (*extract.PackageInfo, error) {
	// Normalize CRLF to LF (spec §2).
	text = strings.ReplaceAll(text, "\r\n", "\n")

	p := &parser{
		file:    filename,
		rawLines: strings.Split(text, "\n"),
	}
	// strings.Split on a string ending with "\n" leaves a trailing "" — drop it.
	if n := len(p.rawLines); n > 0 && p.rawLines[n-1] == "" {
		p.rawLines = p.rawLines[:n-1]
	}
	return p.parseFile()
}

// parser holds parsing state for a single .lyric file.
type parser struct {
	file     string
	rawLines []string // 0-indexed; lineNum = pos+1
	pos      int
}

// --- low-level cursor helpers ----------------------------------------------

// errf returns a formatted error at the current 1-based line number.
func (p *parser) errf(line int, format string, args ...interface{}) error {
	return fmt.Errorf("%s:%d: %s", p.file, line, fmt.Sprintf(format, args...))
}

// classifyIndent returns the indent level (in 2-space units) and the content
// (line with leading whitespace stripped). It errors on tabs in indentation
// and on odd numbers of leading spaces.
func (p *parser) classifyIndent(lineIdx int) (indent int, content string, err error) {
	raw := p.rawLines[lineIdx]
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c == '\t' {
			return 0, "", p.errf(lineIdx+1, "tab in indentation (use spaces only)")
		}
		if c != ' ' {
			break
		}
		i++
	}
	if i%2 != 0 {
		return 0, "", p.errf(lineIdx+1, "odd number of leading spaces (indent must be 2 spaces per level)")
	}
	return i / 2, raw[i:], nil
}

// isBlankOrComment reports whether content (post-indent-strip) is empty,
// whitespace-only, or a `#`-comment line.
func isBlankOrComment(content string) bool {
	t := strings.TrimSpace(content)
	return t == "" || strings.HasPrefix(t, "#")
}

// peekStructural advances the cursor past blank/comment lines, then returns
// the indent and content of the next structural line WITHOUT consuming it.
// Returns ok=false at EOF.
func (p *parser) peekStructural() (indent int, content string, lineNum int, ok bool, err error) {
	for p.pos < len(p.rawLines) {
		ind, c, e := p.classifyIndent(p.pos)
		if e != nil {
			return 0, "", 0, false, e
		}
		if isBlankOrComment(c) {
			p.pos++
			continue
		}
		return ind, c, p.pos + 1, true, nil
	}
	return 0, "", 0, false, nil
}

// consumeStructural is peekStructural that also advances past the returned line.
func (p *parser) consumeStructural() (indent int, content string, lineNum int, ok bool, err error) {
	indent, content, lineNum, ok, err = p.peekStructural()
	if ok {
		p.pos++
	}
	return
}

// --- quoted-string parsing -------------------------------------------------

// parseQuoted parses a `"..."` token at the start of `s` and returns the
// unescaped value plus the remainder (after the closing quote). Per spec
// §12 decision 5: only `\"` and `\\` are recognized as escapes.
func parseQuoted(s string, lineNum int, file string) (val, rest string, err error) {
	if len(s) == 0 || s[0] != '"' {
		return "", "", fmt.Errorf("%s:%d: expected quoted string, got %q", file, lineNum, s)
	}
	var sb strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		if c == '\\' {
			if i+1 >= len(s) {
				return "", "", fmt.Errorf("%s:%d: dangling backslash in string", file, lineNum)
			}
			n := s[i+1]
			if n != '"' && n != '\\' {
				return "", "", fmt.Errorf("%s:%d: unrecognized escape \\%c (only \\\" and \\\\ allowed)", file, lineNum, n)
			}
			sb.WriteByte(n)
			i += 2
			continue
		}
		if c == '"' {
			return sb.String(), s[i+1:], nil
		}
		sb.WriteByte(c)
		i++
	}
	return "", "", fmt.Errorf("%s:%d: unterminated quoted string", file, lineNum)
}

// --- top-level grammar -----------------------------------------------------

func (p *parser) parseFile() (*extract.PackageInfo, error) {
	ind, content, lineNum, ok, err := p.consumeStructural()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%s: empty file (expected `module <name>`)", p.file)
	}
	if ind != 0 {
		return nil, p.errf(lineNum, "module head must be at indent 0, got indent %d", ind)
	}
	if !strings.HasPrefix(content, "module ") {
		return nil, p.errf(lineNum, "expected `module <name>`, got %q", content)
	}
	name := strings.TrimSpace(content[len("module "):])
	if !isIdent(name) {
		return nil, p.errf(lineNum, "invalid module name %q", name)
	}
	pkg := extract.NewPackageInfo(name)

	if err := p.parseModuleBody(pkg); err != nil {
		return nil, err
	}

	// Any trailing structural content is an error.
	if ind, _, lineNum, ok, err := p.peekStructural(); err != nil {
		return nil, err
	} else if ok {
		return nil, p.errf(lineNum, "unexpected content at indent %d after module body ends", ind)
	}
	return pkg, nil
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if i == 0 {
			if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				return false
			}
		} else {
			if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
				return false
			}
		}
	}
	return true
}

// parseModuleBody consumes lines at indent 1 belonging to the module body.
func (p *parser) parseModuleBody(pkg *extract.PackageInfo) error {
	for {
		ind, content, lineNum, ok, err := p.peekStructural()
		if err != nil {
			return err
		}
		if !ok || ind < 1 {
			return nil
		}
		if ind > 1 {
			return p.errf(lineNum, "unexpected indent %d in module body (want 1)", ind)
		}
		tok := firstToken(content)
		switch tok {
		case "source:":
			p.pos++
			val, err := parseModuleSourceList(content[len("source:"):], lineNum, p.file)
			if err != nil {
				return err
			}
			pkg.ModuleSource = val
		case "why:":
			p.pos++
			val, err := parseInlineQuoted(content[len("why:"):], lineNum, p.file, "why:")
			if err != nil {
				return err
			}
			pkg.ModuleWhy = val
		case "doc":
			title, body, err := p.parseDocBlock(1)
			if err != nil {
				return err
			}
			pkg.Docs = append(pkg.Docs, extract.DocBlock{Title: title, Content: body})
		case "invariant":
			inv, err := p.parseInvariantBlock(1)
			if err != nil {
				return err
			}
			pkg.Invariants = append(pkg.Invariants, inv)
		case "class", "struct", "enum":
			if err := p.parseStructDecl(1, tok, pkg); err != nil {
				return err
			}
		case "interface":
			if err := p.parseInterfaceDecl(1, pkg); err != nil {
				return err
			}
		case "func":
			if err := p.parseFuncDecl(1, pkg); err != nil {
				return err
			}
		case "typedef":
			if err := p.parseTypedefDecl(1, pkg); err != nil {
				return err
			}
		default:
			return p.errf(lineNum, "unrecognized block head or key %q in module body", tok)
		}
	}
}

// firstToken returns the leading space-delimited token of content.
func firstToken(content string) string {
	i := 0
	for i < len(content) && content[i] != ' ' {
		i++
	}
	return content[:i]
}

// parseModuleSourceList parses `[ "a", "b" ]` after the `source:` key.
func parseModuleSourceList(rest string, lineNum int, file string) ([]string, error) {
	t := strings.TrimSpace(rest)
	if t == "" {
		return nil, fmt.Errorf("%s:%d: source: requires a JSON list value", file, lineNum)
	}
	var out []string
	if err := json.Unmarshal([]byte(t), &out); err != nil {
		return nil, fmt.Errorf("%s:%d: source: list parse: %v", file, lineNum, err)
	}
	return out, nil
}

// parseInlineQuoted parses ` "..."` after an inline key like `why:` / `doc:`.
func parseInlineQuoted(rest string, lineNum int, file, key string) (string, error) {
	t := strings.TrimLeft(rest, " ")
	val, leftover, err := parseQuoted(t, lineNum, file)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(leftover) != "" {
		return "", fmt.Errorf("%s:%d: trailing content after %s value: %q", file, lineNum, key, leftover)
	}
	return val, nil
}

// --- doc / invariant blocks ------------------------------------------------

// parseDocBlock expects a `doc "Title":` head at indent `ind`, then a heredoc
// body at indent ind+1. Returns title + body.
func (p *parser) parseDocBlock(ind int) (title, body string, err error) {
	_, content, lineNum, _, err := p.consumeStructural()
	if err != nil {
		return "", "", err
	}
	// content starts with "doc "
	rest := strings.TrimPrefix(content, "doc ")
	rest = strings.TrimLeft(rest, " ")
	titleVal, leftover, err := parseQuoted(rest, lineNum, p.file)
	if err != nil {
		return "", "", err
	}
	leftover = strings.TrimLeft(leftover, " ")
	if leftover != ":" {
		return "", "", p.errf(lineNum, "expected `:` after doc title, got %q", leftover)
	}
	body, err = p.parseHeredocAtIndent(ind + 1)
	return titleVal, body, err
}

// parseInvariantBlock expects an `invariant "Title":` head at `ind`, optional
// `verified-by:` and/or `procedural` lines, then a heredoc.
func (p *parser) parseInvariantBlock(ind int) (extract.Invariant, error) {
	var inv extract.Invariant
	_, content, lineNum, _, err := p.consumeStructural()
	if err != nil {
		return inv, err
	}
	rest := strings.TrimPrefix(content, "invariant ")
	rest = strings.TrimLeft(rest, " ")
	titleVal, leftover, err := parseQuoted(rest, lineNum, p.file)
	if err != nil {
		return inv, err
	}
	leftover = strings.TrimLeft(leftover, " ")
	if leftover != ":" {
		return inv, p.errf(lineNum, "expected `:` after invariant title, got %q", leftover)
	}
	inv.Title = titleVal

	// Inner meta lines + heredoc, all at ind+1.
	for {
		childInd, childContent, childLine, ok, err := p.peekStructural()
		if err != nil {
			return inv, err
		}
		if !ok || childInd != ind+1 {
			return inv, p.errf(childLine, "expected heredoc inside invariant %q", titleVal)
		}
		tok := firstToken(childContent)
		switch tok {
		case "verified-by:":
			p.pos++
			val := strings.TrimSpace(childContent[len("verified-by:"):])
			if val == "" {
				return inv, p.errf(childLine, "verified-by: requires test name(s)")
			}
			for _, name := range strings.Split(val, ",") {
				name = strings.TrimSpace(name)
				if name != "" {
					inv.VerifiedBy = append(inv.VerifiedBy, name)
				}
			}
		case "procedural":
			p.pos++
			if strings.TrimSpace(childContent) != "procedural" {
				return inv, p.errf(childLine, "`procedural` takes no value")
			}
			inv.Procedural = true
		case `"""`:
			body, err := p.parseHeredocAtIndent(ind + 1)
			if err != nil {
				return inv, err
			}
			inv.Content = body
			return inv, nil
		default:
			return inv, p.errf(childLine, "unexpected key %q inside invariant body", tok)
		}
	}
}

// parseHeredocAtIndent expects the next structural line to be `"""` at the
// given indent, then reads raw lines (preserving blanks, treating `#` as
// literal) until the closing `"""` at the same indent. Returns the body with
// each content line's leading `indent*2` spaces stripped, joined by LF.
func (p *parser) parseHeredocAtIndent(indent int) (string, error) {
	ind, content, lineNum, ok, err := p.consumeStructural()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%s: expected `\"\"\"` heredoc opener, got EOF", p.file)
	}
	if ind != indent || content != `"""` {
		return "", p.errf(lineNum, "expected `\"\"\"` heredoc opener at indent %d, got %q at indent %d", indent, content, ind)
	}

	want := indent * 2
	var bodyLines []string
	for p.pos < len(p.rawLines) {
		raw := p.rawLines[p.pos]
		// Closing fence: line content (after stripping leading spaces) equals
		// `"""` at exactly the expected indent.
		stripped := strings.TrimLeft(raw, " ")
		leading := len(raw) - len(stripped)
		// Reject tabs in heredoc line indentation.
		if strings.ContainsAny(raw[:leading], "\t") {
			return "", p.errf(p.pos+1, "tab in heredoc line indentation")
		}
		if stripped == `"""` && leading == want {
			p.pos++
			return strings.Join(bodyLines, "\n"), nil
		}
		// Whitespace-only line is a preserved blank line.
		if stripped == "" {
			bodyLines = append(bodyLines, "")
			p.pos++
			continue
		}
		// Content line: must have at least `want` leading spaces; strip exactly that many.
		if leading < want {
			return "", p.errf(p.pos+1, "heredoc body line indented less than opener (got %d spaces, want >= %d)", leading, want)
		}
		bodyLines = append(bodyLines, raw[want:])
		p.pos++
	}
	return "", fmt.Errorf("%s: unterminated heredoc (opened at line %d)", p.file, lineNum)
}

// --- decl parsers ----------------------------------------------------------

// parseStructDecl parses `class <name>` / `struct <name>` / `enum <name>` at ind.
func (p *parser) parseStructDecl(ind int, kind string, pkg *extract.PackageInfo) error {
	_, content, lineNum, _, err := p.consumeStructural()
	if err != nil {
		return err
	}
	rest := strings.TrimPrefix(content, kind+" ")
	name := strings.TrimSpace(rest)
	if !isIdent(name) {
		return p.errf(lineNum, "invalid %s name %q", kind, name)
	}
	s := extract.NewStructInfo()
	if kind == "class" {
		s.IsClass = true
	}
	// Body at ind+1: source: | why: | field <...> | method <...>
	for {
		bInd, bContent, bLineNum, ok, err := p.peekStructural()
		if err != nil {
			return err
		}
		if !ok || bInd <= ind {
			break
		}
		if bInd != ind+1 {
			return p.errf(bLineNum, "unexpected indent %d in %s body (want %d)", bInd, kind, ind+1)
		}
		tok := firstToken(bContent)
		switch tok {
		case "source:":
			p.pos++
			val := strings.TrimSpace(bContent[len("source:"):])
			s.Source = val
			parseFileLine(val, &s.File, &s.Line)
		case "why:":
			p.pos++
			val, err := parseInlineQuoted(bContent[len("why:"):], bLineNum, p.file, "why:")
			if err != nil {
				return err
			}
			s.Why = val
		case "field":
			if err := p.parseFieldBlock(ind+1, s); err != nil {
				return err
			}
		case "method":
			if err := p.parseMethodBlock(ind+1, s.Methods); err != nil {
				return err
			}
		default:
			return p.errf(bLineNum, "unrecognized key %q in %s body", tok, kind)
		}
	}
	pkg.Structs[name] = s
	return nil
}

func (p *parser) parseInterfaceDecl(ind int, pkg *extract.PackageInfo) error {
	_, content, lineNum, _, err := p.consumeStructural()
	if err != nil {
		return err
	}
	rest := strings.TrimPrefix(content, "interface ")
	name := strings.TrimSpace(rest)
	if !isIdent(name) {
		return p.errf(lineNum, "invalid interface name %q", name)
	}
	i := extract.NewInterfaceInfo()
	for {
		bInd, bContent, bLineNum, ok, err := p.peekStructural()
		if err != nil {
			return err
		}
		if !ok || bInd <= ind {
			break
		}
		if bInd != ind+1 {
			return p.errf(bLineNum, "unexpected indent %d in interface body (want %d)", bInd, ind+1)
		}
		tok := firstToken(bContent)
		switch tok {
		case "source:":
			p.pos++
			val := strings.TrimSpace(bContent[len("source:"):])
			i.Source = val
			parseFileLine(val, &i.File, &i.Line)
		case "why:":
			p.pos++
			val, err := parseInlineQuoted(bContent[len("why:"):], bLineNum, p.file, "why:")
			if err != nil {
				return err
			}
			i.Why = val
		case "method":
			if err := p.parseMethodBlock(ind+1, i.Methods); err != nil {
				return err
			}
		default:
			return p.errf(bLineNum, "unrecognized key %q in interface body", tok)
		}
	}
	pkg.Interfaces[name] = i
	return nil
}

func (p *parser) parseFuncDecl(ind int, pkg *extract.PackageInfo) error {
	_, content, lineNum, _, err := p.consumeStructural()
	if err != nil {
		return err
	}
	rest := strings.TrimPrefix(content, "func ")
	rest = strings.TrimLeft(rest, " ")
	name := leadingIdentifier(rest)
	if name == "" {
		return p.errf(lineNum, "func line missing function name: %q", content)
	}
	f := &extract.FuncInfo{SignatureText: strings.TrimRight(rest, " ")}
	if err := p.parseFuncBody(ind, f); err != nil {
		return err
	}
	pkg.Functions[name] = f
	return nil
}

func (p *parser) parseTypedefDecl(ind int, pkg *extract.PackageInfo) error {
	_, content, lineNum, _, err := p.consumeStructural()
	if err != nil {
		return err
	}
	rest := strings.TrimPrefix(content, "typedef ")
	rest = strings.TrimLeft(rest, " ")
	name := leadingIdentifier(rest)
	if name == "" {
		return p.errf(lineNum, "typedef line missing name: %q", content)
	}
	t := &extract.TypeDefInfo{}
	after := rest[len(name):]
	after = strings.TrimLeft(after, " ")
	if strings.HasPrefix(after, ":") {
		t.Underlying = strings.TrimSpace(after[1:])
	} else if after != "" {
		return p.errf(lineNum, "typedef expects `: <underlying>` after name, got %q", after)
	}
	// Optional decl-meta body
	for {
		bInd, bContent, bLineNum, ok, err := p.peekStructural()
		if err != nil {
			return err
		}
		if !ok || bInd <= ind {
			break
		}
		if bInd != ind+1 {
			return p.errf(bLineNum, "unexpected indent %d in typedef body (want %d)", bInd, ind+1)
		}
		tok := firstToken(bContent)
		switch tok {
		case "source:":
			p.pos++
			val := strings.TrimSpace(bContent[len("source:"):])
			t.Source = val
			parseFileLine(val, &t.File, &t.Line)
		case "why:":
			p.pos++
			val, err := parseInlineQuoted(bContent[len("why:"):], bLineNum, p.file, "why:")
			if err != nil {
				return err
			}
			t.Why = val
		default:
			return p.errf(bLineNum, "unrecognized key %q in typedef body", tok)
		}
	}
	pkg.TypeDefs[name] = t
	return nil
}

// parseFuncBody handles the source:/why: lines that follow a `func` or `method` head.
func (p *parser) parseFuncBody(ind int, f *extract.FuncInfo) error {
	for {
		bInd, bContent, bLineNum, ok, err := p.peekStructural()
		if err != nil {
			return err
		}
		if !ok || bInd <= ind {
			return nil
		}
		if bInd != ind+1 {
			return p.errf(bLineNum, "unexpected indent %d in func/method body (want %d)", bInd, ind+1)
		}
		tok := firstToken(bContent)
		switch tok {
		case "source:":
			p.pos++
			val := strings.TrimSpace(bContent[len("source:"):])
			f.Source = val
			parseFileLine(val, &f.File, &f.Line)
		case "why:":
			p.pos++
			val, err := parseInlineQuoted(bContent[len("why:"):], bLineNum, p.file, "why:")
			if err != nil {
				return err
			}
			f.Why = val
		default:
			return nil // hand back to caller; not for us
		}
	}
}

func (p *parser) parseMethodBlock(ind int, into map[string]*extract.FuncInfo) error {
	_, content, lineNum, _, err := p.consumeStructural()
	if err != nil {
		return err
	}
	rest := strings.TrimPrefix(content, "method ")
	rest = strings.TrimLeft(rest, " ")
	name := leadingIdentifier(rest)
	if name == "" {
		return p.errf(lineNum, "method line missing method name: %q", content)
	}
	f := &extract.FuncInfo{SignatureText: strings.TrimRight(rest, " ")}
	if err := p.parseFuncBody(ind, f); err != nil {
		return err
	}
	into[name] = f
	return nil
}

func (p *parser) parseFieldBlock(ind int, s *extract.StructInfo) error {
	_, content, lineNum, _, err := p.consumeStructural()
	if err != nil {
		return err
	}
	rest := strings.TrimPrefix(content, "field ")
	rest = strings.TrimLeft(rest, " ")
	name := leadingIdentifier(rest)
	if name == "" {
		return p.errf(lineNum, "field line missing field name: %q", content)
	}
	after := rest[len(name):]
	after = strings.TrimLeft(after, " ")
	field := extract.FieldInfo{Name: name}
	if strings.HasPrefix(after, ":") {
		field.SignatureText = strings.TrimSpace(after[1:])
	} else if after != "" {
		return p.errf(lineNum, "field expects `: <type>` after name, got %q", after)
	}

	// Optional inner meta: doc: | source:
	for {
		bInd, bContent, bLineNum, ok, err := p.peekStructural()
		if err != nil {
			return err
		}
		if !ok || bInd <= ind {
			break
		}
		if bInd != ind+1 {
			return p.errf(bLineNum, "unexpected indent %d in field body (want %d)", bInd, ind+1)
		}
		tok := firstToken(bContent)
		switch tok {
		case "doc:":
			p.pos++
			val, err := parseInlineQuoted(bContent[len("doc:"):], bLineNum, p.file, "doc:")
			if err != nil {
				return err
			}
			field.Doc = val
		case "source:":
			p.pos++
			// Field-level source is rare; not stored in FieldInfo, so we just
			// accept it for forward compat (no destination field exists).
			_ = strings.TrimSpace(bContent[len("source:"):])
		case "why:":
			return p.errf(bLineNum, "`why:` is not valid at field scope (spec §12 decision 4); promote to a module-scope `doc \"...\"` block")
		default:
			return p.errf(bLineNum, "unrecognized key %q in field body", tok)
		}
	}
	s.Fields = append(s.Fields, field)
	return nil
}

// leadingIdentifier returns the longest [A-Za-z_][A-Za-z0-9_]* prefix of s.
func leadingIdentifier(s string) string {
	i := 0
	for i < len(s) {
		c := s[i]
		if i == 0 {
			if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				break
			}
		} else {
			if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
				break
			}
		}
		i++
	}
	return s[:i]
}

// parseFileLine extracts "file:line" into separate fields, leaving them
// unchanged on failure. Supports paths containing colons by splitting on the
// LAST colon if its tail is all digits.
func parseFileLine(s string, fileOut *string, lineOut *int) {
	idx := strings.LastIndexByte(s, ':')
	if idx <= 0 || idx == len(s)-1 {
		*fileOut = s
		return
	}
	tail := s[idx+1:]
	for _, c := range tail {
		if c < '0' || c > '9' {
			*fileOut = s
			return
		}
	}
	n := 0
	for _, c := range tail {
		n = n*10 + int(c-'0')
	}
	*fileOut = s[:idx]
	*lineOut = n
}
