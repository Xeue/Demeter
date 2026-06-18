// Package expr replaces the legacy eval() used in parseCommand (main.ts ~844)
// with a small, bounded integer-arithmetic evaluator. It supports only integer
// literals, + - * / %, parentheses and unary minus (no identifiers or function
// calls) which both reproduces what Demeter's group values actually use and
// closes the eval() code-injection footgun.
//
// Substitution of the FRAME/SLOT/CARD/SPIGOT keywords happens in the scan layer
// before these functions are called, so the input here is already numeric where
// it is meant to be.
package expr

import (
	"errors"
	"strconv"
	"strings"

	"github.com/Xeue/Demeter/internal/model"
)

// ErrParse means the input is not a valid integer arithmetic expression.
var ErrParse = errors.New("expr: not an arithmetic expression")

// EvalValue evaluates a default-type command value. If the string is a valid
// integer expression it returns an Int value (e.g. "2110-30" gives 2080, matching
// the legacy eval which subtracts); otherwise it falls back to the literal
// string (e.g. "Static", "PTP"), exactly like parseCommand's catch branch.
//
// Note: unlike JS strict-mode eval, a leading-zero literal like "01" is parsed
// as decimal 1 rather than throwing; this is a deliberate, documented divergence
// and does not change blast comparisons (model.ValuesEqualLoose coerces "01"==1).
func EvalValue(s string) model.Value {
	if strings.TrimSpace(s) == "" {
		return model.StrVal(s)
	}
	n, err := evalInt(s)
	if err != nil {
		return model.StrVal(s)
	}
	return model.IntVal(n)
}

// EvalSmartIP evaluates a smartip value: split on '.', evaluate each octet as an
// integer expression, and rejoin with '.'. This path is strict: a non-numeric
// octet returns an error (matching parseCommand's smartip branch, which has no
// per-octet fallback and propagates to the outer catch).
func EvalSmartIP(s string) (string, error) {
	parts := strings.Split(s, ".")
	out := make([]string, len(parts))
	for i, p := range parts {
		n, err := evalInt(p)
		if err != nil {
			return "", err
		}
		out[i] = strconv.FormatInt(n, 10)
	}
	return strings.Join(out, "."), nil
}

// recursive-descent integer evaluator. grammar:
//   expr   := term (('+' | '-') term)*
//   term   := factor (('*' | '/' | '%') factor)*
//   factor := '-' factor | '(' expr ')' | number

type parser struct {
	s   string
	pos int
}

func evalInt(s string) (int64, error) {
	p := &parser{s: s}
	p.skipSpace()
	if p.pos >= len(p.s) {
		return 0, ErrParse
	}
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if p.pos != len(p.s) {
		return 0, ErrParse // trailing junk -> not a pure arithmetic expression
	}
	return v, nil
}

func (p *parser) skipSpace() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t') {
		p.pos++
	}
}

func (p *parser) parseExpr() (int64, error) {
	v, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.s) {
			return v, nil
		}
		op := p.s[p.pos]
		if op != '+' && op != '-' {
			return v, nil
		}
		p.pos++
		rhs, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			v += rhs
		} else {
			v -= rhs
		}
	}
}

func (p *parser) parseTerm() (int64, error) {
	v, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.s) {
			return v, nil
		}
		op := p.s[p.pos]
		if op != '*' && op != '/' && op != '%' {
			return v, nil
		}
		p.pos++
		rhs, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		switch op {
		case '*':
			v *= rhs
		case '/':
			if rhs == 0 {
				return 0, ErrParse
			}
			v /= rhs // integer division (documented divergence from JS float /)
		case '%':
			if rhs == 0 {
				return 0, ErrParse
			}
			v %= rhs
		}
	}
}

func (p *parser) parseFactor() (int64, error) {
	p.skipSpace()
	if p.pos >= len(p.s) {
		return 0, ErrParse
	}
	switch p.s[p.pos] {
	case '-':
		p.pos++
		v, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		return -v, nil
	case '+':
		p.pos++
		return p.parseFactor()
	case '(':
		p.pos++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if p.pos >= len(p.s) || p.s[p.pos] != ')' {
			return 0, ErrParse
		}
		p.pos++
		return v, nil
	default:
		return p.parseNumber()
	}
}

func (p *parser) parseNumber() (int64, error) {
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return 0, ErrParse
	}
	n, err := strconv.ParseInt(p.s[start:p.pos], 10, 64)
	if err != nil {
		return 0, ErrParse
	}
	return n, nil
}
