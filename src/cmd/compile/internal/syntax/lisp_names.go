// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements the go-lisp name mapping (golisp/DESIGN.md §4.2,
// golisp/SPEC.md §6): Lisp names are kebab-case and exported by default,
// and a leading '-' makes a name unexported.

package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// lispGoKeywords are Go's keywords. A bare Lisp symbol that is a
// Go keyword always denotes the keyword, never a name (SPEC I3, A1).
var lispGoKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// lispPredeclared is the frozen list of predeclared names that bare
// Lisp names map to verbatim (SPEC F15). It must not follow the
// toolchain's universe scope, so that new builtins cannot silently
// change the meaning of existing go-lisp code.
var lispPredeclared = map[string]bool{
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
}

// A lispNamer maps names between go-lisp and Go for one file.
type lispNamer struct {
	imports map[string]bool // the file's import names (SPEC A12)
}

// newLispNamer returns a namer for a file with the given import names.
func newLispNamer(imports []string) *lispNamer {
	n := &lispNamer{imports: make(map[string]bool)}
	for _, name := range imports {
		n.imports[name] = true
	}
	return n
}

// exempt reports whether the bare Lisp name s maps to itself.
func (n *lispNamer) exempt(s string) bool {
	return lispPredeclared[s] || s == "main" || s == "init" || n.imports[s]
}

// goName maps the Lisp name s to a Go identifier. Member names
// (selectors, field and method names, keyword keys) have no
// exemptions; bare names may not be Go keywords.
func (n *lispNamer) goName(s string, member bool) (string, error) {
	if s == "_" {
		return s, nil
	}
	if !member {
		if lispGoKeywords[s] {
			return "", fmt.Errorf("keyword %s cannot be used as a name", s)
		}
		if n.exempt(s) {
			return s, nil
		}
	}

	exported := true
	name := s
	if strings.HasPrefix(name, "-") {
		exported = false
		name = name[1:]
	}

	// kebab join: capitalize the first rune of every segment but the first
	var b strings.Builder
	for i, seg := range strings.Split(name, "-") {
		if seg == "" {
			return "", fmt.Errorf("invalid name %s: empty segment", s)
		}
		if i > 0 {
			seg = lispUpperFirst(seg)
		}
		b.WriteString(seg)
	}
	g := b.String()
	if exported {
		g = lispUpperFirst(g)
	}
	if !lispIsIdent(g) {
		return "", fmt.Errorf("invalid name %s: %s is not a Go identifier", s, g)
	}
	return g, nil
}

// lispName returns the Lisp spelling of the Go identifier g.
// If decl is set, g is being declared (not referenced); a bare
// declaration of a predeclared name needs a '-' (SPEC F16).
// If head is set, the name is at the head of a list (an
// interface method name), where Go keywords are special even
// for member names (SPEC F1).
func (n *lispNamer) lispName(g string, member, decl, head bool) string {
	if g == "_" {
		return g
	}
	ok := func(s string) bool {
		if (!member || head) && lispGoKeywords[s] {
			return false
		}
		if !member && decl && lispPredeclared[s] {
			return false
		}
		got, err := n.goName(s, member)
		return err == nil && got == g
	}
	k := lispKebab(g)
	for _, s := range []string{k, "-" + k, g, "-" + g} {
		if ok(s) {
			return s
		}
	}
	// unreachable for valid identifiers: "-"+g always maps back to g
	panic(fmt.Sprintf("no go-lisp spelling for %q", g))
}

// lispKebab converts a Go identifier to kebab-case: words are split at
// camel-case boundaries and lower-cased, except acronyms (words without
// lower-case letters, like HTTP), which keep their case.
// For example ReadAll becomes read-all and ServeHTTP becomes serve-HTTP.
func lispKebab(g string) string {
	rs := []rune(g)
	var words []string
	start := 0
	for i := 1; i < len(rs); i++ {
		if !unicode.IsUpper(rs[i]) {
			continue
		}
		prev := rs[i-1]
		nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
		if unicode.IsLower(prev) || unicode.IsDigit(prev) || unicode.IsUpper(prev) && nextLower {
			words = append(words, string(rs[start:i]))
			start = i
		}
	}
	words = append(words, string(rs[start:]))
	for i, w := range words {
		if strings.IndexFunc(w, unicode.IsLower) >= 0 || utf8.RuneCountInString(w) == 1 {
			words[i] = lispLowerFirst(w)
		}
	}
	return strings.Join(words, "-")
}

func lispUpperFirst(s string) string {
	r, w := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[w:]
}

func lispLowerFirst(s string) string {
	r, w := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[w:]
}

// lispIsIdent reports whether s is a Go identifier.
func lispIsIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if !(unicode.IsLetter(r) || r == '_' || i > 0 && unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

// lispImportName returns the name an import introduces into the file
// scope for go-lisp name mapping, or "" if it introduces none: the
// explicit alias, or else a name guessed from the path (SPEC F17).
// Dot and blank imports introduce no name.
func lispImportName(alias, path string) string {
	switch alias {
	case "":
		return lispGuessImportName(path)
	case ".", "_":
		return ""
	}
	return alias
}

// lispGuessImportName guesses the package name of an import path:
// the last path element, or the element before it if the last one is
// a major version (vN); a ".vN" suffix is removed. If the guess is not
// a valid identifier, it returns "".
func lispGuessImportName(path string) string {
	elems := strings.Split(path, "/")
	name := elems[len(elems)-1]
	if lispIsMajorVersion(name) && len(elems) > 1 {
		name = elems[len(elems)-2]
	}
	if i := strings.LastIndex(name, ".v"); i >= 0 && lispIsMajorVersion(name[i+1:]) {
		name = name[:i]
	}
	if !lispIsIdent(name) {
		return ""
	}
	return name
}

func lispIsMajorVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	_, err := strconv.ParseUint(s[1:], 10, 64)
	return err == nil
}
