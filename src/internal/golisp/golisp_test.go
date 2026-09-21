// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golisp

import (
	"io"
	"strings"
	"testing"
)

func TestGoHeader(t *testing.T) {
	src := ";go:build linux\n  ; comment\n\n(package p) ; not header\n;go:build ignored\n"
	header, n := GoHeader([]byte(src))
	if got, want := string(header), "//go:build linux\n  // comment\n\n"; got != want {
		t.Errorf("header = %q; want %q", got, want)
	}
	if n != len(";go:build linux\n  ; comment\n\n") {
		t.Errorf("n = %d", n)
	}
}

func TestComments(t *testing.T) {
	src := `;a
(f ";not" "\";not" 'x' ` + "`;not`" + `) ;b
;c`
	var got []string
	for _, c := range Comments([]byte(src), len(src)) {
		got = append(got, c.Text)
	}
	if strings.Join(got, "|") != "//a|//b|//c" {
		t.Errorf("comments = %q", got)
	}
}

func TestScanner(t *testing.T) {
	src := `(a [b "c)" #_(d e) 'f'] {g h} #{i} #tag (j)) ; x
k`
	s := NewScanner([]byte(src))
	var dump func(x *Form) string
	dump = func(x *Form) string {
		if x.Kind == 'a' || x.Kind == '"' {
			return x.Text
		}
		var parts []string
		for _, e := range x.Elems {
			parts = append(parts, dump(e))
		}
		return string(x.Kind) + strings.Join(parts, " ")
	}
	var got []string
	for {
		x, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, dump(x))
	}
	if want := `(a [b "c)" 'f' {g h {i (j|k`; strings.Join(got, "|") != want {
		t.Errorf("forms = %q; want %q", strings.Join(got, "|"), want)
	}

	for _, bad := range []string{"(a", "a)", `"a`, "[a (b])"} {
		s := NewScanner([]byte(bad))
		var err error
		for err == nil {
			_, err = s.Next()
		}
		if _, ok := err.(*Error); !ok {
			t.Errorf("%q: got %v; want syntax error", bad, err)
		}
	}
}

func TestReadHeader(t *testing.T) {
	h, err := ReadHeader([]byte(`;go:build x
(package main)
(import "fmt" [s "strings"] ` + "`raw/path`" + `)
(import "os")
(func main [] [])
(import "late")`))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, imp := range h.Imports {
		paths = append(paths, imp.Path)
	}
	if h.Package != "main" || strings.Join(paths, " ") != "fmt strings raw/path os" {
		t.Errorf("got %s %q", h.Package, paths)
	}
	for _, bad := range []string{"", "(func)", "(package)", "(package p) (import fmt)"} {
		if _, err := ReadHeader([]byte(bad)); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestGoName(t *testing.T) {
	for _, test := range []struct {
		lisp   string
		member bool
		want   string
	}{
		{"test-total", false, "TestTotal"},
		{"-helper", false, "helper"},
		{"serve-HTTP", false, "ServeHTTP"},
		{"len", false, "len"},
		{"len", true, "Len"},
		{"main", false, "main"},
		{"t", true, "T"},
		{"a--b", false, ""},
		{"x?", false, ""},
	} {
		if got := GoName(test.lisp, test.member); got != test.want {
			t.Errorf("GoName(%q, %v) = %q; want %q", test.lisp, test.member, got, test.want)
		}
	}
}
