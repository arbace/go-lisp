// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build

import (
	"fmt"
	"go/token"
	"strings"
	"testing"
)

func TestReadLispInfo(t *testing.T) {
	src := `;go:build linux && !never
;go:debug panicnil=1

(package main) ; the package

#_(import "discarded")
(import "fmt"
        [str "strings"]
        [_ "embed"]
        "example.com/x/v2")

;go:embed a.txt "b c.txt"
(var -data string)

(func main [] [] (fmt.println "(import \"not\")"))
`
	info := &fileInfo{name: "x.lgo", fset: token.NewFileSet()}
	if err := readLispInfo(strings.NewReader(src), info); err != nil {
		t.Fatal(err)
	}
	if info.parseErr != nil {
		t.Fatal(info.parseErr)
	}
	if got := info.parsed.Name.Name; got != "main" {
		t.Errorf("package = %q", got)
	}
	var imports []string
	for _, imp := range info.imports {
		imports = append(imports, fmt.Sprintf("%s@%s", imp.path, info.fset.Position(imp.pos)))
	}
	if got, want := strings.Join(imports, " "), `fmt@x.lgo:7:9 strings@x.lgo:8:14 embed@x.lgo:9:12 example.com/x/v2@x.lgo:10:9`; got != want {
		t.Errorf("imports:\ngot  %s\nwant %s", got, want)
	}
	if got, want := string(info.header), "//go:build linux && !never\n//go:debug panicnil=1\n\n"; got != want {
		t.Errorf("header = %q; want %q", got, want)
	}
	if len(info.directives) != 2 || info.directives[1].Text != "//go:debug panicnil=1" || info.directives[1].Pos.Line != 2 {
		t.Errorf("directives = %v", info.directives)
	}
	var embeds []string
	for _, e := range info.embeds {
		embeds = append(embeds, fmt.Sprintf("%s@%d:%d", e.pattern, e.pos.Line, e.pos.Column))
	}
	if got, want := strings.Join(embeds, " "), "a.txt@12:11 b c.txt@12:17"; got != want {
		t.Errorf("embeds: got %s, want %s", got, want)
	}
}

func TestReadLispInfoErrors(t *testing.T) {
	for _, test := range []struct{ src, err string }{
		{"", "expected (package name)"},
		{"(func f [] [])", "expected (package name)"},
		{"(package p", "missing )"},
		{"(package p) (import fmt)", "invalid import path: fmt"},
		{`(package p) (import "C")`, "go-lisp files cannot use cgo"},
		{`(package p) (import "a b")`, `invalid import path: "a b"`},
		{`(package p) (import "fmt`, "literal not terminated"},
	} {
		info := &fileInfo{name: "x.lgo", fset: token.NewFileSet()}
		if err := readLispInfo(strings.NewReader(test.src), info); err != nil {
			t.Fatal(err)
		}
		if info.parseErr == nil || !strings.Contains(info.parseErr.Error(), test.err) {
			t.Errorf("%q: got error %v; want %q", test.src, info.parseErr, test.err)
		}
	}
}
