// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syntax

import (
	"fmt"
	"strings"
	"testing"
)

// lispParseString parses go-lisp source, collecting errors.
func lispParseString(src string, mode Mode) (*File, []Error) {
	var errs []Error
	f, _ := ParseLisp(NewFileBase("x.lgo"), strings.NewReader(src), func(err error) {
		errs = append(errs, err.(Error))
	}, nil, mode)
	return f, errs
}

// lispGo parses go-lisp source and returns its declarations printed as
// Go on one line each, or the first error.
func lispGo(src string, mode Mode) string {
	f, errs := lispParseString(src, mode)
	if len(errs) > 0 {
		return "error: " + errs[0].Msg
	}
	var list []string
	for _, d := range f.DeclList {
		var b strings.Builder
		Fprint(&b, d, LineForm)
		list = append(list, b.String())
	}
	return strings.Join(list, "; ")
}

func TestLispParseExprs(t *testing.T) {
	for _, test := range []struct{ lisp, want string }{
		// names (D18)
		{"x", "X"},
		{"-x", "x"},
		{"len", "len"},
		{"read-all", "ReadAll"},
		{"fmt.println", "fmt.Println"},
		{"x.-f.g", "X.f.G"},
		{"map", "error: syntax error: keyword map cannot be used as a name"},
		{"empty?", "error: syntax error: invalid name empty?: Empty? is not a Go identifier"},

		// literals and operators
		{"-5", "-5"},
		{"(- 5)", "-5"},
		{"(+ a b c)", "A + B + C"},
		{"(< a b)", "A < B"},
		{"(< a b c)", "error: syntax error: comparison < takes exactly 2 operands"},
		{"(! a b)", "error: syntax error: operator ! is unary"},
		{"(<- ch)", "<-Ch"},
		{"(<- ch v)", "error: syntax error: send (<- ch v) is a statement, not an expression"},
		{"(* p)", "*P"},
		{"(& x)", "&X"},
		{"(^ x)", "^X"},
		{"(&^ a b)", "A &^ B"},
		{`"a\tb"`, `"a\tb"`},
		{"`raw`", "`raw`"},
		{"'x'", "'x'"},
		{"0x1p-2", "0x1p-2"},

		// func type vs func literal (A6)
		{"(func [] [])", "func()"},
		{"(func [] [] ())", "func() {}"},
		{"(func [-x int] [int] (return -x))", "func(x int) int { return x }"},
		{"(func [:_ int string] [error])", "func(int, string) error"},
		{"(func [int string error] [])", "func(int, string, error)"},
		{"(func [reader writer] [])", "func(Reader Writer)"}, // D11 pitfall: a named param
		{"(func [int error] [])", "error: syntax error: cannot declare predeclared name int without '-' (write -int to shadow it, or Int to export)"}, // F16 catches the pitfall for predeclared types
		{"(func [-a (... int)] [])", "func(a ...int)"},
		{"(func [-a (... int), -b int] [])", "error: syntax error: can only use ... with final parameter in list"},

		// index, slice, assert, selector (D10, D12, Q1, F2)
		{"(:index a i)", "A[I]"},
		{"(:index f int string)", "F[int, string]"},
		{"(:slice a :_ n)", "A[:N]"},
		{"(:slice a :_ :_)", "A[:]"},
		{"(:slice a 1 2 3)", "A[1:2:3]"},
		{"(:slice a 1 :_ 3)", "error: syntax error: middle index required in 3-index slice"},
		{"(:slice a _ n)", "A[_:N]"}, // _ is the blank identifier (Q1)
		{"(:assert x int)", "X.(int)"},
		{"(:assert x type)", "X.(type)"},
		{"(:sel (f) a.b)", "F().A.B"},
		{"(:sel (f) -a)", "F().a"},

		// composite literals (D5, D18)
		{"(:lit t (:kv :w 1) (:kv -h 2))", "T{ W: 1, h: 2, }"},
		{"(:lit (:slice-of point) (:lit :_ 1 2))", "[]Point{{1, 2}}"},
		{"(:lit (map string int) (:kv k 1))", "map[string]int{ K: 1, }"},
		{"(:lit :_ 1)", "{1}"},
		{"(:kv a b)", "error: syntax error: (:kv key value) is only allowed in (:lit ...)"},

		// types
		{"(:array-of ... int)", "[...]int"},
		{"(:array-of 4 byte)", "[4]byte"},
		{"(map string (:slice-of int))", "map[string][]int"},
		{"(<-chan int)", "<-chan int"},
		{"(chan<- (chan int))", "chan<- chan int"},
		{"(struct [a b int] [-c string `tag`] [io.reader] [(* -base) 42])",
			"struct{A, B int; c string `tag`; io.Reader; *base 42}"},
		{"(interface (Type [] [string]) (| (~ int) string) fmt.stringer)",
			"interface{Type() string; ~int | string; fmt.Stringer}"},
		{"(interface (func [:_ int string] [error]))", "interface{func(int, string) error}"},
		{"(interface (type [] []))", "error: syntax error: unexpected (type ...) in expression"},

		// calls
		{"(f)", "F()"},
		{"(f xs ...)", "F(Xs...)"},
		{"((* t) x)", "*T(X)"}, // correct tree; Go's printer omits the parentheses (F9)
		{"((:index f int) x)", "F[int](X)"},
		{"(if a b)", "error: syntax error: unexpected (if ...) in expression"},
		{"()", "error: syntax error: unexpected () in expression"},
		{"[a]", "error: syntax error: unexpected vector in expression"},
	} {
		got := lispGo("(package p)\n(import \"fmt\" \"io\")\n(var _ = "+test.lisp+")", 0)
		got = strings.TrimPrefix(got, "\"fmt\"; \"io\"; var _ = ")
		if got != test.want {
			t.Errorf("%s:\ngot  %s\nwant %s", test.lisp, got, test.want)
		}
	}
}

func TestLispParseStmts(t *testing.T) {
	for _, test := range []struct{ lisp, want string }{
		{"(= [a b] b a)", "A, B = B, A"},
		{"(:= [-v -ok] (:index m k))", "v, ok := M[K]"},
		{"(:= -len 3)", "len := 3"},
		{"(:= len 3)", "error: syntax error: cannot declare predeclared name len without '-' (write -len to shadow it, or Len to export)"},
		{"(:= (:index a 0) 1)", "A[0] := 1"}, // accepted like Go; types2 reports it
		{"(+= x 1) (++ x) (-- x) (&^= x m) (<<= x 1)", "X += 1; X++; X--; X &^= M; X <<= 1"},
		{"(<- ch v) (<- ch)", "Ch <- V; <-Ch"},
		{"(var -x int = 1) (const (a = iota) (b))", "var x int = 1; const ( A = iota; B )"},

		// if (D3)
		{"(if [(:= -x 1)] (> -x 0) (f) (else if (< -x 0) (g)) (else (h)))",
			"if x := 1; x > 0 { F() } else if x < 0 { G() } else { H() }"},
		{"(if c (else (if d)))", "if C {} else { if D {} }"},
		{"(if c (else (f)) (g))", "error: syntax error: else must be a trailing form of an if statement"},
		{"(if)", "error: syntax error: missing condition in if statement"},

		// for (D15, F25)
		{"(for [])", "for {}"},
		{"(for [c])", "for C {}"},
		{"(for [(:= -i 0) (< -i n) (++ -i)])", "for i := 0; i < N; i++ {}"},
		{"(for [(:= -i 0) :_ (++ -i)])", "for i := 0; ; i++ {}"},
		{"(for [k v (range m)])", "for K, V := range M {}"},
		{"(for [_ -v (range m)])", "for _, v := range M {}"},
		{"(for [(= [k v] (range m))])", "for K, V = range M {}"},
		{"(for [(range ch)])", "for range Ch {}"},
		{"(for [a b])", "error: syntax error: invalid for header (F25): want [], [cond], [init cond post], or a range clause"},
		{"(for [(:= -i 0) (< -i n) (:= -j 1)])", "error: syntax error: cannot declare in post statement of for loop"},

		// switch, type switch, select (D16)
		{"(switch [(:= -x (f))] -x (case [1 2] (a) (fallthrough)) (default (b)))",
			"switch x := F(); x { case 1, 2: A(); fallthrough; default: B() }"},
		{"(switch (case [(< x 0)] (neg)))", "switch { case X < 0: Neg() }"},
		{"(:type-switch [-v x] (case [int string] (use -v)) (case [nil]))",
			"switch v := X.(type) { case int, string: Use(v); case nil: }"},
		{"(:type-switch [(:= -y 1)] [x] (default))", "switch y := 1; X.(type) { default: }"},
		{"(select (case (<- out v)) (case (:= [-x -ok] (<- in)) (got -x -ok)) (default))",
			"select { case Out <- V: case x, ok := <-In: Got(x, ok); default: }"},

		// labels, branches, go/defer, blocks
		{"(:label -outer (for [] (continue -outer)))", "outer: for { continue outer }"},
		{"(:label -end ())", "end:"},
		{"(goto -end) (:label -end ())", "goto end; end:"},
		{"(go (f)) (defer (g))", "go F(); defer G()"},
		{"(:block (f))", "{ F() }"},
		{"(return a b)", "return A, B"},
		{"(else (f))", "error: syntax error: else must be a trailing form of an if statement"},
	} {
		got := lispGo("(package p)\n(func -f [] []\n"+test.lisp+")", 0)
		if !strings.HasPrefix(got, "error: ") {
			got = strings.TrimSuffix(strings.TrimPrefix(got, "func f() { "), " }")
		}
		if got != test.want {
			t.Errorf("%s:\ngot  %s\nwant %s", test.lisp, got, test.want)
		}
	}
}

func TestLispParseDecls(t *testing.T) {
	for _, test := range []struct{ lisp, want string }{
		{`(import "fmt")`, `import "fmt"`},
		{`(import "fmt" [str "strings"] [_ "embed"] [. "math"])`, `"fmt"; str "strings"; _ "embed"; . "math"`},
		{`(import [my-str "strings"])`, "error: syntax error: invalid import name symbol my-str"},
		{`(func f [] []) (import "fmt")`, "error: syntax error: imports must appear before other declarations"},
		{"(func f [] [])", "func F()"},
		{"(func f [] [] ())", "func F() {}"},
		{"(func main [] [] ())", "func main() {}"},
		{"(func [-r (* rect)] area [] [float64] ())", "func (r *Rect) Area() float64 {}"},
		{"(func [rect] -area [] [] ())", "func (Rect) area() {}"},
		{"(func Map [t any, u any] [-xs (:slice-of t)] [] ())", "func Map[T any, U any](xs []T) {}"},
		{"(func f [] [] [] ())", "error: syntax error: empty type parameter list"},
		{"(func [a b] m [] [] ())", "func (A B) M() {}"}, // [a b] is one named receiver (D11)
		{"(func [a b c] m [] [] ())", "error: syntax error: method has 3 receivers; want exactly one"},
		{"(type t int) (type a = b)", "type T int; type A = B"},
		{"(type list [t any] (struct [-head (* (:index -node t))]))", "type List[T any] struct{head *node[T]}"},
		{"(type t [] int)", "error: syntax error: empty type parameter list"},
		{"(var (x int) (y = 1))", "X int; Y = 1"}, // specs of one group, printed one by one
		{"(var)", ""},
		{"(var x)", "error: syntax error: missing type or = values in var declaration"},
		{"(var x int string)", "error: syntax error: unexpected symbol string in var declaration (missing =?)"},
		{"(type error (struct))", "error: syntax error: cannot declare predeclared name error without '-' (write -error to shadow it, or Error to export)"},
		{"(if c)", "error: syntax error: non-declaration statement outside function body: (if ...)"},
	} {
		got := lispGo("(package p)\n"+test.lisp, 0)
		if got != test.want {
			t.Errorf("%s:\ngot  %s\nwant %s", test.lisp, got, test.want)
		}
	}
}

func TestLispParseFile(t *testing.T) {
	for _, test := range []struct{ src, want string }{
		{"", "error: syntax error: package statement must be first"},
		{"(func f [] [])", "error: syntax error: package statement must be first"},
		{"(package my-pkg)", "error: syntax error: invalid package name my-pkg"},
		{"(package p q)", "error: syntax error: package clause must be (package name)"},
	} {
		if got := lispGo(test.src, 0); got != test.want {
			t.Errorf("%q:\ngot  %s\nwant %s", test.src, got, test.want)
		}
	}

	f, errs := lispParseString(";go:build linux && go1.21\n\n(package p)", 0)
	if len(errs) > 0 || f.GoVersion != "go1.21" || f.PkgName.Value != "p" {
		t.Errorf("GoVersion: got %v %q", errs, f.GoVersion)
	}
}

// TestLispCheckBranches checks that the parser runs Go's branch checks (F5).
func TestLispCheckBranches(t *testing.T) {
	for _, test := range []struct{ body, want string }{
		{"(for [] (break))", ""},
		{"(break)", "break is not in a loop, switch, or select"},
		{"(continue)", "continue is not in a loop"},
		{"(switch x (case [1] (fallthrough)))", "cannot fallthrough final case in switch"},
		{"(goto -l)", "label l not defined"},
		{"(:label -l (f))", "label l defined and not used"},
		{"(for [] ((func [] [] (break))))", "break is not in a loop, switch, or select"},
	} {
		got := lispGo("(package p)\n(func -f [] []\n"+test.body+")", CheckBranches)
		if test.want == "" {
			if strings.HasPrefix(got, "error: ") {
				t.Errorf("%s: unexpected %s", test.body, got)
			}
		} else if !strings.Contains(got, test.want) {
			t.Errorf("%s:\ngot  %s\nwant error %q", test.body, got, test.want)
		}
	}
}

// TestLispPragmas checks that directives reach the pragma handler
// and attach like //go: directives do (SPEC §7, F11).
func TestLispPragmas(t *testing.T) {
	src := `;go:build linux
(package p)

;go:noinline
(func f [] [] ())

;go:embed misplaced
(var
  ;go:embed x.txt
  (x string))

(func g [] []
  ;go:nosplit
  (h))
`
	type call struct {
		pos  string
		text string // "" reports unused pragmas
	}
	var calls []call
	type pragma struct{ texts []string }
	pragh := func(pos Pos, blank bool, text string, current Pragma) Pragma {
		calls = append(calls, call{fmt.Sprintf("%d:%d", pos.Line(), pos.Col()), text})
		if text == "" {
			return nil
		}
		p, _ := current.(*pragma)
		if p == nil {
			p = new(pragma)
		}
		p.texts = append(p.texts, text)
		return p
	}
	f, err := ParseLisp(NewFileBase("x.lgo"), strings.NewReader(src), nil, pragh, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []call{
		{"1:2", "go:build linux"},
		{"4:2", "go:noinline"},
		{"7:2", "go:embed misplaced"},
		{"8:1", ""}, // directive before a group is misplaced (F11)
		{"9:4", "go:embed x.txt"},
		{"13:4", "go:nosplit"},
		{"14:5", ""}, // directives before statements are unused
	}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Errorf("pragma handler calls:\ngot  %v\nwant %v", calls, want)
	}
	texts := func(p Pragma) string {
		if p == nil {
			return ""
		}
		return strings.Join(p.(*pragma).texts, ",")
	}
	if got := texts(f.Pragma); got != "go:build linux" {
		t.Errorf("file pragma: got %q", got)
	}
	if got := texts(f.DeclList[0].(*FuncDecl).Pragma); got != "go:noinline" {
		t.Errorf("func pragma: got %q", got)
	}
	if got := texts(f.DeclList[1].(*VarDecl).Pragma); got != "go:embed x.txt" {
		t.Errorf("grouped var pragma: got %q", got)
	}
}

// FuzzLispParse checks that ParseLisp never panics, and that any
// file it accepts prints back as go-lisp that parses to the same tree.
func FuzzLispParse(f *testing.F) {
	for _, seed := range []string{
		"(package p)\n(func main [] [] (fmt.println \"hi\"))",
		"(package p)\n(type t (struct [a int] [(* b) `x`]))\n(var (x = 1) (y int))",
		"(package p)\n(func f [t any] [-x t] [:_ int error] (if [(:= -v 1)] -v (return) (else if c) (else)) (for [k v (range m)] (switch (case [1]))))",
		"(package p)\n(var _ = (:lit (map string int) (:kv \"a\" (- 1))))",
		"(package p)\n(func f [] [] (:type-switch [x] (case [int])) (select (default)) (:label -l ()) (goto -l))",
		"(package", "(package p) (func)", "(package p) (if)", "(package p) (var [])", "(package p) (type)",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		f1, errs := lispParseString(src, 0)
		if len(errs) > 0 || f1 == nil {
			return
		}
		var b strings.Builder
		if err := lispPrint(&b, f1, nil); err != nil {
			return // e.g. BadExpr-free trees Go itself cannot express
		}
		f2, errs := lispParseString(b.String(), 0)
		if len(errs) > 0 {
			t.Fatalf("reprint of %q does not parse: %v\n%s", src, errs[0], b.String())
		}
		if d := lispCompare(f1, f2); d != "" {
			t.Fatalf("reprint of %q differs: %s\n%s", src, d, b.String())
		}
	})
}
