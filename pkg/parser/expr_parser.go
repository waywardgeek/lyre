package parser

import (
	"fmt"
	"strings"

	"github.com/waywardgeek/lyre/pkg/ast"
)

// --- Precedence levels for Pratt parsing ---
const (
	precNone       = 0
	precOr         = 1  // ||
	precAnd        = 2  // &&
	precBitOr      = 3  // |
	precBitXor     = 4  // ^
	precBitAnd     = 5  // &
	precEquality   = 6  // == !=
	precComparison = 7  // < > <= >=
	precShift      = 8  // << >>
	precAdditive   = 9  // + -
	precMult       = 10 // * / %
	precUnary      = 11 // - !
	precPostfix    = 12 // . () []
)

func binaryPrec(kind TokenKind) int {
	switch kind {
	case TPipePipe:
		return precOr
	case TAmpAmp:
		return precAnd
	case TPipe:
		return precBitOr
	case TCaret:
		return precBitXor
	case TAmp:
		return precBitAnd
	case TEqEq, TBangEq:
		return precEquality
	case TLt, TGt, TLtEq, TGtEq:
		return precComparison
	case TShl, TShr:
		return precShift
	case TPlus, TMinus:
		return precAdditive
	case TStar, TSlash, TPercent:
		return precMult
	case TLParen:
		return precNone
	default:
		return precNone
	}
}

func tokenToBinaryOp(kind TokenKind) ast.BinaryOp {
	switch kind {
	case TPlus:
		return ast.OpAdd
	case TMinus:
		return ast.OpSub
	case TStar:
		return ast.OpMul
	case TSlash:
		return ast.OpDiv
	case TPercent:
		return ast.OpMod
	case TEqEq:
		return ast.OpEq
	case TBangEq:
		return ast.OpNeq
	case TLt:
		return ast.OpLt
	case TLtEq:
		return ast.OpLe
	case TGt:
		return ast.OpGt
	case TGtEq:
		return ast.OpGe
	case TAmpAmp:
		return ast.OpAnd
	case TPipePipe:
		return ast.OpOr
	case TAmp:
		return ast.OpBitAnd
	case TPipe:
		return ast.OpBitOr
	case TCaret:
		return ast.OpBitXor
	case TShl:
		return ast.OpShl
	case TShr:
		return ast.OpShr
	default:
		return ast.OpAdd // unreachable
	}
}

// parseExpr parses an expression using Pratt/precedence climbing.
func (p *Parser) parseExpr() (*ast.Expr, error) {
	return p.parsePrecExpr(precNone + 1)
}

// parseExprNoStructLit parses an expression but suppresses struct literal
// parsing. Used in for/while/if/match where the expression precedes a
// mandatory block, so `Ident {` must be treated as variable + block start.
func (p *Parser) parseExprNoStructLit() (*ast.Expr, error) {
	old := p.noStructLit
	p.noStructLit = true
	defer func() { p.noStructLit = old }()
	return p.parsePrecExpr(precNone + 1)
}

func (p *Parser) parsePrecExpr(minPrec int) (*ast.Expr, error) {
	left, err := p.parseUnaryExpr()
	if err != nil {
		return nil, err
	}

	for {
		prec := binaryPrec(p.peek().Kind)
		if prec < minPrec {
			break
		}
		op := p.next()
		right, err := p.parsePrecExpr(prec + 1) // left-associative
		if err != nil {
			return nil, err
		}
		left = &ast.Expr{
			Kind: ast.ExprBinary,
			Data: &ast.BinaryExpr{Left: *left, Op: tokenToBinaryOp(op.Kind), Right: *right},
			Span: ast.Span{Start: left.Span.Start, End: right.Span.End},
		}
	}
	return left, nil
}

func (p *Parser) parseUnaryExpr() (*ast.Expr, error) {
	tok := p.peek()
	if tok.Kind == TBang {
		p.next()
		operand, err := p.parseUnaryExpr()
		if err != nil {
			return nil, err
		}
		return &ast.Expr{
			Kind: ast.ExprUnary,
			Data: &ast.UnaryExpr{Op: ast.OpNot, Operand: *operand},
			Span: ast.Span{Start: tok.Span.Start, End: operand.Span.End},
		}, nil
	}
	if tok.Kind == TMinus {
		p.next()
		operand, err := p.parseUnaryExpr()
		if err != nil {
			return nil, err
		}
		return &ast.Expr{
			Kind: ast.ExprUnary,
			Data: &ast.UnaryExpr{Op: ast.OpNeg, Operand: *operand},
			Span: ast.Span{Start: tok.Span.Start, End: operand.Span.End},
		}, nil
	}
	return p.parsePostfixExpr()
}

func (p *Parser) parsePostfixExpr() (*ast.Expr, error) {
	expr, err := p.parsePrimaryExpr()
	if err != nil {
		return nil, err
	}

	for {
		switch p.peek().Kind {
		case TDot:
			p.next()
			name, err := p.expectIdentLike()
			if err != nil {
				return nil, err
			}
			// Check for method call: obj.method(args) or obj.method<T>(args)
			if p.peek().Kind == TLParen {
				p.next()
				args, mutArgs, end, err := p.parseArgList()
				if err != nil {
					return nil, err
				}
				expr = &ast.Expr{
					Kind: ast.ExprMethodCall,
					Data: &ast.MethodCallExpr{Receiver: *expr, Method: name.Text, Args: args, MutArgs: mutArgs},
					Span: ast.Span{Start: expr.Span.Start, End: end},
				}
			} else if p.peek().Kind == TLt {
				// Try method<TypeArgs>(args)
				saved := *p.lex
				savedErrors := len(p.errors)
				typeArgs, ok := p.tryParseTypeArgs()
				if ok && p.peek().Kind == TLParen {
					p.next()
					args, mutArgs, end, err := p.parseArgList()
					if err != nil {
						return nil, err
					}
					expr = &ast.Expr{
						Kind: ast.ExprMethodCall,
						Data: &ast.MethodCallExpr{Receiver: *expr, Method: name.Text, TypeArgs: typeArgs, Args: args, MutArgs: mutArgs},
						Span: ast.Span{Start: expr.Span.Start, End: end},
					}
				} else {
					*p.lex = saved
					p.errors = p.errors[:savedErrors]
					expr = &ast.Expr{
						Kind: ast.ExprFieldAccess,
						Data: &ast.FieldAccessExpr{Receiver: *expr, Field: name.Text},
						Span: ast.Span{Start: expr.Span.Start, End: name.Span.End},
					}
				}
			} else if p.peek().Kind == TLBrace && expr.Kind == ast.ExprIdent && p.isStructLitAhead() {
				// Qualified struct literal: pkg.Type{} or pkg.Type{field: val}
				qualName := expr.Data.(*ast.IdentExpr).Name + "." + name.Text
				qualTok := Token{Kind: TIdent, Text: qualName, Span: ast.Span{Start: expr.Span.Start, End: name.Span.End}}
				var litErr error
				expr, litErr = p.parseStructLit(qualTok)
				if litErr != nil {
					return nil, litErr
				}
			} else {
				expr = &ast.Expr{
					Kind: ast.ExprFieldAccess,
					Data: &ast.FieldAccessExpr{Receiver: *expr, Field: name.Text},
					Span: ast.Span{Start: expr.Span.Start, End: name.Span.End},
				}
			}
		case TLt:
			// Try to parse generic call: expr<TypeArgs>(args)
			// Save lexer state for backtracking if this is actually a comparison
			saved := *p.lex
			savedErrors := len(p.errors)
			typeArgs, ok := p.tryParseTypeArgs()
			if ok && p.peek().Kind == TLParen {
				p.next() // consume '('
				args, mutArgs, end, err := p.parseArgList()
				if err != nil {
					return nil, err
				}
				expr = &ast.Expr{
					Kind: ast.ExprCall,
					Data: &ast.CallExpr{Func: *expr, TypeArgs: typeArgs, Args: args, MutArgs: mutArgs},
					Span: ast.Span{Start: expr.Span.Start, End: end},
				}
			} else if ok && p.peek().Kind == TLBrace && expr.Kind == ast.ExprIdent && p.isStructLitAhead() {
				// Generic struct/class literal: TypeName<T> { field: value }
				identName := expr.Data.(*ast.IdentExpr).Name
				nameTok := Token{Kind: TIdent, Text: identName, Span: expr.Span}
				var litErr error
				expr, litErr = p.parseStructLit(nameTok)
				if litErr != nil {
					return nil, litErr
				}
				// Store type args on the struct lit
				sl := expr.Data.(*ast.StructLitExpr)
				sl.TypeArgs = typeArgs
			} else {
				// Not a generic call — restore and let binary handle it
				*p.lex = saved
				p.errors = p.errors[:savedErrors]
				return expr, nil
			}
		case TLParen:
			p.next()
			args, mutArgs, end, err := p.parseArgList()
			if err != nil {
				return nil, err
			}
			expr = &ast.Expr{
				Kind: ast.ExprCall,
				Data: &ast.CallExpr{Func: *expr, Args: args, MutArgs: mutArgs},
				Span: ast.Span{Start: expr.Span.Start, End: end},
			}
		case TLBracket:
			p.next()
			// Check for slice with no low bound: [:high] or [:]
			if p.peek().Kind == TColon {
				p.next() // consume ':'
				var high *ast.Expr
				if p.peek().Kind != TRBracket {
					h, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					high = h
				}
				end, err := p.expect(TRBracket)
				if err != nil {
					return nil, err
				}
				expr = &ast.Expr{
					Kind: ast.ExprSlice,
					Data: &ast.SliceExpr{Receiver: *expr, Low: nil, High: high},
					Span: ast.Span{Start: expr.Span.Start, End: end.Span.End},
				}
			} else {
				index, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				// Check for slice: [low:high] or [low:]
				if p.peek().Kind == TColon {
					p.next() // consume ':'
					var high *ast.Expr
					if p.peek().Kind != TRBracket {
						h, err := p.parseExpr()
						if err != nil {
							return nil, err
						}
						high = h
					}
					end, err := p.expect(TRBracket)
					if err != nil {
						return nil, err
					}
					expr = &ast.Expr{
						Kind: ast.ExprSlice,
						Data: &ast.SliceExpr{Receiver: *expr, Low: index, High: high},
						Span: ast.Span{Start: expr.Span.Start, End: end.Span.End},
					}
				} else {
					end, err := p.expect(TRBracket)
					if err != nil {
						return nil, err
					}
					expr = &ast.Expr{
						Kind: ast.ExprIndex,
						Data: &ast.IndexExpr{Receiver: *expr, Index: *index},
						Span: ast.Span{Start: expr.Span.Start, End: end.Span.End},
					}
				}
			}
		case TBang:
			// Postfix ! — unwrap optional, panic if nil
			bang := p.next()
			expr = &ast.Expr{
				Kind: ast.ExprUnwrap,
				Data: &ast.UnwrapExpr{Operand: *expr},
				Span: ast.Span{Start: expr.Span.Start, End: bang.Span.End},
			}
		case TQuestion:
			// Postfix ? — error propagation, early return on error
			q := p.next()
			expr = &ast.Expr{
				Kind: ast.ExprTry,
				Data: &ast.TryExpr{Operand: *expr},
				Span: ast.Span{Start: expr.Span.Start, End: q.Span.End},
			}
		case TIs:
			// Postfix is — variant type check: expr is VariantName
			p.next() // consume 'is'
			variantTok, err := p.expectIdentLike()
			if err != nil {
				return nil, err
			}
			expr = &ast.Expr{
				Kind: ast.ExprIs,
				Data: &ast.IsExpr{Operand: *expr, Variant: variantTok.Text},
				Span: ast.Span{Start: expr.Span.Start, End: variantTok.Span.End},
			}
		case TAs:
			// Postfix as — type cast: expr as Type
			p.next() // consume 'as'
			targetType, err := p.parseTypeExpr()
			if err != nil {
				return nil, err
			}
			expr = &ast.Expr{
				Kind: ast.ExprCast,
				Data: &ast.CastExpr{
					TargetType: *targetType,
					Operand:    *expr,
				},
				Span: ast.Span{Start: expr.Span.Start, End: targetType.Span.End},
			}
		default:
			return expr, nil
		}
	}
}

func (p *Parser) parseArgList() ([]ast.Expr, []bool, ast.Pos, error) {
	p.exprDepth++
	defer func() { p.exprDepth-- }()
	var args []ast.Expr
	var mutArgs []bool
	hasMut := false
	for p.peek().Kind != TRParen && p.peek().Kind != TEOF {
		p.skipNewlines()
		isMut := false
		if p.peek().Kind == TMut {
			isMut = true
			hasMut = true
			p.next()
		}
		arg, err := p.parseExpr()
		if err != nil {
			return nil, nil, ast.Pos{}, err
		}
		args = append(args, *arg)
		mutArgs = append(mutArgs, isMut)
		if p.peek().Kind == TComma {
			p.next()
		}
		p.skipNewlines()
	}
	end, err := p.expect(TRParen)
	if err != nil {
		return nil, nil, ast.Pos{}, err
	}
	if !hasMut {
		mutArgs = nil // don't allocate for common case
	}
	return args, mutArgs, end.Span.End, nil
}

func (p *Parser) parsePrimaryExpr() (*ast.Expr, error) {
	tok := p.peek()
	switch tok.Kind {
	case TIntLit:
		p.next()
		return &ast.Expr{
			Kind: ast.ExprIntLit,
			Data: &ast.IntLitExpr{Value: tok.Text},
			Span: tok.Span,
		}, nil
	case TCharLit:
		p.next()
		// Character literal — convert to integer value (u8)
		val := 0
		if len(tok.Text) > 0 {
			val = int(tok.Text[0])
		}
		return &ast.Expr{
			Kind: ast.ExprIntLit,
			Data: &ast.IntLitExpr{Value: fmt.Sprintf("%d", val), TypeHint: "u8"},
			Span: tok.Span,
		}, nil
	case TBacktickSym:
		p.next()
		// Desugar `name` → sym("name")
		return &ast.Expr{
			Kind: ast.ExprCall,
			Data: &ast.CallExpr{
				Func: ast.Expr{
					Kind: ast.ExprIdent,
					Data: &ast.IdentExpr{Name: "sym"},
					Span: tok.Span,
				},
				Args: []ast.Expr{
					{
						Kind: ast.ExprStringLit,
						Data: &ast.StringLitExpr{Value: tok.Text},
						Span: tok.Span,
					},
				},
			},
			Span: tok.Span,
		}, nil
	case TFloatLit:
		p.next()
		return &ast.Expr{
			Kind: ast.ExprFloatLit,
			Data: &ast.FloatLitExpr{Value: tok.Text},
			Span: tok.Span,
		}, nil
	case TStringLit, TTripleStringLit:
		p.next()
		return &ast.Expr{
			Kind: ast.ExprStringLit,
			Data: &ast.StringLitExpr{Value: tok.Text},
			Span: tok.Span,
		}, nil
	case TFStringLit:
		p.next()
		return p.parseFString(tok)
	case TTrue:
		p.next()
		return &ast.Expr{
			Kind: ast.ExprBoolLit,
			Data: &ast.BoolLitExpr{Value: true},
			Span: tok.Span,
		}, nil
	case TFalse:
		p.next()
		return &ast.Expr{
			Kind: ast.ExprBoolLit,
			Data: &ast.BoolLitExpr{Value: false},
			Span: tok.Span,
		}, nil
	case TNil:
		p.next()
		return &ast.Expr{
			Kind: ast.ExprNil,
			Span: tok.Span,
		}, nil
	case TIdent, TSelf:
		p.next()
		name := tok.Text
		if tok.Kind == TSelf {
			name = "self"
		}
		// Check for map literal: map[K]V{...}
		if name == "map" && p.peek().Kind == TLBracket {
			return p.parseMapLit(tok)
		}
		// Check for struct literal: TypeName{Field: value, ...}
		// Named fields: always detected via isStructLitAhead (Ident: pattern)
		// Positional fields: only allowed when inside () or [] (exprDepth > 0)
		// where { can't start a block, avoiding ambiguity with for/while/if bodies
		if p.peek().Kind == TLBrace && (p.isStructLitAhead() || p.exprDepth > 0) {
			return p.parseStructLit(tok)
		}
		return &ast.Expr{
			Kind: ast.ExprIdent,
			Data: &ast.IdentExpr{Name: name},
			Span: tok.Span,
		}, nil
	case TLParen:
		// Parenthesized expr or tuple literal
		p.next()
		p.exprDepth++
		defer func() { p.exprDepth-- }()
		if p.peek().Kind == TRParen {
			// Empty tuple / unit
			end := p.next()
			return &ast.Expr{
				Kind: ast.ExprTupleLit,
				Data: &ast.TupleLitExpr{},
				Span: ast.Span{Start: tok.Span.Start, End: end.Span.End},
			}, nil
		}
		first, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.peek().Kind == TComma {
			// Tuple
			elems := []ast.Expr{*first}
			for p.peek().Kind == TComma {
				p.next()
				if p.peek().Kind == TRParen {
					break
				}
				elem, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				elems = append(elems, *elem)
			}
			end, err := p.expect(TRParen)
			if err != nil {
				return nil, err
			}
			return &ast.Expr{
				Kind: ast.ExprTupleLit,
				Data: &ast.TupleLitExpr{Elems: elems},
				Span: ast.Span{Start: tok.Span.Start, End: end.Span.End},
			}, nil
		}
		// Parenthesized expression
		if _, err := p.expect(TRParen); err != nil {
			return nil, err
		}
		return first, nil
	case TLBracket:
		// List literal: [1, 2, 3]
		p.next()
		p.exprDepth++
		defer func() { p.exprDepth-- }()
		var elems []ast.Expr
		for p.peek().Kind != TRBracket && p.peek().Kind != TEOF {
			p.skipNewlines()
			elem, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			elems = append(elems, *elem)
			if p.peek().Kind == TComma {
				p.next()
			}
			p.skipNewlines()
		}
		end, err := p.expect(TRBracket)
		if err != nil {
			return nil, err
		}
		return &ast.Expr{
			Kind: ast.ExprListLit,
			Data: &ast.ListLitExpr{Elems: elems},
			Span: ast.Span{Start: tok.Span.Start, End: end.Span.End},
		}, nil
	case TMatch:
		return p.parseMatchExpr()
	case TPipe:
		return p.parseLambdaExpr()
	case TIf:
		return p.parseIfExpr()
	default:
		return nil, &ParseError{
			Message: fmt.Sprintf("expected expression, got %s (%q)", tokenNames[tok.Kind], tok.Text),
			Span:    tok.Span,
		}
	}
}

func (p *Parser) parseMapLit(mapTok Token) (*ast.Expr, error) {
	// Already consumed "map", peek is [
	p.next() // [
	// We need to skip the type part: map[K]V{...}
	// For now, just consume tokens until we hit {
	// This is a simplification — in a full type checker we'd parse types
	depth := 1
	for depth > 0 && p.peek().Kind != TEOF {
		switch p.peek().Kind {
		case TLBracket:
			depth++
			p.next()
		case TRBracket:
			depth--
			p.next()
		default:
			p.next()
		}
	}
	// Consume the value type until {
	for p.peek().Kind != TLBrace && p.peek().Kind != TEOF {
		p.next()
	}
	if _, err := p.expect(TLBrace); err != nil {
		return nil, err
	}
	p.skipNewlines()
	var entries []ast.MapEntry
	for p.peek().Kind != TRBrace && p.peek().Kind != TEOF {
		key, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TColon); err != nil {
			return nil, err
		}
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		entries = append(entries, ast.MapEntry{Key: *key, Value: *val})
		if p.peek().Kind == TComma {
			p.next()
		}
		p.skipNewlines()
	}
	end, err := p.expect(TRBrace)
	if err != nil {
		return nil, err
	}
	return &ast.Expr{
		Kind: ast.ExprMapLit,
		Data: &ast.MapLitExpr{Entries: entries},
		Span: ast.Span{Start: mapTok.Span.Start, End: end.Span.End},
	}, nil
}

// parseIfExpr parses if cond { expr } else { expr } as an expression.
// Requires else branch (must produce a value). Supports else-if chains.
func (p *Parser) parseIfExpr() (*ast.Expr, error) {
	start := p.peek().Span.Start
	p.next() // consume 'if'
	cond, err := p.parseExprNoStructLit()
	if err != nil {
		return nil, err
	}
	thenBlock, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	var elseIfs []ast.ElseIfBranch
	var elseBlock *ast.Block

	for p.peek().Kind == TElse {
		p.next() // consume 'else'
		if p.peek().Kind == TIf {
			p.next() // consume 'if'
			eifCond, err := p.parseExprNoStructLit()
			if err != nil {
				return nil, err
			}
			eifBlock, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			elseIfs = append(elseIfs, ast.ElseIfBranch{Cond: *eifCond, Body: *eifBlock})
		} else {
			eb, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			elseBlock = eb
			break
		}
	}

	if elseBlock == nil {
		return nil, &ParseError{
			Message: "if expression requires an else branch",
			Span:    ast.Span{Start: start},
		}
	}

	end := elseBlock.Stmts[len(elseBlock.Stmts)-1].Span.End
	if len(elseBlock.Stmts) == 0 {
		end = cond.Span.End
	}

	return &ast.Expr{
		Kind: ast.ExprIfElse,
		Data: &ast.IfElseExpr{
			Cond:    *cond,
			Then:    *thenBlock,
			Else:    *elseBlock,
			ElseIfs: elseIfs,
		},
		Span: ast.Span{Start: start, End: end},
	}, nil
}

func (p *Parser) parseMatchExpr() (*ast.Expr, error) {
	start := p.peek().Span.Start
	p.next() // consume 'match'
	value, err := p.parseExprNoStructLit()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TLBrace); err != nil {
		return nil, err
	}
	p.skipNewlines()
	stmt, err := p.parseMatchArms()
	if err != nil {
		return nil, err
	}
	end := p.peek().Span.End
	return &ast.Expr{
		Kind: ast.ExprMatch,
		Data: &ast.MatchStmt{Value: *value, Arms: stmt},
		Span: ast.Span{Start: start, End: end},
	}, nil
}

// parseLambdaExpr parses |params| -> ReturnType { body } or |params| { body }
func (p *Parser) parseLambdaExpr() (*ast.Expr, error) {
	start := p.peek().Span.Start
	p.next() // consume opening |

	// Parse params: name: Type, ...
	// We can't reuse parseParam because parseTypeExpr would consume | as union.
	// Instead, parse manually: name: BaseType (no union/func types in lambda params).
	var params []ast.Param
	for p.peek().Kind != TPipe && p.peek().Kind != TEOF {
		nameTok, err := p.expectIdentLike()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TColon); err != nil {
			return nil, err
		}
		typ, err := p.parseBaseType()
		if err != nil {
			return nil, err
		}
		// Allow optional ? after base type
		if p.peek().Kind == TQuestion {
			p.next()
			typ = &ast.TypeExpr{
				Kind: ast.TypeOptional,
				Data: ast.OptionalType{Inner: *typ},
				Span: typ.Span,
			}
		}
		params = append(params, ast.Param{
			Name: nameTok.Text,
			Type: *typ,
		})
		if p.peek().Kind == TComma {
			p.next()
		}
	}
	if _, err := p.expect(TPipe); err != nil {
		return nil, err
	}

	// Optional return type: -> Type
	var retType *ast.TypeExpr
	if p.peek().Kind == TArrow {
		p.next()
		rt, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		retType = rt
	}

	// Body block
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	return &ast.Expr{
		Kind: ast.ExprLambda,
		Data: &ast.LambdaExpr{
			Params:     params,
			ReturnType: retType,
			Body:       body,
		},
		Span: ast.Span{Start: start, End: body.Span.End},
	}, nil
}

// --- Statement parsing ---

// parseBlock parses { stmts... }
func (p *Parser) parseBlock() (*ast.Block, error) {
	start := p.peek().Span.Start
	if _, err := p.expect(TLBrace); err != nil {
		return nil, err
	}
	p.skipNewlines()
	var stmts []ast.Stmt
	for p.peek().Kind != TRBrace && p.peek().Kind != TEOF {
		stmt, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, *stmt)
		p.skipNewlines()
	}
	end, err := p.expect(TRBrace)
	if err != nil {
		return nil, err
	}
	return &ast.Block{
		Stmts: stmts,
		Span:  ast.Span{Start: start, End: end.Span.End},
	}, nil
}

func (p *Parser) parseStmt() (*ast.Stmt, error) {
	tok := p.peek()
	// Handle contextual keywords — only rewrite when context confirms keyword usage
	if tok.Kind == TIdent {
		if tok.Text == "lock" {
			// lock(expr) { ... } — only if followed by '('
			saved := *p.lex
			p.next()
			next := p.peek()
			*p.lex = saved
			if next.Kind == TLParen {
				tok.Kind = TLock
			}
		}
	}
	switch tok.Kind {
	case TLet:
		return p.parseVarDecl()
	case TReturn:
		return p.parseReturn()
	case TIf:
		return p.parseIf()
	case TFor:
		return p.parseFor()
	case TWhile:
		return p.parseWhile()
	case TMatch:
		return p.parseMatchStmt()
	case TBreak:
		p.next()
		return &ast.Stmt{Kind: ast.StmtBreak, Span: tok.Span}, nil
	case TContinue:
		p.next()
		return &ast.Stmt{Kind: ast.StmtContinue, Span: tok.Span}, nil
	case TCascadeKw:
		return p.parseCascade()
	case TSpawn:
		return p.parseSpawn()
	case TSelect:
		return p.parseSelect()
	case TLock:
		return p.parseLock()
	case TYield:
		return p.parseYield()
	case TLBrace:
		blk, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &ast.Stmt{Kind: ast.StmtBlock, Data: blk, Span: blk.Span}, nil
	default:
		// Expression statement or assignment
		return p.parseExprOrAssign()
	}
}

func (p *Parser) parseVarDecl() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	p.next() // consume 'let'
	isMut := false
	if p.peek().Kind == TMut {
		isMut = true
		p.next()
	}

	// Check for tuple destructuring: let (a, b) = expr
	if p.peek().Kind == TLParen {
		p.next() // consume '('
		var names []string
		for {
			name, err := p.expectIdentLike()
			if err != nil {
				return nil, err
			}
			names = append(names, name.Text)
			if p.peek().Kind == TComma {
				p.next()
			} else {
				break
			}
		}
		if _, err := p.expect(TRParen); err != nil {
			return nil, err
		}
		if _, err := p.expect(TAssign); err != nil {
			return nil, err
		}
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.Stmt{
			Kind: ast.StmtVarDecl,
			Data: &ast.VarDeclStmt{Names: names, IsMut: isMut, Value: val},
			Span: ast.Span{Start: start, End: p.peek().Span.Start},
		}, nil
	}

	// Check for pattern let: let Variant(x, y) = expr else { ... }
	// or let _ = expr else { ... }
	// Detected by: Ident followed by '(' (variant pattern), or '_' (wildcard)
	if p.isPatternLetAhead() {
		return p.parseLetElse(start, isMut)
	}

	name, err := p.expectIdentLike()
	if err != nil {
		return nil, err
	}
	decl := &ast.VarDeclStmt{Name: name.Text, IsMut: isMut}

	// Optional type annotation
	if p.peek().Kind == TColon {
		p.next()
		typ, err := p.parseTypeExpr()
		if err != nil {
			return nil, err
		}
		decl.Type = typ
	}

	// Optional initializer
	if p.peek().Kind == TAssign {
		p.next()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		decl.Value = val
	}

	return &ast.Stmt{
		Kind: ast.StmtVarDecl,
		Data: decl,
		Span: ast.Span{Start: start, End: p.peek().Span.Start},
	}, nil
}

// isPatternLetAhead checks if the current position looks like a pattern in let context.
// Detects: Ident( (variant pattern) or _ (wildcard pattern).
func (p *Parser) isPatternLetAhead() bool {
	tok := p.peek()
	if tok.Kind == TIdent && len(tok.Text) > 0 && tok.Text[0] >= 'A' && tok.Text[0] <= 'Z' {
		// Save state and check for '(' after ident
		saved := *p.lex
		pushedSaved := p.pushed
		p.next() // consume ident
		isVariant := p.peek().Kind == TLParen
		*p.lex = saved
		p.pushed = pushedSaved
		return isVariant
	}
	return false
}

// parseLetElse parses: let Pattern = expr else { body }
func (p *Parser) parseLetElse(start ast.Pos, isMut bool) (*ast.Stmt, error) {
	pat, err := p.parsePattern()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TAssign); err != nil {
		return nil, err
	}
	val, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TElse); err != nil {
		return nil, p.newError(p.peek().Span, "let with pattern requires 'else' block")
	}
	elseBlock, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.Stmt{
		Kind: ast.StmtVarDecl,
		Data: &ast.VarDeclStmt{
			IsMut:     isMut,
			Value:     val,
			Pattern:   pat,
			ElseBlock: elseBlock,
		},
		Span: ast.Span{Start: start, End: p.peek().Span.Start},
	}, nil
}

func (p *Parser) parseReturn() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	p.next() // consume 'return'
	ret := &ast.ReturnStmt{}
	// Check if there's an expression following (not newline/EOF/})
	if p.peek().Kind != TNewline && p.peek().Kind != TEOF && p.peek().Kind != TRBrace {
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		ret.Value = val
	}
	return &ast.Stmt{
		Kind: ast.StmtReturn,
		Data: ret,
		Span: ast.Span{Start: start, End: p.peek().Span.Start},
	}, nil
}

func (p *Parser) parseYield() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	p.next() // consume 'yield'
	val, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.Stmt{
		Kind: ast.StmtYield,
		Data: &ast.YieldStmt{Value: val},
		Span: ast.Span{Start: start, End: p.peek().Span.Start},
	}, nil
}

func (p *Parser) parseIf() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	p.next() // consume 'if'

	// Check for 'if let Pattern = expr { ... }'
	if p.peek().Kind == TLet {
		return p.parseIfLet(start)
	}

	cond, err := p.parseExprNoStructLit()
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	ifStmt := &ast.IfStmt{Condition: *cond, Then: *then}

	// else if / else
	for p.peek().Kind == TElse {
		p.next()
		if p.peek().Kind == TIf {
			p.next()
			elifCond, err := p.parseExprNoStructLit()
			if err != nil {
				return nil, err
			}
			elifBody, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			ifStmt.ElseIfs = append(ifStmt.ElseIfs, ast.ElseIf{
				Condition: *elifCond,
				Body:      *elifBody,
				Span:      elifBody.Span,
			})
		} else {
			elseBody, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			ifStmt.Else = elseBody
			break
		}
	}

	return &ast.Stmt{
		Kind: ast.StmtIf,
		Data: ifStmt,
		Span: ast.Span{Start: start, End: p.peek().Span.Start},
	}, nil
}

// parseIfLet parses: if let Pattern = expr { body } [else { body }]
func (p *Parser) parseIfLet(start ast.Pos) (*ast.Stmt, error) {
	p.next() // consume 'let'
	pat, err := p.parsePattern()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TAssign); err != nil {
		return nil, err
	}
	value, err := p.parseExprNoStructLit()
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	ifStmt := &ast.IfStmt{
		LetPattern: pat,
		LetValue:   value,
		Then:       *then,
	}
	// Optional else block
	if p.peek().Kind == TElse {
		p.next()
		elseBody, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		ifStmt.Else = elseBody
	}
	return &ast.Stmt{
		Kind: ast.StmtIf,
		Data: ifStmt,
		Span: ast.Span{Start: start, End: p.peek().Span.Start},
	}, nil
}

func (p *Parser) parseFor() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	p.next() // consume 'for'
	varName, err := p.expectIdentLike()
	if err != nil {
		return nil, err
	}
	// Check for index variable: for i, x in xs
	var indexVar string
	if p.peek().Kind == TComma {
		p.next() // consume ','
		indexVar = varName.Text
		varName, err = p.expectIdentLike()
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.expect(TIn); err != nil {
		return nil, err
	}
	collection, err := p.parseExprNoStructLit()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.Stmt{
		Kind: ast.StmtFor,
		Data: &ast.ForStmt{Var: varName.Text, IndexVar: indexVar, Collection: *collection, Body: *body},
		Span: ast.Span{Start: start, End: body.Span.End},
	}, nil
}

func (p *Parser) parseWhile() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	p.next() // consume 'while'
	cond, err := p.parseExprNoStructLit()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.Stmt{
		Kind: ast.StmtWhile,
		Data: &ast.WhileStmt{Condition: *cond, Body: *body},
		Span: ast.Span{Start: start, End: body.Span.End},
	}, nil
}

func (p *Parser) parseMatchStmt() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	p.next() // consume 'match'
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TLBrace); err != nil {
		return nil, err
	}
	p.skipNewlines()
	arms, err := p.parseMatchArms()
	if err != nil {
		return nil, err
	}
	return &ast.Stmt{
		Kind: ast.StmtMatch,
		Data: &ast.MatchStmt{Value: *value, Arms: arms},
		Span: ast.Span{Start: start, End: p.peek().Span.Start},
	}, nil
}

func (p *Parser) parseMatchArms() ([]ast.MatchArm, error) {
	var arms []ast.MatchArm
	for p.peek().Kind != TRBrace && p.peek().Kind != TEOF {
		pat, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		// Multi-pattern: pat1 | pat2 | pat3
		var altPatterns []ast.Pattern
		for p.peek().Kind == TPipe {
			p.next()
			alt, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			altPatterns = append(altPatterns, *alt)
		}
		// Optional guard clause: `if <expr>`
		var guard *ast.Expr
		if p.peek().Kind == TIf {
			p.next()
			g, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			guard = g
		}
		if _, err := p.expect(TFatArrow); err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		arms = append(arms, ast.MatchArm{Pattern: *pat, Patterns: altPatterns, Guard: guard, Body: *body, Span: body.Span})
		p.skipNewlines()
	}
	if _, err := p.expect(TRBrace); err != nil {
		return nil, err
	}
	return arms, nil
}

func (p *Parser) parsePattern() (*ast.Pattern, error) {
	tok := p.peek()
	switch tok.Kind {
	case TIdent:
		p.next()
		// Wildcard: _
		if tok.Text == "_" {
			return &ast.Pattern{Kind: ast.PatWildcard, Span: tok.Span}, nil
		}
		// Qualified variant pattern: EnumName.Variant or EnumName.Variant(bindings)
		if p.peek().Kind == TDot {
			p.next() // consume '.'
			variantTok := p.peek()
			if variantTok.Kind != TIdent {
				return nil, p.newError(variantTok.Span, "expected variant name after '.'")
			}
			p.next() // consume variant name
			// EnumName.Variant(bindings)
			if p.peek().Kind == TLParen {
				p.next()
				var bindings []ast.Pattern
				for p.peek().Kind != TRParen && p.peek().Kind != TEOF {
					b, err := p.parsePattern()
					if err != nil {
						return nil, err
					}
					bindings = append(bindings, *b)
					if p.peek().Kind == TComma {
						p.next()
					}
				}
				if _, err := p.expect(TRParen); err != nil {
					return nil, err
				}
				return &ast.Pattern{
					Kind: ast.PatVariant,
					Data: &ast.VariantPattern{Name: variantTok.Text, Bindings: bindings},
					Span: ast.Span{Start: tok.Span.Start, End: p.peek().Span.End},
				}, nil
			}
			// EnumName.Variant (unit variant, no parens)
			return &ast.Pattern{
				Kind: ast.PatIdent,
				Data: &ast.IdentPattern{Name: variantTok.Text},
				Span: ast.Span{Start: tok.Span.Start, End: variantTok.Span.End},
			}, nil
		}
		// Check for variant pattern: Name(bindings)
		if p.peek().Kind == TLParen {
			p.next()
			var bindings []ast.Pattern
			for p.peek().Kind != TRParen && p.peek().Kind != TEOF {
				b, err := p.parsePattern()
				if err != nil {
					return nil, err
				}
				bindings = append(bindings, *b)
				if p.peek().Kind == TComma {
					p.next()
				}
			}
			if _, err := p.expect(TRParen); err != nil {
				return nil, err
			}
			return &ast.Pattern{
				Kind: ast.PatVariant,
				Data: &ast.VariantPattern{Name: tok.Text, Bindings: bindings},
				Span: ast.Span{Start: tok.Span.Start, End: p.peek().Span.End},
			}, nil
		}
		// Simple ident binding
		return &ast.Pattern{
			Kind: ast.PatIdent,
			Data: &ast.IdentPattern{Name: tok.Text},
			Span: tok.Span,
		}, nil
	case TIntLit, TCharLit, TStringLit, TTripleStringLit, TTrue, TFalse:
		p.next()
		expr := ast.Expr{Span: tok.Span}
		switch tok.Kind {
		case TIntLit:
			expr.Kind = ast.ExprIntLit
			expr.Data = &ast.IntLitExpr{Value: tok.Text}
		case TCharLit:
			expr.Kind = ast.ExprIntLit
			val := 0
			if len(tok.Text) > 0 {
				val = int(tok.Text[0])
			}
			expr.Data = &ast.IntLitExpr{Value: fmt.Sprintf("%d", val), TypeHint: "u8"}
		case TStringLit, TTripleStringLit:
			expr.Kind = ast.ExprStringLit
			expr.Data = &ast.StringLitExpr{Value: tok.Text}
		case TTrue:
			expr.Kind = ast.ExprBoolLit
			expr.Data = &ast.BoolLitExpr{Value: true}
		case TFalse:
			expr.Kind = ast.ExprBoolLit
			expr.Data = &ast.BoolLitExpr{Value: false}
		}
		return &ast.Pattern{
			Kind: ast.PatLiteral,
			Data: &ast.LiteralPattern{Expr: expr},
			Span: tok.Span,
		}, nil
	case TLParen:
		// Tuple pattern: (x, y, ...)
		p.next()
		var elems []ast.Pattern
		for p.peek().Kind != TRParen && p.peek().Kind != TEOF {
			elem, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			elems = append(elems, *elem)
			if p.peek().Kind == TComma {
				p.next()
			}
		}
		end := p.peek().Span.End
		if _, err := p.expect(TRParen); err != nil {
			return nil, err
		}
		return &ast.Pattern{
			Kind: ast.PatTuple,
			Data: &ast.TuplePattern{Elems: elems},
			Span: ast.Span{Start: tok.Span.Start, End: end},
		}, nil
	default:
		return nil, &ParseError{
			Message: fmt.Sprintf("expected pattern, got %s (%q)", tokenNames[tok.Kind], tok.Text),
			Span:    tok.Span,
		}
	}
}

func (p *Parser) parseCascade() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	p.next() // consume 'cascade'
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.Stmt{
		Kind: ast.StmtCascade,
		Data: &ast.CascadeStmt{Body: *body},
		Span: ast.Span{Start: start, End: body.Span.End},
	}, nil
}

func (p *Parser) parseSpawn() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	p.next() // consume 'spawn'
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.Stmt{
		Kind: ast.StmtSpawn,
		Data: &ast.SpawnStmt{Body: *body},
		Span: ast.Span{Start: start, End: body.Span.End},
	}, nil
}

func (p *Parser) parseSelect() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	p.next() // consume 'select'
	if _, err := p.expect(TLBrace); err != nil {
		return nil, err
	}
	p.skipNewlines()
	var cases []ast.SelectCase
	for p.peek().Kind != TRBrace && p.peek().Kind != TEOF {
		sc, err := p.parseSelectCase()
		if err != nil {
			return nil, err
		}
		cases = append(cases, *sc)
		p.skipNewlines()
	}
	if _, err := p.expect(TRBrace); err != nil {
		return nil, err
	}
	return &ast.Stmt{
		Kind: ast.StmtSelect,
		Data: &ast.SelectStmt{Cases: cases},
		Span: ast.Span{Start: start, End: p.peek().Span.End},
	}, nil
}

func (p *Parser) parseSelectCase() (*ast.SelectCase, error) {
	start := p.peek().Span.Start
	// default case
	if p.peek().Kind == TIdent && p.peek().Text == "default" {
		p.next()
		if _, err := p.expect(TFatArrow); err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &ast.SelectCase{IsDefault: true, Body: *body, Span: ast.Span{Start: start, End: body.Span.End}}, nil
	}
	// case expr => { ... } or case var = expr => { ... }
	if _, err := p.expect(TCase); err != nil {
		return nil, err
	}
	// Try: case ident = expr => body (receive with binding)
	var bindVar string
	if p.peek().Kind == TIdent {
		saved := *p.lex
		nametok := p.next()
		if p.peek().Kind == TAssign {
			p.next()
			bindVar = nametok.Text
		} else {
			// Not a binding, rewind
			*p.lex = saved
		}
	}
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TFatArrow); err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.SelectCase{
		BindVar: bindVar,
		Expr:    expr,
		Body:    *body,
		Span:    ast.Span{Start: start, End: body.Span.End},
	}, nil
}

func (p *Parser) parseLock() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	p.next() // consume 'lock'
	if _, err := p.expect(TLParen); err != nil {
		return nil, err
	}
	mutex, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TRParen); err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.Stmt{
		Kind: ast.StmtLock,
		Data: &ast.LockStmt{Mutex: *mutex, Body: *body},
		Span: ast.Span{Start: start, End: body.Span.End},
	}, nil
}

func (p *Parser) parseExprOrAssign() (*ast.Stmt, error) {
	start := p.peek().Span.Start
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}

	// Check for assignment: = += -= *= /=
	switch p.peek().Kind {
	case TAssign:
		p.next()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.Stmt{
			Kind: ast.StmtAssign,
			Data: &ast.AssignStmt{Target: *expr, Value: *val},
			Span: ast.Span{Start: start, End: p.peek().Span.Start},
		}, nil
	case TPlusEq, TMinusEq, TStarEq, TSlashEq:
		// Desugar x += y into x = x + y
		opTok := p.next()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		var binOp ast.BinaryOp
		switch opTok.Kind {
		case TPlusEq:
			binOp = ast.OpAdd
		case TMinusEq:
			binOp = ast.OpSub
		case TStarEq:
			binOp = ast.OpMul
		case TSlashEq:
			binOp = ast.OpDiv
		}
		return &ast.Stmt{
			Kind: ast.StmtAssign,
			Data: &ast.AssignStmt{
				Target: *expr,
				Value: ast.Expr{
					Kind: ast.ExprBinary,
					Data: &ast.BinaryExpr{Left: *expr, Op: binOp, Right: *val},
					Span: ast.Span{Start: expr.Span.Start, End: val.Span.End},
				},
			},
			Span: ast.Span{Start: start, End: p.peek().Span.Start},
		}, nil
	}

	// Expression statement
	return &ast.Stmt{
		Kind: ast.StmtExpr,
		Data: &ast.ExprStmt{Expr: *expr},
		Span: ast.Span{Start: start, End: expr.Span.End},
	}, nil
}

// isStructLitAhead peeks past { to see if the first content is Ident: (field initializer).
// An empty {} is also treated as a struct literal.
func (p *Parser) isStructLitAhead() bool {
	if p.noStructLit {
		return false
	}
	// Save lexer state for lookahead
	saved := *p.lex
	savedErrors := len(p.errors)
	p.next() // consume {
	p.skipNewlines()
	result := false
	if p.peek().Kind == TRBrace {
		// Empty struct literal: TypeName{}
		result = true
	} else if p.peek().Kind == TIdent {
		// Check for FieldName:
		p.next()
		if p.peek().Kind == TColon {
			result = true
		}
	}
	*p.lex = saved // restore
	p.errors = p.errors[:savedErrors]
	return result
}

func (p *Parser) parseStructLit(nameTok Token) (*ast.Expr, error) {
	p.next() // consume {
	var fields []ast.StructLitField
	p.skipNewlines()
	for p.peek().Kind != TRBrace && p.peek().Kind != TEOF {
		// Try named field: ident ':'
		if p.peek().Kind == TIdent {
			saved := *p.lex
			savedErrors := len(p.errors)
			fieldName := p.next()
			if p.peek().Kind == TColon {
				p.next() // consume :
				val, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				fields = append(fields, ast.StructLitField{Name: fieldName.Text, Value: *val})
				if p.peek().Kind == TComma {
					p.next()
				}
				p.skipNewlines()
				continue
			}
			// Not a named field — restore and parse as positional
			*p.lex = saved
			p.errors = p.errors[:savedErrors]
		}
		// Positional field
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		fields = append(fields, ast.StructLitField{Name: "", Value: *val})
		if p.peek().Kind == TComma {
			p.next()
		}
		p.skipNewlines()
	}
	end, err := p.expect(TRBrace)
	if err != nil {
		return nil, err
	}
	return &ast.Expr{
		Kind: ast.ExprStructLit,
		Data: &ast.StructLitExpr{TypeName: nameTok.Text, Fields: fields},
		Span: ast.Span{Start: nameTok.Span.Start, End: end.Span.End},
	}, nil
}


// parseFString processes the raw text of an f-string token, splitting on {expr}
// boundaries into alternating string parts and parsed expressions.
func (p *Parser) parseFString(tok Token) (*ast.Expr, error) {
	raw := tok.Text
	var parts []ast.Expr
	var buf strings.Builder
	i := 0

	for i < len(raw) {
		if raw[i] == 0x01 {
			// Sentinel for escaped {{ → literal {
			buf.WriteByte('{')
			i++
		} else if raw[i] == 0x02 {
			// Sentinel for escaped }} → literal }
			buf.WriteByte('}')
			i++
		} else if raw[i] == '{' {
			// Emit accumulated string part
			parts = append(parts, ast.Expr{
				Kind: ast.ExprStringLit,
				Data: &ast.StringLitExpr{Value: buf.String()},
				Span: tok.Span,
			})
			buf.Reset()

			// Find matching closing brace
			i++ // skip opening {
			depth := 1
			exprStart := i
			for i < len(raw) && depth > 0 {
				if raw[i] == '{' {
					depth++
				} else if raw[i] == '}' {
					depth--
				}
				if depth > 0 {
					i++
				}
			}
			exprText := raw[exprStart:i]
			i++ // skip closing }

			// Parse the expression text, preserving source position for error messages
			exprParser := &Parser{lex: NewLexer(exprText, tok.Span.Start.File)}
			exprParser.lex.line = tok.Span.Start.Line
			exprParser.lex.column = tok.Span.Start.Column + exprStart
			expr, err := exprParser.parseExpr()
			if err != nil {
				return nil, err
			}
			parts = append(parts, *expr)
		} else {
			buf.WriteByte(raw[i])
			i++
		}
	}

	// Emit final string part
	parts = append(parts, ast.Expr{
		Kind: ast.ExprStringLit,
		Data: &ast.StringLitExpr{Value: buf.String()},
		Span: tok.Span,
	})

	return &ast.Expr{
		Kind: ast.ExprStringInterp,
		Data: &ast.StringInterpExpr{Parts: parts},
		Span: tok.Span,
	}, nil
}

// tryParseTypeArgs attempts to parse <TypeExpr, TypeExpr, ...> and consume the '>'.
// Returns the parsed type args and true on success. On failure, the caller must
// restore the lexer state — this function does NOT backtrack on its own.
func (p *Parser) tryParseTypeArgs() ([]ast.TypeExpr, bool) {
	p.next() // consume '<'
	var typeArgs []ast.TypeExpr
	for p.peek().Kind != TGt && p.peek().Kind != TShr && p.peek().Kind != TEOF {
		te, err := p.parseTypeExpr()
		if err != nil {
			return nil, false
		}
		typeArgs = append(typeArgs, *te)
		if p.peek().Kind == TComma {
			p.next()
		}
	}
	if p.peek().Kind == TShr {
		// >> is actually two > tokens — consume one and push back a >
		tok := p.next()
		p.pushBack(Token{Kind: TGt, Text: ">", Span: ast.Span{
			Start: ast.Pos{File: tok.Span.Start.File, Line: tok.Span.Start.Line, Column: tok.Span.Start.Column + 1},
			End:   tok.Span.End,
		}})
		return typeArgs, true
	}
	if p.peek().Kind != TGt {
		return nil, false
	}
	p.next() // consume '>'
	return typeArgs, true
}
