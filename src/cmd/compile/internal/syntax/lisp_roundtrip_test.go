// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syntax

import (
	"bytes"
	"fmt"
	"internal/testenv"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// lispRoundTrip parses Go source, prints it as go-lisp, parses that
// with ParseLisp, and compares the two syntax trees (SPEC §8).
// It returns the go-lisp text and a description of the first
// difference or error ("" if the trees are equivalent).
func lispRoundTrip(filename string, src []byte) (lisp, diff string) {
	var dirs []lispDirective
	pragh := func(pos Pos, blank bool, text string, current Pragma) Pragma {
		if text != "" {
			dirs = append(dirs, lispDirective{pos, blank, text})
		}
		return current // nil: the trees carry no pragmas
	}
	f1, err := Parse(NewFileBase(filename), bytes.NewReader(src), nil, pragh, 0)
	if err != nil {
		return "", "" // not valid Go syntax; nothing to compare
	}
	var b strings.Builder
	if err := lispPrint(&b, f1, dirs); err != nil {
		return "", fmt.Sprintf("print: %v", err)
	}
	lisp = b.String()
	var errs []string
	f2, _ := ParseLisp(NewFileBase(filename), strings.NewReader(lisp), func(err error) {
		errs = append(errs, err.Error())
	}, nil, 0)
	if len(errs) > 0 {
		return lisp, "ParseLisp: " + strings.Join(errs[:min(len(errs), 3)], "\n\t")
	}
	if d := lispCompare(f1, f2); d != "" {
		return lisp, d
	}
	if d := lispCheckPositions(f2); d != "" {
		return lisp, d
	}
	return lisp, ""
}

// lispSkipFields are node fields that the round trip does not compare:
// positions, pragmas (the handler is nil), and fields set by later passes.
var lispSkipFields = map[string]bool{
	"Group": true, "Pragma": true, "Rbrace": true, "Colon": true, "EOF": true,
	"Target": true, "DeferAt": true,
}

var (
	lispStmtSliceType = reflect.TypeFor[[]Stmt]()
	lispDeclSliceType = reflect.TypeFor[[]Decl]()
)

// lispCompare compares two syntax trees modulo the normalizations
// of SPEC §8 and returns the first difference, or "".
func lispCompare(a, b Node) string {
	return lispCmp("File", reflect.ValueOf(a), reflect.ValueOf(b))
}

func lispUnparenValue(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Interface && !v.IsNil() {
		p, ok := v.Interface().(*ParenExpr)
		if !ok {
			break
		}
		v = reflect.ValueOf(&p.X).Elem()
	}
	return v
}

func lispCmp(path string, x, y reflect.Value) string {
	x, y = lispUnparenValue(x), lispUnparenValue(y)
	if x.Kind() != y.Kind() {
		return fmt.Sprintf("%s: kind %s vs %s", path, x.Kind(), y.Kind())
	}
	switch x.Kind() {
	case reflect.Interface, reflect.Pointer:
		if x.IsNil() || y.IsNil() {
			if x.IsNil() != y.IsNil() {
				return fmt.Sprintf("%s: %s vs %s", path, lispShow(x), lispShow(y))
			}
			return ""
		}
		if x.Kind() == reflect.Interface {
			if x.Elem().Type() != y.Elem().Type() {
				return fmt.Sprintf("%s: %s vs %s", path, x.Elem().Type(), y.Elem().Type())
			}
		}
		return lispCmp(path, x.Elem(), y.Elem())

	case reflect.Struct:
		t := x.Type()
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() || lispSkipFields[f.Name] {
				continue
			}
			if d := lispCmp(path+"."+f.Name, x.Field(i), y.Field(i)); d != "" {
				return d
			}
		}
		return ""

	case reflect.Slice:
		xs, ys := lispSliceElems(x), lispSliceElems(y)
		if x.Type() == lispDeclSliceType {
			if gx, gy := lispGroups(xs), lispGroups(ys); gx != gy {
				return fmt.Sprintf("%s: groups %s vs %s", path, gx, gy)
			}
		}
		if len(xs) != len(ys) {
			return fmt.Sprintf("%s: length %d vs %d", path, len(xs), len(ys))
		}
		for i := range xs {
			if d := lispCmp(fmt.Sprintf("%s[%d]", path, i), xs[i], ys[i]); d != "" {
				return d
			}
		}
		return ""

	case reflect.Array:
		for i := range x.Len() {
			if d := lispCmp(fmt.Sprintf("%s[%d]", path, i), x.Index(i), y.Index(i)); d != "" {
				return d
			}
		}
		return ""
	}

	if !x.Equal(y) {
		return fmt.Sprintf("%s: %v vs %v", path, x, y)
	}
	return ""
}

// lispSliceElems returns the elements of a slice value; empty
// statements are dropped from statement lists (SPEC §8.2).
func lispSliceElems(v reflect.Value) []reflect.Value {
	var list []reflect.Value
	for i := range v.Len() {
		e := v.Index(i)
		if v.Type() == lispStmtSliceType {
			if _, ok := e.Interface().(*EmptyStmt); ok {
				continue
			}
		}
		list = append(list, e)
	}
	return list
}

// lispGroups describes the grouping of a declaration list; a group
// holding a single import counts as ungrouped (SPEC §8.4).
func lispGroups(list []reflect.Value) string {
	var b strings.Builder
	for i := 0; i < len(list); {
		d := list[i].Interface().(Decl)
		n := 1
		if g := lispDeclGroup(d); g != nil {
			for i+n < len(list) && lispDeclGroup(list[i+n].Interface().(Decl)) == g {
				n++
			}
			_, imp := d.(*ImportDecl)
			if n > 1 || !imp {
				fmt.Fprintf(&b, "(%d)", n)
				i += n
				continue
			}
		}
		b.WriteString(".")
		i += n
	}
	return b.String()
}

func lispShow(v reflect.Value) string {
	if v.IsNil() {
		return "nil"
	}
	return v.Elem().Type().String()
}

// lispCheckPositions reports the first node without a known position.
func lispCheckPositions(n Node) string {
	var bad string
	Inspect(n, func(n Node) bool {
		if bad != "" || n == nil {
			return false
		}
		if !n.Pos().IsKnown() {
			bad = fmt.Sprintf("%T has no position", n)
		}
		return true
	})
	return bad
}

func TestLispRoundTripGolden(t *testing.T) {
	src, err := os.ReadFile("testdata/lisp/print.go")
	if err != nil {
		t.Fatal(err)
	}
	if lisp, d := lispRoundTrip("print.go", src); d != "" {
		t.Errorf("round trip: %s\n%s", d, lisp)
	}
}

// TestLispRoundTripCorpus round-trips every Go file in $GOROOT/src
// and $GOROOT/test that parses (SPEC §10.2).
func TestLispRoundTripCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping corpus test in short mode")
	}
	goroot := testenv.GOROOT(t)
	var files, failed int
	for _, dir := range []string{filepath.Join(goroot, "src"), filepath.Join(goroot, "test")} {
		filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				t.Error(err)
				return nil
			}
			files++
			if _, d := lispRoundTrip(path, src); d != "" {
				failed++
				if failed <= 25 {
					t.Errorf("%s: %s", path, d)
				}
			}
			return nil
		})
	}
	t.Logf("round-tripped %d files, %d failed", files, failed)
}

// FuzzLispRoundTrip checks that any Go source the Go parser accepts
// round-trips through go-lisp (SPEC §10.3).
func FuzzLispRoundTrip(f *testing.F) {
	if src, err := os.ReadFile("testdata/lisp/print.go"); err == nil {
		f.Add(string(src))
	}
	for _, seed := range []string{
		"package p; func f() { x := (a); _ = x.(type); for ;; {}; L: }",
		"package p; type T[P any] struct{ a, b P `x`; *U 1 }; var _ = T[int]{a: 1}",
		"package p; import (\"a\"; b \"c\"); const (X = iota; Y); var ()",
		"package p; func (T) m[P any]() {}; func g(...int)",
		"package p; func f() { select { case x := <-c: case c <- 1: default: }; switch x := y.(type) {} }",
		"package p; var _ = func() {}; var _ = [...]int{1: 2}; var _ = map[[2]int]int{{1, 2}: 3}",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if lisp, d := lispRoundTrip("fuzz.go", []byte(src)); d != "" {
			t.Fatalf("round trip of %q: %s\n%s", src, d, lisp)
		}
	})
}
