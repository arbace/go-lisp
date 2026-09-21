// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements the reader for go-lisp source files: it turns
// source text into a tree of forms (lists, vectors, symbols, literals, ...).
// The forms are turned into syntax nodes by the go-lisp parser.
// See golisp/SPEC.md §1 for the lexical syntax.

package syntax

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A lispKind classifies a lispForm.
type lispKind uint8

const (
	lispList    lispKind = iota // ( ... )
	lispVector                  // [ ... ]
	lispMap                     // { ... }
	lispSet                     // #{ ... }
	lispTagged                  // #tag form
	lispSymbol                  // foo, a.b, +, <-chan, ...
	lispKeyword                 // :foo
	lispLit                     // number, string, raw string, or rune literal
)

var lispKindNames = [...]string{
	lispList:    "list",
	lispVector:  "vector",
	lispMap:     "map",
	lispSet:     "set",
	lispTagged:  "tagged form",
	lispSymbol:  "symbol",
	lispKeyword: "keyword",
	lispLit:     "literal",
}

func (k lispKind) String() string { return lispKindNames[k] }

// A lispForm is a form read from go-lisp source.
type lispForm struct {
	Kind lispKind
	Pos  Pos // position of the first character

	// End is the position of the closing delimiter of a
	// list, vector, map, or set; it is unset for other forms.
	End Pos

	// Text is the symbol name, the keyword name (without ':'),
	// the tag name (without '#'), or the verbatim literal source.
	Text string

	Lit LitKind // valid if Kind == lispLit
	Bad bool    // valid if Kind == lispLit; true means the literal is malformed

	// Elems holds the elements of a collection, or
	// the single tagged form of a tagged form.
	Elems []*lispForm
}

// A lispDirective is a ";go:" comment directive.
type lispDirective struct {
	Pos   Pos    // position of the directive text (after ';')
	Blank bool   // only whitespace precedes the comment on its line
	Text  string // directive text (without ';'), e.g. "go:noinline"
}

// A lispFile is the result of reading a go-lisp source file.
type lispFile struct {
	Forms      []*lispForm
	Directives []lispDirective // in source order
	EOF        Pos
}

// lispRead reads the go-lisp source src into forms.
// Errors are reported through errh; if errh is nil, reading
// stops at the first error. The first error is returned.
// Line directives (";line filename:line[:col]" at column 1)
// change the position base of subsequent forms.
func lispRead(base *PosBase, src io.Reader, errh ErrorHandler) (_ *lispFile, first error) {
	defer func() {
		if p := recover(); p != nil {
			if err, ok := p.(Error); ok {
				first = err
				return
			}
			panic(p)
		}
	}()

	var r lispReader
	r.p.file = base
	r.p.base = base
	r.p.errh = errh
	buf, err := io.ReadAll(src)
	if err != nil {
		r.p.errorAt(MakePos(base, linebase, colbase), err.Error())
		return nil, r.p.first
	}
	r.src = buf
	r.line, r.col = linebase, colbase
	r.blank = true
	if bytes.HasPrefix(buf, []byte("\xEF\xBB\xBF")) {
		r.off = 3 // ignore BOM at file start, as Go does
	}

	f := new(lispFile)
	f.Forms = r.forms(nil)
	f.Directives = r.dirs
	f.EOF = r.pos()
	return f, r.p.first
}

// A lispReader holds the reader's state.
type lispReader struct {
	// p provides error reporting and position bases
	// (including line directive handling).
	p parser

	src       []byte
	off       int  // byte offset of the current character
	line, col uint // position of the current character
	blank     bool // only whitespace so far on the current line
	dirs      []lispDirective
}

func (r *lispReader) pos() Pos { return r.p.posAt(r.line, r.col) }

func (r *lispReader) errorf(pos Pos, format string, args ...any) {
	r.p.errorAt(pos, fmt.Sprintf(format, args...))
}

// ch returns the current character and its width in bytes.
// At EOF, ch returns -1, 0.
func (r *lispReader) ch() (rune, int) {
	if r.off >= len(r.src) {
		return -1, 0
	}
	if b := r.src[r.off]; b < utf8.RuneSelf {
		return rune(b), 1
	}
	return utf8.DecodeRune(r.src[r.off:])
}

// peekAt returns the character n bytes after the current one (-1 if none).
// It must only be used to look at ASCII characters.
func (r *lispReader) peekAt(n int) rune {
	if r.off+n >= len(r.src) {
		return -1
	}
	return rune(r.src[r.off+n])
}

// badChar returns Go's error message for an invalid source
// character (NUL, invalid UTF-8, BOM), or "" if c is valid.
func badChar(c rune, w int) string {
	switch {
	case c == 0:
		return "invalid NUL character"
	case c == utf8.RuneError && w == 1:
		return "invalid UTF-8 encoding"
	case c == 0xFEFF:
		return "invalid BOM in the middle of the file"
	}
	return ""
}

// advance moves past the current character, reporting invalid characters.
func (r *lispReader) advance() {
	c, w := r.ch()
	if c < 0 {
		return
	}
	if msg := badChar(c, w); msg != "" {
		r.errorf(r.pos(), "%s", msg)
	}
	r.off += w
	if c == '\n' {
		r.line++
		r.col = colbase
		r.blank = true
	} else {
		r.col += uint(w)
	}
}

// isLispSpace reports whether c is whitespace; commas are whitespace.
func isLispSpace(c rune) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ','
}

// isLispSymbolChar reports whether c may occur in a symbol or keyword.
func isLispSymbolChar(c rune) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '_':
		return true
	case strings.ContainsRune("+-*/%&|^<>=!~.?", c):
		return true
	case c >= utf8.RuneSelf:
		return unicode.IsLetter(c) || unicode.IsDigit(c)
	}
	return false
}

// isLispDelim reports whether c may directly follow an atom.
func isLispDelim(c rune) bool {
	return c < 0 || isLispSpace(c) || strings.ContainsRune(";)]}", c)
}

// skipSpace skips whitespace and comments, recording directives
// and applying line directives.
func (r *lispReader) skipSpace() {
	for {
		c, _ := r.ch()
		switch {
		case isLispSpace(c):
			r.advance()
		case c == ';':
			r.comment()
		default:
			return
		}
	}
}

// comment reads a ';' comment, which extends to the end of the line.
func (r *lispReader) comment() {
	line, col, blank := r.line, r.col, r.blank
	r.advance() // ';'
	start := r.off
	for c, _ := r.ch(); c >= 0 && c != '\n'; c, _ = r.ch() {
		r.advance()
	}
	text := strings.TrimSuffix(string(r.src[start:r.off]), "\r")
	switch {
	case strings.HasPrefix(text, "go:"):
		r.dirs = append(r.dirs, lispDirective{r.p.posAt(line, col+1), blank, text})
	case col == colbase && strings.HasPrefix(text, "line "):
		// The new position base starts at the beginning of the next line.
		r.p.updateBase(MakePos(r.p.file, line+1, colbase), line, col+1+5, text[5:])
	}
}

// forms reads forms until EOF or the closing delimiter of open.
// If open is nil, forms reads until EOF.
func (r *lispReader) forms(open *lispForm) []*lispForm {
	var closer rune
	if open != nil {
		closer = map[lispKind]rune{lispList: ')', lispVector: ']', lispMap: '}', lispSet: '}'}[open.Kind]
	}
	var list []*lispForm
	for {
		r.skipSpace()
		c, _ := r.ch()
		switch {
		case c < 0:
			if open != nil {
				r.errorf(open.Pos, "%s not terminated: missing %c", open.Kind, closer)
				open.End = r.pos()
			}
			return list
		case open != nil && c == closer:
			open.End = r.pos()
			r.advance()
			return list
		case c == ')' || c == ']' || c == '}':
			r.errorf(r.pos(), "unexpected %c", c)
			r.advance()
			continue
		}
		if x := r.form(); x != nil {
			list = append(list, x)
		}
	}
}

// form reads the form at the current (non-space) position.
// It returns nil if the form was discarded with #_.
func (r *lispReader) form() *lispForm {
	r.blank = false
	pos := r.pos()
	c, _ := r.ch()
	switch c {
	case '(', '[', '{':
		x := &lispForm{Kind: map[rune]lispKind{'(': lispList, '[': lispVector, '{': lispMap}[c], Pos: pos}
		r.advance()
		x.Elems = r.forms(x)
		if x.Kind == lispMap && len(x.Elems)%2 != 0 {
			r.errorf(pos, "map must contain an even number of forms")
		}
		return x

	case '#':
		return r.dispatch()

	case '"', '`', '\'':
		return r.literal()

	case ':':
		r.advance()
		name := r.symbolText()
		if name == "" {
			r.errorf(pos, "invalid keyword: missing name after ':'")
		}
		return r.atomEnd(&lispForm{Kind: lispKeyword, Pos: pos, Text: name})
	}

	if r.atNumber() {
		return r.number()
	}
	if isLispSymbolChar(c) {
		return r.atomEnd(&lispForm{Kind: lispSymbol, Pos: pos, Text: r.symbolText()})
	}
	if msg := badChar(r.ch()); msg == "" {
		r.errorf(pos, "invalid character %#U", c)
	}
	r.advance() // reports bad characters
	return nil
}

// dispatch reads a form starting with '#': a set, a discard, or a tagged form.
func (r *lispReader) dispatch() *lispForm {
	pos := r.pos()
	r.advance() // '#'
	c, _ := r.ch()
	switch {
	case c == '{':
		x := &lispForm{Kind: lispSet, Pos: pos}
		r.advance()
		x.Elems = r.forms(x)
		return x

	case c == '_':
		r.advance()
		r.next(pos, "#_") // read and drop
		return nil

	case unicode.IsLetter(c):
		tag := r.symbolText()
		x := r.next(pos, "#"+tag)
		if x == nil {
			return nil
		}
		return &lispForm{Kind: lispTagged, Pos: pos, Text: tag, Elems: []*lispForm{x}}
	}
	r.errorf(pos, "invalid dispatch character after '#'")
	return nil
}

// next reads the form following a prefix such as #_ or #tag at pos.
func (r *lispReader) next(pos Pos, prefix string) *lispForm {
	for {
		r.skipSpace()
		c, _ := r.ch()
		if c < 0 || c == ')' || c == ']' || c == '}' {
			r.errorf(pos, "missing form after %s", prefix)
			return nil
		}
		if x := r.form(); x != nil {
			return x
		}
		// the form after the prefix was itself discarded; keep going
	}
}

// symbolText reads a (possibly empty) run of symbol characters.
func (r *lispReader) symbolText() string {
	start := r.off
	for c, _ := r.ch(); isLispSymbolChar(c); c, _ = r.ch() {
		r.advance()
	}
	return string(r.src[start:r.off])
}

// atomEnd checks that the atom x is followed by a delimiter.
func (r *lispReader) atomEnd(x *lispForm) *lispForm {
	if c, w := r.ch(); !isLispDelim(c) && badChar(c, w) == "" {
		r.errorf(r.pos(), "unexpected %q after %s", c, x.Kind)
		// skip the rest of the malformed token
		for c, _ := r.ch(); isLispSymbolChar(c); c, _ = r.ch() {
			r.advance()
		}
	}
	return x
}

// atNumber reports whether the current position starts a
// (possibly signed) number: a digit, or '.' followed by a digit.
func (r *lispReader) atNumber() bool {
	i := 0
	if c := r.peekAt(0); c == '+' || c == '-' {
		i = 1
	}
	if c := r.peekAt(i); c == '.' {
		i++
	}
	c := r.peekAt(i)
	return '0' <= c && c <= '9'
}

// number reads a number. A leading sign reads as a unary operation
// on the unsigned literal, since Go has no negative literals (SPEC A4).
func (r *lispReader) number() *lispForm {
	var sign *lispForm
	if c, _ := r.ch(); c == '+' || c == '-' {
		sign = &lispForm{Kind: lispSymbol, Pos: r.pos(), Text: string(c)}
		r.advance()
	}

	pos := r.pos()
	start := r.off
	hex := r.peekAt(0) == '0' && (r.peekAt(1) == 'x' || r.peekAt(1) == 'X')
	for {
		c := r.peekAt(0)
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_' || c == '.' {
			r.advance()
			continue
		}
		// exponent sign: e/E for decimal, p/P for hexadecimal numbers
		if (c == '+' || c == '-') && r.off > start {
			prev := rune(r.src[r.off-1])
			if !hex && (prev == 'e' || prev == 'E') || hex && (prev == 'p' || prev == 'P') {
				r.advance()
				continue
			}
		}
		break
	}
	lit := r.atomEnd(r.checkLit(pos, string(r.src[start:r.off])))
	if sign == nil {
		return lit
	}
	return &lispForm{Kind: lispList, Pos: sign.Pos, End: lit.Pos, Elems: []*lispForm{sign, lit}}
}

// literal reads a string, raw string, or rune literal.
func (r *lispReader) literal() *lispForm {
	pos := r.pos()
	start := r.off
	quote, _ := r.ch()
	r.advance()
	for {
		c, _ := r.ch()
		switch {
		case c == quote:
			r.advance()
			return r.atomEnd(r.checkLit(pos, string(r.src[start:r.off])))
		case c < 0 || c == '\n' && quote != '`':
			what := "string"
			if quote == '\'' {
				what = "rune literal"
			}
			r.errorf(pos, "%s not terminated", what)
			return &lispForm{Kind: lispLit, Pos: pos, Text: string(r.src[start:r.off]), Bad: true}
		case c == '\\' && quote != '`':
			r.advance()
			if c, _ := r.ch(); c < 0 || c == '\n' {
				continue // reported as not terminated
			}
		}
		r.advance()
	}
}

// checkLit validates the literal text at pos with Go's own scanner,
// so that go-lisp literals are exactly Go literals (SPEC §1).
func (r *lispReader) checkLit(pos Pos, text string) *lispForm {
	x := &lispForm{Kind: lispLit, Pos: pos, Text: text}

	var msg string
	var mline, mcol uint
	var s scanner
	s.init(strings.NewReader(text), func(line, col uint, m string) {
		if msg == "" {
			msg, mline, mcol = m, line, col
		}
	}, 0)
	s.next()
	x.Lit = s.kind
	if s.tok == _Literal && s.lit == text && !s.bad && msg == "" {
		return x
	}

	x.Bad = true
	switch {
	case msg == "invalid UTF-8 encoding" || msg == "invalid NUL character" || strings.HasPrefix(msg, "invalid BOM"):
		return x // already reported by advance
	case msg == "":
		msg = fmt.Sprintf("invalid literal %s", text)
	case mline == linebase:
		pos = MakePos(pos.Base(), pos.Line(), pos.Col()+mcol-colbase)
	}
	r.errorf(pos, "%s", msg)
	return x
}
