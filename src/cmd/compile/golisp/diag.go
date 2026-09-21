// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file rewrites compiler diagnostics for go-lisp files so that they
// use the names as written in the go-lisp source (read-all, -count)
// instead of the Go names (ReadAll, count). Only names are rewritten;
// expressions and types in messages keep their Go syntax.

package main

import (
	"bytes"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"cmd/compile/internal/syntax"
)

// lispSpellings returns, for the go-lisp source src, the map from the Go
// name of every identifier to its spelling in the source, where the two
// differ. A Go name written several ways maps to its most frequent spelling.
func lispSpellings(file string, src []byte) map[string]string {
	f, err := syntax.ParseLisp(syntax.NewFileBase(file), bytes.NewReader(src), nil, nil, 0)
	if err != nil || f == nil {
		return nil
	}
	lines := bytes.Split(src, []byte("\n"))
	counts := map[string]map[string]int{}
	syntax.Inspect(f, func(n syntax.Node) bool {
		name, ok := n.(*syntax.Name)
		if !ok {
			return true
		}
		pos := name.Pos()
		if pos.Line() == 0 || int(pos.Line()) > len(lines) {
			return true
		}
		line := lines[pos.Line()-1]
		col := int(pos.Col()) - 1
		if col < 0 || col >= len(line) {
			return true
		}
		spelling := lispToken(line[col:])
		if spelling == "" || spelling == name.Value {
			return true
		}
		if counts[name.Value] == nil {
			counts[name.Value] = map[string]int{}
		}
		counts[name.Value][spelling]++
		return true
	})
	spell := map[string]string{}
	for g, m := range counts {
		best, n := "", 0
		for s, c := range m {
			if c > n || c == n && s < best {
				best, n = s, c
			}
		}
		spell[g] = best
	}
	return spell
}

// lispToken returns the name at the start of b: a run of identifier
// characters and '-', without a keyword's leading ':' or a dotted
// symbol's following segments.
func lispToken(b []byte) string {
	b = bytes.TrimPrefix(b, []byte(":"))
	n := 0
	for n < len(b) {
		r, w := utf8.DecodeRune(b[n:])
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			break
		}
		n += w
	}
	return string(b[:n])
}

// lispDiagRewriter rewrites the names in compiler diagnostics
// for the go-lisp files among files.
type lispDiagRewriter struct {
	spell map[string]map[string]string // file name -> Go name -> spelling
}

func newLispDiagRewriter(files []string) *lispDiagRewriter {
	r := &lispDiagRewriter{spell: map[string]map[string]string{}}
	for _, file := range files {
		if !syntax.IsLispFile(file) {
			continue
		}
		if src, err := os.ReadFile(file); err == nil {
			r.spell[file] = lispSpellings(file, src)
		}
	}
	return r
}

// rewrite rewrites one line of compiler output. Lines that report a
// position in a go-lisp file ("file.lgo:line:col: message") have the
// identifiers in their message replaced by their go-lisp spellings;
// quoted text is left alone.
func (r *lispDiagRewriter) rewrite(line string) string {
	for file, spell := range r.spell {
		if !strings.HasPrefix(line, file+":") {
			continue
		}
		// skip "file:line:col: "
		rest := line[len(file)+1:]
		i := strings.Index(rest, ": ")
		if i < 0 {
			return line
		}
		prefix := line[:len(file)+1+i+2]
		return prefix + lispRewriteNames(rest[i+2:], spell)
	}
	return line
}

// lispRewriteNames replaces whole-word identifiers in msg that have
// a go-lisp spelling, outside of quoted text ("..." and `...`).
func lispRewriteNames(msg string, spell map[string]string) string {
	var b strings.Builder
	var quote rune
	for i := 0; i < len(msg); {
		r, w := utf8.DecodeRuneInString(msg[i:])
		switch {
		case quote != 0:
			if r == '\\' && quote == '"' && i+w < len(msg) {
				_, w2 := utf8.DecodeRuneInString(msg[i+w:])
				b.WriteString(msg[i : i+w+w2])
				i += w + w2
				continue
			}
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '`':
			quote = r
		case (unicode.IsLetter(r) || r == '_') && !lispAfterIdentChar(msg, i):
			j := i
			for j < len(msg) {
				r2, w2 := utf8.DecodeRuneInString(msg[j:])
				if !(unicode.IsLetter(r2) || unicode.IsDigit(r2) || r2 == '_') {
					break
				}
				j += w2
			}
			word := msg[i:j]
			if s, ok := spell[word]; ok {
				word = s
			}
			b.WriteString(word)
			i = j
			continue
		}
		b.WriteString(msg[i : i+w])
		i += w
	}
	return b.String()
}

// lispAfterIdentChar reports whether msg[i] follows an identifier
// character (as in 1x), so that it does not start a word.
func lispAfterIdentChar(msg string, i int) bool {
	r, _ := utf8.DecodeLastRuneInString(msg[:i])
	return i > 0 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
}
