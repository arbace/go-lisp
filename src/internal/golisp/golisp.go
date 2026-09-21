// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package golisp reads the parts of go-lisp source files that the go
// command and go/build need: the leading comments (build constraints and
// directives), the package clause, the imports, ;go:embed comments, and
// the top-level functions of test files.
//
// go-lisp is Go written as s-expressions; see golisp/DESIGN.md and
// golisp/SPEC.md. The compiler has its own complete parser
// (cmd/compile/internal/syntax); this package knows just enough of the
// reader syntax to find form boundaries.
package golisp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsFile reports whether name is a go-lisp source file.
func IsFile(name string) bool {
	return strings.HasSuffix(name, ".lgo")
}

// GoHeader returns the leading comment lines and blank lines of src with
// ';' comments turned into '//' comments, as a Go file's header would be
// (so ;go:build lines become //go:build lines), and the length of those
// lines in src.
func GoHeader(src []byte) (header []byte, n int) {
	var b bytes.Buffer
	for data := src; len(data) > 0; {
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line = data[:i+1]
		}
		trimmed := bytes.TrimLeft(line, " \t\r,")
		switch {
		case len(bytes.TrimSpace(trimmed)) == 0:
			b.Write(line)
		case trimmed[0] == ';':
			b.Write(line[:len(line)-len(trimmed)])
			b.WriteString("//")
			b.Write(trimmed[1:])
		default:
			return b.Bytes(), n
		}
		data = data[len(line):]
		n += len(line)
	}
	return b.Bytes(), n
}

// A Comment is a ';' comment.
type Comment struct {
	Off  int    // offset of the ';'
	Text string // comment text with "//" in place of ";", without the line end
}

// Comments returns the ';' comments in src[:end] (outside of literals).
func Comments(src []byte, end int) []Comment {
	s := &Scanner{src: src}
	var list []Comment
	for s.off < end && s.off < len(src) {
		switch src[s.off] {
		case ';':
			start := s.off
			for s.off < len(src) && src[s.off] != '\n' {
				s.off++
			}
			text := strings.TrimSuffix(string(src[start+1:s.off]), "\r")
			list = append(list, Comment{start, "//" + text})
		case '"', '`', '\'':
			if s.skipQuoted() != nil {
				return list
			}
		default:
			s.off++
		}
	}
	return list
}

// A Form is a form read by a Scanner.
type Form struct {
	Kind  byte   // '(' list, '[' vector, '{' map or set, '"' string, 'a' atom (symbol, keyword, number, rune)
	Text  string // source text of strings and atoms
	Off   int    // offset of the form's first character
	Elems []*Form
}

// Head returns the atom at the head of a list, or "".
func (x *Form) Head() string {
	if x.Kind == '(' && len(x.Elems) > 0 && x.Elems[0].Kind == 'a' {
		return x.Elems[0].Text
	}
	return ""
}

// An Error is a syntax error at an offset.
type Error struct {
	Off int
	Msg string
}

func (e *Error) Error() string { return e.Msg }

// A Scanner reads forms from go-lisp source.
type Scanner struct {
	src []byte
	off int
}

// NewScanner returns a scanner of src.
func NewScanner(src []byte) *Scanner {
	return &Scanner{src: src}
}

func (s *Scanner) errorf(off int, format string, args ...any) error {
	return &Error{off, fmt.Sprintf(format, args...)}
}

// Next returns the next form, or io.EOF at the end of the source.
func (s *Scanner) Next() (*Form, error) {
	if err := s.skipSpace(); err != nil {
		return nil, err
	}
	if s.off >= len(s.src) {
		return nil, io.EOF
	}
	start := s.off
	switch c := s.src[s.off]; c {
	case '(', '[', '{':
		closer := map[byte]byte{'(': ')', '[': ']', '{': '}'}[c]
		s.off++
		x := &Form{Kind: c, Off: start}
		for {
			if err := s.skipSpace(); err != nil {
				return nil, err
			}
			if s.off >= len(s.src) {
				return nil, s.errorf(start, "missing %c", closer)
			}
			if s.src[s.off] == closer {
				s.off++
				return x, nil
			}
			e, err := s.Next()
			if err != nil {
				if err == io.EOF {
					err = s.errorf(start, "missing %c", closer)
				}
				return nil, err
			}
			x.Elems = append(x.Elems, e)
		}
	case ')', ']', '}':
		return nil, s.errorf(start, "unexpected %c", c)
	case '"', '`', '\'':
		if err := s.skipQuoted(); err != nil {
			return nil, err
		}
		kind := byte('"')
		if c == '\'' {
			kind = 'a'
		}
		return &Form{Kind: kind, Text: string(s.src[start:s.off]), Off: start}, nil
	case '#':
		if s.off+1 < len(s.src) && s.src[s.off+1] == '{' {
			s.off++ // a set reads like a map
			x, err := s.Next()
			if x != nil {
				x.Off = start
			}
			return x, err
		}
		// a tagged form: the tag, then the form
		s.off++
		s.atom()
		return s.Next()
	}
	s.atom()
	if s.off == start {
		return nil, s.errorf(start, "invalid character %q", s.src[start])
	}
	return &Form{Kind: 'a', Text: string(s.src[start:s.off]), Off: start}, nil
}

// skipSpace skips whitespace, commas, comments, and #_ discarded forms.
func (s *Scanner) skipSpace() error {
	for s.off < len(s.src) {
		switch c := s.src[s.off]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			s.off++
		case c == ';':
			for s.off < len(s.src) && s.src[s.off] != '\n' {
				s.off++
			}
		case c == '#' && s.off+1 < len(s.src) && s.src[s.off+1] == '_':
			s.off += 2
			if _, err := s.Next(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

// atom advances past a symbol, keyword, or number.
func (s *Scanner) atom() {
	for s.off < len(s.src) {
		switch s.src[s.off] {
		case ' ', '\t', '\n', '\r', ',', ';', '(', ')', '[', ']', '{', '}', '"', '`', '\'':
			return
		}
		s.off++
	}
}

var errUnterminated = errors.New("literal not terminated")

// skipQuoted advances past a string, raw string, or rune literal.
func (s *Scanner) skipQuoted() error {
	start := s.off
	quote := s.src[s.off]
	s.off++
	for s.off < len(s.src) {
		c := s.src[s.off]
		switch {
		case c == quote:
			s.off++
			return nil
		case c == '\\' && quote != '`':
			s.off++
		case c == '\n' && quote != '`':
			return s.errorf(start, "%v", errUnterminated)
		}
		s.off++
	}
	return s.errorf(start, "%v", errUnterminated)
}

// A Header is the package clause and imports of a go-lisp file.
type Header struct {
	Package    string
	PackageOff int // offset of the package name
	Imports    []Import
}

// An Import is an import spec.
type Import struct {
	Path string // unquoted import path
	Off  int    // offset of the path literal
}

// ReadHeader reads the package clause and the imports of src.
// It does not validate import paths.
func ReadHeader(src []byte) (*Header, error) {
	s := NewScanner(src)
	x, err := s.Next()
	if err == io.EOF || err == nil && (x.Head() != "package" || len(x.Elems) != 2 || x.Elems[1].Kind != 'a') {
		off := len(src)
		if x != nil {
			off = x.Off
		}
		return nil, s.errorf(off, "expected (package name)")
	}
	if err != nil {
		return nil, err
	}
	h := &Header{Package: x.Elems[1].Text, PackageOff: x.Elems[1].Off}
	for {
		x, err := s.Next()
		if err == io.EOF {
			return h, nil
		}
		if err != nil {
			return h, err
		}
		if x.Head() != "import" {
			return h, nil // imports come first, as in Go
		}
		for _, spec := range x.Elems[1:] {
			lit := spec
			if spec.Kind == '[' && len(spec.Elems) == 2 {
				lit = spec.Elems[1]
			}
			path, err := strconv.Unquote(lit.Text)
			if lit.Kind != '"' || err != nil {
				return h, s.errorf(lit.Off, "invalid import path: %s", lit.Text)
			}
			h.Imports = append(h.Imports, Import{path, lit.Off})
		}
	}
}

// predeclared is the frozen list of predeclared names that bare go-lisp
// names map to verbatim (golisp/SPEC.md F15). It must match
// lispPredeclared in cmd/compile/internal/syntax/lisp_names.go.
var predeclared = map[string]bool{
	"any": true, "append": true, "bool": true, "byte": true, "cap": true,
	"clear": true, "close": true, "comparable": true, "complex": true,
	"complex128": true, "complex64": true, "copy": true, "delete": true,
	"error": true, "false": true, "float32": true, "float64": true,
	"imag": true, "int": true, "int16": true, "int32": true, "int64": true,
	"int8": true, "iota": true, "len": true, "make": true, "max": true,
	"min": true, "new": true, "nil": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true, "rune": true,
	"string": true, "true": true, "uint": true, "uint16": true,
	"uint32": true, "uint64": true, "uint8": true, "uintptr": true,
	"main": true, "init": true,
}

// GoName returns the Go name of the go-lisp name s (golisp/SPEC.md §6):
// kebab-case, exported by default, '-' for unexported. Bare names (not
// member names) that are predeclared, main, or init map to themselves;
// import names are not known here and are mapped like other names.
// GoName returns "" if s is not a valid go-lisp name.
func GoName(s string, member bool) string {
	if s == "_" || !member && predeclared[s] {
		return s
	}
	exported := true
	if strings.HasPrefix(s, "-") {
		exported = false
		s = s[1:]
	}
	var b strings.Builder
	for i, seg := range strings.Split(s, "-") {
		if seg == "" {
			return ""
		}
		if i > 0 || exported {
			seg = upperFirst(seg)
		}
		b.WriteString(seg)
	}
	g := b.String()
	for i, r := range g {
		if !(unicode.IsLetter(r) || r == '_' || i > 0 && unicode.IsDigit(r)) {
			return ""
		}
	}
	return g
}

func upperFirst(s string) string {
	r, w := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[w:]
}
