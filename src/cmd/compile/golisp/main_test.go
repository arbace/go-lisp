// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"internal/testenv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cmd/compile/internal/syntax"
)

const helloLisp = `(package main)

(import "fmt" "os" "strings")

;go:noinline
(func [g (* greeter)] greet [] [string]
  (++ g.-count)
  (return (fmt.sprintf "hello, %s! (#%d)" (strings.to-upper g.name) g.-count)))

(func main [] []
  (:= -g (& (:lit greeter (:kv :name (:index os.args 1)))))
  (for [(:= -i 0) (< -i (count)) (++ -i)]
    (fmt.println (-g.greet))))
`

// The Go half of a mixed go-lisp and Go main package.
const helloGo = `package main

type Greeter struct {
	Name  string
	count int
}

func Count() int { return 2 }
`

func TestConvert(t *testing.T) {
	goSrc, err := lispToGo("hello.lgo", []byte(helloLisp))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"//go:noinline\nfunc (G *Greeter) Greet() string {", "G.count++", "strings.ToUpper(G.Name)"} {
		if !strings.Contains(string(goSrc), want) {
			t.Errorf("lisp2go output does not contain %q:\n%s", want, goSrc)
		}
	}

	// go2lisp(lisp2go(x)) == go2lisp(x) for Go x, since the go-lisp
	// printer depends only on the syntax tree.
	lisp1, err := syntax.GoToLisp("x.go", bytes.NewReader(goSrc))
	if err != nil {
		t.Fatal(err)
	}
	goSrc2, err := lispToGo("x.lgo", lisp1)
	if err != nil {
		t.Fatal(err)
	}
	lisp2, err := syntax.GoToLisp("x.go", bytes.NewReader(goSrc2))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lisp1, lisp2) {
		t.Errorf("conversion is not stable:\n%s\n---\n%s", lisp1, lisp2)
	}
}

func TestDroppedComments(t *testing.T) {
	src := "//go:build linux\n\n// Package p.\npackage p /* x */\n\n//go:noinline\nfunc f() {} // f\n"
	if n := droppedComments("x.go", []byte(src)); n != 3 {
		t.Errorf("got %d dropped comments; want 3", n)
	}
}

func TestBuildAndRun(t *testing.T) {
	testenv.MustHaveGoBuild(t)
	dir := t.TempDir()
	lisp := filepath.Join(dir, "hello.lgo")
	gofile := filepath.Join(dir, "greeter.go")
	if err := os.WriteFile(lisp, []byte(helloLisp), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gofile, []byte(helloGo), 0o666); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "hello")
	if err := build(testenv.GoToolPath(t), []string{lisp, gofile}, exe, true); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(exe, "go-lisp").CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if want := "hello, GO-LISP! (#1)\nhello, GO-LISP! (#2)\n"; string(out) != want {
		t.Errorf("got %q; want %q", out, want)
	}
}

func TestBuildErrors(t *testing.T) {
	testenv.MustHaveGoBuild(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "p.lgo")
	if err := os.WriteFile(file, []byte("(package p)\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	err := build(testenv.GoToolPath(t), []string{file}, filepath.Join(dir, "p"), true)
	if err == nil || !strings.Contains(err.Error(), "package p is not a main package") {
		t.Errorf("got %v; want error about package p", err)
	}
}

func TestLispSpellings(t *testing.T) {
	src := []byte(`(package main)
(import "fmt" [str "strings"])
(type greeter (struct [name string] [-count int]))
(func [g (* greeter)] greet [] [] (++ g.-count) (fmt.println (str.to-upper g.name)))
(func main [] [] (:= -x (:lit greeter (:kv :name "a"))) (-x.greet) (read-all) (ReadAll))
`)
	got := lispSpellings("x.lgo", src)
	want := map[string]string{
		"Greeter": "greeter", "Name": "name", "count": "-count", "G": "g",
		"Greet": "greet", "Println": "println", "ToUpper": "to-upper",
		"x": "-x", "ReadAll": "read-all", // read-all and ReadAll: a tie, the smaller spelling wins
	}
	for g, s := range want {
		if got[g] != s {
			t.Errorf("spelling of %s: got %q, want %q", g, got[g], s)
		}
	}
	for _, g := range []string{"fmt", "str", "main", "string", "int"} {
		if s, ok := got[g]; ok {
			t.Errorf("%s has spelling %q; want none (same as Go)", g, s)
		}
	}

	r := &lispDiagRewriter{spell: map[string]map[string]string{"x.lgo": got}}
	for _, test := range []struct{ in, want string }{
		{`x.lgo:5:3: invalid operation: x + "count" (mismatched types *Greeter and untyped string)`,
			`x.lgo:5:3: invalid operation: -x + "count" (mismatched types *greeter and untyped string)`},
		{`x.lgo:4:40: G.count undefined (type Greeter has no field or method count, but does have field Count)`,
			`x.lgo:4:40: g.-count undefined (type greeter has no field or method -count, but does have field Count)`},
		{`x.lgo:1:1: 1x count`, `x.lgo:1:1: 1x -count`},
		{`other.go:1:1: x count`, `other.go:1:1: x count`},
	} {
		if got := r.rewrite(test.in); got != test.want {
			t.Errorf("rewrite(%q):\ngot  %q\nwant %q", test.in, got, test.want)
		}
	}
}

func TestBuildErrorNames(t *testing.T) {
	testenv.MustHaveGoBuild(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "bad.lgo")
	src := "(package main)\n(func main [] []\n  (:= -my-count 1)\n  (:= -s (+ -my-count \"a\")))\n"
	if err := os.WriteFile(file, []byte(src), 0o666); err != nil {
		t.Fatal(err)
	}
	// build writes the compiler's messages to os.Stderr; capture them.
	stderr := os.Stderr
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = wr
	err = build(testenv.GoToolPath(t), []string{file}, filepath.Join(dir, "bad"), true)
	wr.Close()
	os.Stderr = stderr
	var msgs bytes.Buffer
	msgs.ReadFrom(rd)
	if err == nil {
		t.Fatal("build succeeded; want a type error")
	}
	if !strings.Contains(msgs.String(), `-my-count + "a"`) {
		t.Errorf("error does not use go-lisp names:\n%s", msgs.String())
	}
}
