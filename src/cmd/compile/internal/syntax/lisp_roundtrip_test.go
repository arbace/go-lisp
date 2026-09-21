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
// statements and empty declaration groups (var ()) are dropped
// from statement lists (SPEC §8.2).
func lispSliceElems(v reflect.Value) []reflect.Value {
	var list []reflect.Value
	for i := range v.Len() {
		e := v.Index(i)
		if v.Type() == lispStmtSliceType {
			switch s := e.Interface().(type) {
			case *EmptyStmt:
				continue
			case *DeclStmt:
				if len(s.DeclList) == 0 {
					continue
				}
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
		if out, d := lispRoundTripGo("fuzz.go", []byte(src)); d != "" && d != lispNotPrintable {
			t.Fatalf("Go -> go-lisp -> Go of %q: %s\n%s", src, d, out)
		}
	})
}

// lispRoundTripGo converts Go source to go-lisp and back to Go
// (Go -> go-lisp -> AST -> parenthesize -> Go -> AST) and compares the
// result with the original tree and directives (SPEC F9).
func lispRoundTripGo(filename string, src []byte) (goSrc, diff string) {
	goSrc, diff = lispRoundTripGo1(filename, src)
	if diff != "" {
		// Excuse failures only where Go's own printer fails as well.
		if f, err := Parse(NewFileBase(filename), bytes.NewReader(src), nil, nil, 0); err == nil && !lispGoPrintable(f) {
			return goSrc, lispNotPrintable
		}
	}
	return goSrc, diff
}

func lispRoundTripGo1(filename string, src []byte) (goSrc, diff string) {
	collect := func(dirs *[]string) PragmaHandler {
		return func(pos Pos, blank bool, text string, current Pragma) Pragma {
			if text != "" {
				// A trailing CR (from a CR CR LF line end) is not preserved.
				*dirs = append(*dirs, strings.TrimRight(text, "\r"))
			}
			return current
		}
	}
	var dirs1, dirs2 []string
	f1, err := Parse(NewFileBase(filename), bytes.NewReader(src), nil, collect(&dirs1), 0)
	if err != nil {
		return "", ""
	}
	lisp, err := GoToLisp(filename, bytes.NewReader(src))
	if err != nil {
		return "", fmt.Sprintf("GoToLisp: %v", err)
	}
	out, err := LispToGo(filename, bytes.NewReader(lisp))
	if err != nil {
		return "", fmt.Sprintf("LispToGo: %v", err)
	}
	f2, err := Parse(NewFileBase(filename), bytes.NewReader(out), nil, collect(&dirs2), 0)
	if err != nil {
		return string(out), fmt.Sprintf("generated Go does not parse: %v", err)
	}
	if d := lispCompare(f1, f2); d != "" {
		return string(out), d
	}
	if fmt.Sprint(dirs1) != fmt.Sprint(dirs2) {
		return string(out), fmt.Sprintf("directives %q vs %q", dirs1, dirs2)
	}
	return string(out), ""
}

// lispNotPrintable is the diff reported for trees that Go's own printer
// cannot round-trip; lisp2go is only expected to be as good as it.
const lispNotPrintable = "(not printable as Go by the syntax printer)"

// lispGoPrintable reports whether Go's syntax printer can print f so
// that it parses back to the same tree. The Go parser accepts a few
// invalid programs whose trees it cannot print, such as a type
// parameter whose constraint came from a call: type A[A(~0)] int.
// If the printer crashes (it cannot print var () statements), the tree
// counts as printable, so that lisp2go, which handles that, is tested.
func lispGoPrintable(f *File) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = true
		}
	}()
	var b bytes.Buffer
	if _, err := Fprint(&b, f, 0); err != nil {
		return false
	}
	f2, err := Parse(NewFileBase("x.go"), &b, nil, nil, 0)
	if err != nil {
		return false
	}
	f2.GoVersion = f.GoVersion // the printer does not print //go:build lines
	return lispCompare(f, f2) == ""
}

func TestLispToGoGolden(t *testing.T) {
	src, err := os.ReadFile("testdata/lisp/print.go")
	if err != nil {
		t.Fatal(err)
	}
	if out, d := lispRoundTripGo("print.go", src); d != "" {
		t.Errorf("Go -> go-lisp -> Go: %s\n%s", d, out)
	}
}

func TestLispToGoParens(t *testing.T) {
	for _, test := range []struct{ lisp, want string }{
		{"(* (+ a b) c)", "(A + B) * C"},
		{"(- a (- b c))", "A - (B - C)"},
		{"(- (- a b) c)", "A - B - C"},
		{"(|| a (&& b c))", "A || B && C"},
		{"(&& (|| a b) c)", "(A || B) && C"},
		{"(- (- x))", "-(-X)"},
		{"(& (& x))", "&(&X)"},
		{"(& (^ x))", "&(^X)"},
		{"(* (* p))", "**P"},
		{"(- (+ a b))", "-(A + B)"},
		{"(:sel (* p) x)", "(*P).X"},
		{"((* t) x)", "(*T)(X)"},
		{"((func [] []) f)", "(func())(F)"},
		{"((<-chan int) c)", "(<-chan int)(C)"},
		{"(:index (+ a b) 0)", "(A + B)[0]"},
		{"(chan (<-chan int))", "chan (<-chan int)"},
		{"(chan<- (chan int))", "chan<- chan int"},
		{"(<- (<- c))", "<-<-C"},
	} {
		out, err := LispToGo("x.lgo", strings.NewReader("(package p)\n(var _ = "+test.lisp+")"))
		if err != nil {
			t.Errorf("%s: %v", test.lisp, err)
			continue
		}
		got := strings.TrimSpace(strings.TrimPrefix(string(out), "package p\n\nvar _ = "))
		if got != test.want {
			t.Errorf("%s:\ngot  %s\nwant %s", test.lisp, got, test.want)
		}
	}

	// composite literals in statement headers
	out, err := LispToGo("x.lgo", strings.NewReader(`(package p)
(func -f [] []
  (if (== x (:lit t)) ())
  (for [(:= -i (:lit t)) (< -i.n 3) :_] ())
  (switch (:lit t) (default))
  (for [_ -v (range (:lit t (:kv :a 1)))] ()))`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"if X == (T{})", "i := (T{}); ", "switch (T{})", "range (T{"} {
		if !strings.Contains(strings.ReplaceAll(string(out), "{ ", "{"), want) && !strings.Contains(string(out), want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
	if _, err := Parse(NewFileBase("x.go"), bytes.NewReader(out), nil, nil, 0); err != nil {
		t.Errorf("generated Go does not parse: %v\n%s", err, out)
	}
}

// TestLispToGoCorpus checks Go -> go-lisp -> Go for every valid Go file
// in $GOROOT/src and $GOROOT/test.
func TestLispToGoCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping corpus test in short mode")
	}
	goroot := testenv.GOROOT(t)
	var files, failed, skipped int
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
			_, diff := lispRoundTripGo(path, src)
			if diff == lispNotPrintable {
				skipped++
				return nil
			}
			if diff != "" {
				failed++
				if failed <= 25 {
					t.Errorf("%s: %s", path, diff)
				}
			}
			return nil
		})
	}
	t.Logf("Go -> go-lisp -> Go for %d files, %d failed, %d skipped (not printable by Go's own printer)", files, failed, skipped)
}
