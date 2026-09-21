// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"internal/testenv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cmd/compile/internal/syntax"
)

// TestVimIndent checks that the Vim indentation of golisp/vim reproduces
// the layout of the go-lisp printer: it removes the indentation of
// printed go-lisp files and has Vim re-indent them.
func TestVimIndent(t *testing.T) {
	vim, err := exec.LookPath("vim")
	if err != nil {
		t.Skip("vim not found")
	}
	goroot := testenv.GOROOT(t)
	plugin := filepath.Join(goroot, "golisp", "vim")
	for _, file := range []string{
		filepath.Join(goroot, "src", "cmd", "compile", "internal", "syntax", "testdata", "lisp", "print.go"),
		filepath.Join(goroot, "src", "strings", "builder.go"),
		filepath.Join(goroot, "src", "sort", "sort.go"),
		filepath.Join(goroot, "src", "container", "list", "list.go"),
		filepath.Join(goroot, "src", "unicode", "utf8", "utf8.go"),
		filepath.Join(goroot, "src", "cmd", "go", "internal", "run", "run.go"), // multi-line raw strings
	} {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			want, err := syntax.GoToLisp(file, bytes.NewReader(src))
			if err != nil {
				t.Fatal(err)
			}
			lgo := filepath.Join(t.TempDir(), "x.lgo")
			if err := os.WriteFile(lgo, lispDedent(want), 0o666); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(vim, "-N", "-u", "NONE", "-i", "NONE", "-es",
				"-c", "set rtp^="+plugin, "-c", "filetype plugin indent on", "-c", "syntax on",
				"-c", "edit "+lgo, "-c", "normal! gg=G", "-c", "wq")
			cmd.Stdin = strings.NewReader("")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("vim: %v\n%s", err, out)
			}
			got, err := os.ReadFile(lgo)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				g, w := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
				for i := range min(len(g), len(w)) {
					if g[i] != w[i] {
						t.Fatalf("line %d:\nvim     %q\nprinter %q", i+1, g[i], w[i])
					}
				}
				t.Fatalf("lengths differ: %d vs %d lines", len(g), len(w))
			}
		})
	}
}

// lispDedent removes the indentation of all lines of go-lisp source
// except those that continue a multi-line raw string.
func lispDedent(src []byte) []byte {
	var b bytes.Buffer
	state := byte(0) // 0, '"', '\'', '`', or ';'
	lineStart := true
	for i := 0; i < len(src); i++ {
		c := src[i]
		if lineStart && state != '`' && (c == ' ' || c == '\t') {
			continue
		}
		lineStart = false
		b.WriteByte(c)
		switch {
		case state == 0 && (c == '"' || c == '\'' || c == '`' || c == ';'):
			state = c
		case state == ';' && c == '\n':
			state = 0
		case (state == '"' || state == '\'') && c == '\\' && i+1 < len(src):
			i++
			b.WriteByte(src[i])
		case (state == '"' || state == '\'' || state == '`') && c == state:
			state = 0
		}
		if c == '\n' {
			lineStart = true
		}
	}
	return b.Bytes()
}

// TestVimSyntax checks the highlighting of golisp/vim for some tokens.
func TestVimSyntax(t *testing.T) {
	vim, err := exec.LookPath("vim")
	if err != nil {
		t.Skip("vim not found")
	}
	plugin := filepath.Join(testenv.GOROOT(t), "golisp", "vim")
	src := `;go:build linux
(package main)
;; comment
(import "fmt")
(func [-r (* rect)] area [] [float64]
  (:= -x (:index -xs 0))
  (if (== -x nil) (return (len "a\tb")))
  (+= -n 0x1p-2)
  (fmt.println 'x' ` + "`raw`" + ` (:lit :_ (:kv :w 1)) true))
(type point (struct [x int]))
`
	file := filepath.Join(t.TempDir(), "x.lgo")
	if err := os.WriteFile(file, []byte(src), 0o666); err != nil {
		t.Fatal(err)
	}
	tokens := []struct{ pat, group string }{
		{";go:build", "golispDirective"},
		{"package", "golispKeyword"},
		{";; comment", "golispComment"},
		{`"fmt"`, "golispString"},
		{"area", "golispFuncName"},
		{"float64", "golispType"},
		{":=", "golispForm"},
		{":index", "golispForm"},
		{"nil", "golispConstant"},
		{"==", "golispOperator"},
		{"return", "golispKeyword"},
		{"len", "golispBuiltin"},
		{`\t`, "golispEscape"},
		{"+=", "golispOperator"},
		{"0x1p-2", "golispNumber"},
		{"'x'", "golispRune"},
		{"`raw`", "golispRawString"},
		{":_", "golispOmitted"},
		{":w", "golispForm"},
		{"true", "golispConstant"},
		{"point", "golispTypeName"},
		{"struct", "golispTypeKeyword"},
	}
	var script strings.Builder
	fmt.Fprintf(&script, "set rtp^=%s\nfiletype plugin indent on\nsyntax on\nedit %s\nredir! > %s.out\n", plugin, file, file)
	for _, tok := range tokens {
		fmt.Fprintf(&script, "call cursor(1, 1) | let [l, c] = searchpos('\\V%s', 'cW') | echo synIDattr(synID(l, c, 1), 'name')\n",
			strings.ReplaceAll(strings.ReplaceAll(tok.pat, `\`, `\\`), "'", "''"))
	}
	script.WriteString("redir END\nqall!\n")
	vimscript := file + ".vim"
	if err := os.WriteFile(vimscript, []byte(script.String()), 0o666); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(vim, "-N", "-u", "NONE", "-i", "NONE", "-es", "-S", vimscript)
	cmd.Stdin = strings.NewReader("")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vim: %v\n%s", err, out)
	}
	out, err := os.ReadFile(file + ".out")
	if err != nil {
		t.Fatal(err)
	}
	groups := strings.Fields(string(out))
	if len(groups) != len(tokens) {
		t.Fatalf("got %d groups for %d tokens: %q", len(groups), len(tokens), groups)
	}
	for i, tok := range tokens {
		if groups[i] != tok.group {
			t.Errorf("%s: highlighted as %s; want %s", tok.pat, groups[i], tok.group)
		}
	}
}
