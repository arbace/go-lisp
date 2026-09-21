// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syntax

import (
	"strings"
	"testing"
)

func TestLispGoName(t *testing.T) {
	n := newLispNamer([]string{"fmt", "str"})
	for _, test := range []struct {
		lisp   string
		member bool
		want   string // "" means error
	}{
		{"_", false, "_"},
		{"rect", false, "Rect"},
		{"-helper", false, "helper"},
		{"read-all", false, "ReadAll"},
		{"-read-all", false, "readAll"},
		{"serve-HTTP", false, "ServeHTTP"},
		{"HTTP-client", false, "HTTPClient"},
		{"EOF", false, "EOF"},
		{"ReadAll", false, "ReadAll"},
		{"sha256-sum", false, "Sha256Sum"},
		{"_x", false, "_x"},
		{"-_x", false, "_x"},
		{"变量", false, "变量"},
		{"größe", false, "Größe"},

		// exemptions apply to bare names only
		{"len", false, "len"},
		{"-len", false, "len"},
		{"Len", false, "Len"},
		{"len", true, "Len"},
		{"main", false, "main"},
		{"init", false, "init"},
		{"nil", false, "nil"},
		{"fmt", false, "fmt"},
		{"str", false, "str"},
		{"fmt", true, "Fmt"},
		{"println", true, "Println"},

		// keywords are not bare names, but are fine as members
		{"map", false, ""},
		{"type", false, ""},
		{"else", false, ""},
		{"Map", false, "Map"},
		{"type", true, "Type"},
		{"-type", true, "type"},

		// invalid spellings
		{"a--b", false, ""},
		{"a-", false, ""},
		{"-", false, ""},
		{"--x", false, ""},
		{"empty?", false, ""},
		{"a.b", false, ""},
		{"1x", false, ""},
	} {
		got, err := n.goName(test.lisp, test.member)
		if test.want == "" {
			if err == nil {
				t.Errorf("goName(%q, member=%v) = %q; want error", test.lisp, test.member, got)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Errorf("goName(%q, member=%v) = %q, %v; want %q", test.lisp, test.member, got, err, test.want)
		}
	}
}

func TestLispName(t *testing.T) {
	n := newLispNamer([]string{"fmt"})
	for _, test := range []struct {
		goName             string
		member, decl, head bool
		want               string
	}{
		{"_", false, false, false, "_"},
		{"ReadAll", false, false, false, "read-all"},
		{"readAll", false, false, false, "-read-all"},
		{"ServeHTTP", false, false, false, "serve-HTTP"},
		{"HTTPClient", false, false, false, "HTTP-client"},
		{"EOF", false, false, false, "EOF"},
		{"URL", true, false, false, "URL"},
		{"X", false, false, false, "x"},
		{"x", false, false, false, "-x"},
		{"err", false, false, false, "-err"},
		{"Sha256Sum", false, false, false, "sha256-sum"},
		{"MAX_SIZE", false, false, false, "MAX_SIZE"},
		{"max_size", false, false, false, "-max_size"},
		{"变量", false, false, false, "变量"},

		// predeclared names: verbatim when referenced, '-' when declared (F16)
		{"len", false, false, false, "len"},
		{"len", false, true, false, "-len"},
		{"string", false, false, false, "string"},
		{"New", false, false, false, "New"},
		{"Int64", false, false, false, "Int64"},
		{"Int64", true, false, false, "int64"},
		{"String", true, false, false, "string"},
		{"Error", true, false, false, "error"},

		// main, init, and import names may be declared bare
		{"main", false, true, false, "main"},
		{"init", false, true, false, "init"},
		{"fmt", false, false, false, "fmt"},
		{"fmt", false, true, false, "fmt"},

		// keywords
		{"Map", false, false, false, "Map"},
		{"Type", true, false, false, "type"},
		{"Type", true, false, true, "Type"}, // interface method head (F1)
		{"type", true, false, false, "-type"},

		// members
		{"buf", true, false, false, "-buf"},
		{"Println", true, false, false, "println"},
		{"init", true, false, false, "-init"},
	} {
		got := n.lispName(test.goName, test.member, test.decl, test.head)
		if got != test.want {
			t.Errorf("lispName(%q, member=%v, decl=%v, head=%v) = %q; want %q",
				test.goName, test.member, test.decl, test.head, got, test.want)
		}
	}
}

func TestLispImportName(t *testing.T) {
	for _, test := range []struct{ alias, path, want string }{
		{"", "fmt", "fmt"},
		{"", "net/http", "http"},
		{"", "math/rand/v2", "rand"},
		{"", "gopkg.in/yaml.v3", "yaml"},
		{"", "example.com/go-yaml", ""},
		{"", "example.com/v2", ""}, // "example.com" is not an identifier
		{"", "C", "C"},
		{"str", "strings", "str"},
		{".", "strings", ""},
		{"_", "embed", ""},
	} {
		if got := lispImportName(test.alias, test.path); got != test.want {
			t.Errorf("lispImportName(%q, %q) = %q; want %q", test.alias, test.path, got, test.want)
		}
	}
}

// FuzzLispNames checks that every Go identifier has a go-lisp spelling
// in every position that maps back to it.
func FuzzLispNames(f *testing.F) {
	for _, s := range []string{"x", "X", "ReadAll", "readAll", "ServeHTTP", "HTTPClient", "EOF",
		"len", "Len", "main", "fmt", "Map", "type", "_x", "x_Y", "IPv6Addr", "变量", "Größe", "a1B2"} {
		f.Add(s)
	}
	n := newLispNamer([]string{"fmt", "os"})
	f.Fuzz(func(t *testing.T, g string) {
		if !lispIsIdent(g) || lispGoKeywords[g] {
			return
		}
		for _, c := range []struct{ member, decl, head bool }{
			{false, false, false}, {false, true, false}, {true, false, false}, {true, false, true},
		} {
			s := n.lispName(g, c.member, c.decl, c.head)
			got, err := n.goName(s, c.member)
			if err != nil || got != g {
				t.Fatalf("%q (%+v): lisp %q maps back to %q, %v", g, c, s, got, err)
			}
			if strings.ContainsAny(s, " ()[]{}\"';") {
				t.Fatalf("%q: lisp spelling %q is not a symbol", g, s)
			}
		}
	})
}
