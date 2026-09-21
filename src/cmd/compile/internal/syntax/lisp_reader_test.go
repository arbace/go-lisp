// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syntax

import (
	"fmt"
	"strings"
	"testing"
)

// lispDump prints forms in a canonical, unambiguous way:
// symbols bare, keywords with ':', literals verbatim.
func lispDump(list []*lispForm) string {
	var b strings.Builder
	var dump func(x *lispForm)
	dumpList := func(open, close string, elems []*lispForm) {
		b.WriteString(open)
		for i, e := range elems {
			if i > 0 {
				b.WriteByte(' ')
			}
			dump(e)
		}
		b.WriteString(close)
	}
	dump = func(x *lispForm) {
		switch x.Kind {
		case lispList:
			dumpList("(", ")", x.Elems)
		case lispVector:
			dumpList("[", "]", x.Elems)
		case lispMap:
			dumpList("{", "}", x.Elems)
		case lispSet:
			dumpList("#{", "}", x.Elems)
		case lispTagged:
			b.WriteString("#" + x.Text + " ")
			dump(x.Elems[0])
		case lispKeyword:
			b.WriteString(":" + x.Text)
		default:
			b.WriteString(x.Text)
		}
	}
	for i, x := range list {
		if i > 0 {
			b.WriteByte(' ')
		}
		dump(x)
	}
	return b.String()
}

func lispReadString(src string) (*lispFile, []Error) {
	var errs []Error
	f, _ := lispRead(NewFileBase("x.lgo"), strings.NewReader(src), func(err error) {
		errs = append(errs, err.(Error))
	})
	return f, errs
}

func TestLispRead(t *testing.T) {
	for _, test := range []struct{ src, want string }{
		{"", ""},
		{"()", "()"},
		{"(a b c)", "(a b c)"},
		{"[a, b ,c]", "[a b c]"},
		{"(f ; comment\n x)", "(f x)"},
		{"(f\r\n\tx)", "(f x)"},
		{"{a 1} #{a b}", "{a 1} #{a b}"},

		// keywords
		{":index := :_ :slice-of :kv :w", ":index := :_ :slice-of :kv :w"},

		// symbols
		{"a.b.c x.-f -helper serve-HTTP new-reader", "a.b.c x.-f -helper serve-HTTP new-reader"},
		{"<-chan chan<- <- ... . _ ! != == <= >= << >> &^ &^= ~ | || & && % %=", "<-chan chan<- <- ... . _ ! != == <= >= << >> &^ &^= ~ | || & && % %="},
		{"+ - * / ++ -- += -= *= /= -x --x", "+ - * / ++ -- += -= *= /= -x --x"},
		{"größe 变量 x1 _x empty?", "größe 变量 x1 _x empty?"},

		// numbers (verbatim)
		{"0 42 0x2A 0X2a 0o52 0O52 0b101 052 1_000 0x_1F", "0 42 0x2A 0X2a 0o52 0O52 0b101 052 1_000 0x_1F"},
		{"1.5 .5 5. 1e3 1E-3 1e+3 0x1p-2 0X1P+2 0x1.8p1", "1.5 .5 5. 1e3 1E-3 1e+3 0x1p-2 0X1P+2 0x1.8p1"},
		{"3i 1.5i 0x1p-2i 1e3i 0i", "3i 1.5i 0x1p-2i 1e3i 0i"},

		// signed numbers read as unary operations (SPEC A4)
		{"-5 +1.5 -.5 -0x10 -1e-3", "(- 5) (+ 1.5) (- .5) (- 0x10) (- 1e-3)"},
		{"(-5) (- 5)", "((- 5)) (- 5)"},

		// strings, raw strings, runes (verbatim)
		{`"" "a b" "a\tb" "\xff\u00e9\U0001F600\101\a\v\\\""`, `"" "a b" "a\tb" "\xff\u00e9\U0001F600\101\a\v\\\""`},
		{"`raw\\d+` `multi\nline` ``", "`raw\\d+` `multi\nline` ``"},
		{`'a' '\n' '\'' '\x07' '\377' '\u00e9' '\U0001F600' 'é' '"'`, `'a' '\n' '\'' '\x07' '\377' '\u00e9' '\U0001F600' 'é' '"'`},
		{`"it's" "(" ";not a comment"`, `"it's" "(" ";not a comment"`},

		// discard and tags
		{"(a #_ b c)", "(a c)"},
		{"#_ #_ a b c", "c"},
		{"#_(a b) c", "c"},
		{"#go/imag 3 #go/bytes [\"a\" 255]", "#go/imag 3 #go/bytes [\"a\" 255]"},
		{"#t #_ x y", "#t y"},

		// adjacent collections need no space
		{"(a)(b)[c]", "(a) (b) [c]"},
		{"(f (g))[x]", "(f (g)) [x]"},

		// byte order mark at file start is ignored
		{"\uFEFF(a)", "(a)"},
	} {
		f, errs := lispReadString(test.src)
		if len(errs) > 0 {
			t.Errorf("%q: unexpected error: %v", test.src, errs[0])
			continue
		}
		if got := lispDump(f.Forms); got != test.want {
			t.Errorf("%q:\ngot  %s\nwant %s", test.src, got, test.want)
		}
	}
}

func TestLispLitKind(t *testing.T) {
	for _, test := range []struct {
		src  string
		kind LitKind
	}{
		{"42", IntLit}, {"0x2A", IntLit}, {"1_000", IntLit},
		{"1.5", FloatLit}, {".5", FloatLit}, {"0x1p-2", FloatLit}, {"1e3", FloatLit},
		{"3i", ImagLit}, {"1.5i", ImagLit},
		{"'a'", RuneLit}, {`'\n'`, RuneLit},
		{`"a"`, StringLit}, {"`a`", StringLit},
	} {
		f, errs := lispReadString(test.src)
		if len(errs) > 0 || len(f.Forms) != 1 {
			t.Errorf("%s: unexpected result %v %v", test.src, f, errs)
			continue
		}
		if x := f.Forms[0]; x.Kind != lispLit || x.Lit != test.kind || x.Bad {
			t.Errorf("%s: got kind %v lit %v bad %v; want literal %v", test.src, x.Kind, x.Lit, x.Bad, test.kind)
		}
	}
}

func TestLispReadErrors(t *testing.T) {
	for _, test := range []struct {
		src, pos, msg string // pos is "line:col" of the first error
	}{
		{"(a b", "1:1", "list not terminated: missing )"},
		{"[a (b)", "1:1", "vector not terminated: missing ]"},
		{"(a]", "1:3", "unexpected ]"},
		{"a)", "1:2", "unexpected )"},
		{"{a}", "1:1", "map must contain an even number of forms"},
		{`"abc`, "1:1", "string not terminated"},
		{"\"abc\ndef\"", "1:1", "string not terminated"},
		{"`abc", "1:1", "string not terminated"},
		{"'a", "1:1", "rune literal not terminated"},
		{`x "a\qb"`, "1:6", "unknown escape"},
		{`'ab'`, "1:1", "more than one character in rune literal"},
		{`''`, "1:2", "empty rune literal or unescaped '"},
		{"0x", "1:3", "hexadecimal literal has no digits"},
		{"09", "1:2", "invalid digit '9' in octal literal"},
		{"1__0", "1:3", "'_' must separate successive digits"},
		{"1abc", "1:1", "invalid literal 1abc"},
		{"0x1e-3", "1:5", "unexpected '-' after literal"}, // hex: only p/P take a sign
		{"(f(g))", "1:3", "unexpected '(' after symbol"},  // Go-style call syntax
		{"fmt.Println(x)", "1:12", "unexpected '(' after symbol"},
		{"#", "1:1", "invalid dispatch character after '#'"},
		{"(#_)", "1:2", "missing form after #_"},
		{"#tag", "1:1", "missing form after #tag"},
		{": x", "1:1", "invalid keyword: missing name after ':'"},
		{`a"b"`, "1:2", `unexpected '"' after symbol`},
		{`"a"b`, "1:4", "unexpected 'b' after literal"},
		{"'a'b", "1:4", "unexpected 'b' after literal"},
		{"@x", "1:1", "invalid character U+0040 '@'"},
		{`\a`, "1:1", `invalid character U+005C '\'`},
		{"(a\x00)", "1:3", "invalid NUL character"},
		{"(a \xff)", "1:4", "invalid UTF-8 encoding"},
		{"(a \uFEFF)", "1:4", "invalid BOM in the middle of the file"},
		{"(a\n  (b\n", "2:3", "list not terminated: missing )"},
	} {
		_, errs := lispReadString(test.src)
		if len(errs) == 0 {
			t.Errorf("%q: expected error %q", test.src, test.msg)
			continue
		}
		err := errs[0]
		pos := fmt.Sprintf("%d:%d", err.Pos.Line(), err.Pos.Col())
		if pos != test.pos || !strings.Contains(err.Msg, test.msg) {
			t.Errorf("%q: got %s: %s; want %s: %s", test.src, pos, err.Msg, test.pos, test.msg)
		}
	}
}

func TestLispReadFirstErrorStops(t *testing.T) {
	// Without an error handler, reading stops at the first error.
	_, err := lispRead(NewFileBase("x.lgo"), strings.NewReader("(a))) \"x"), nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected )") {
		t.Errorf("got %v; want first error \"unexpected )\"", err)
	}
}

func TestLispReadPositions(t *testing.T) {
	src := "(f [x 1]\n   \"s\" :k)\n-5 #t y"
	f, errs := lispReadString(src)
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	at := func(pos Pos) string { return fmt.Sprintf("%d:%d", pos.Line(), pos.Col()) }
	list := f.Forms[0]
	for _, test := range []struct {
		got  Pos
		want string
	}{
		{list.Pos, "1:1"},
		{list.End, "2:10"},
		{list.Elems[0].Pos, "1:2"},          // f
		{list.Elems[1].Pos, "1:4"},          // [
		{list.Elems[1].End, "1:8"},          // ]
		{list.Elems[1].Elems[1].Pos, "1:7"}, // 1
		{list.Elems[2].Pos, "2:4"},          // "s"
		{list.Elems[3].Pos, "2:8"},          // :k
		{f.Forms[1].Pos, "3:1"},             // (- 5)
		{f.Forms[1].Elems[0].Pos, "3:1"},    // -
		{f.Forms[1].Elems[1].Pos, "3:2"},    // 5
		{f.Forms[2].Pos, "3:4"},             // #t
		{f.Forms[2].Elems[0].Pos, "3:7"},    // y
		{f.EOF, "3:8"},
	} {
		if got := at(test.got); got != test.want {
			t.Errorf("%s: got position %s; want %s", test.want, got, test.want)
		}
	}
}

func TestLispReadDirectives(t *testing.T) {
	src := ";go:build linux\n\n(package p)\n\n  ;go:noinline\n(f) ;go:nosplit\n;; go:not-a-directive\n; go:nor-this\n"
	f, errs := lispReadString(src)
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	want := []struct {
		pos   string
		blank bool
		text  string
	}{
		{"1:2", true, "go:build linux"},
		{"5:4", true, "go:noinline"},
		{"6:6", false, "go:nosplit"},
	}
	if len(f.Directives) != len(want) {
		t.Fatalf("got %d directives; want %d: %v", len(f.Directives), len(want), f.Directives)
	}
	for i, d := range f.Directives {
		pos := fmt.Sprintf("%d:%d", d.Pos.Line(), d.Pos.Col())
		if pos != want[i].pos || d.Blank != want[i].blank || d.Text != want[i].text {
			t.Errorf("directive %d: got %s %v %q; want %s %v %q", i, pos, d.Blank, d.Text, want[i].pos, want[i].blank, want[i].text)
		}
	}
}

func TestLispReadLineDirectives(t *testing.T) {
	src := "(a)\n;line foo.lgo:10\n(b)\n (c) ;line bar.lgo:20\n;line :30:5\n(d)"
	f, errs := lispReadString(src)
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	// Without a column, a line directive leaves columns unknown (0), as in Go.
	for i, want := range []string{"x.lgo:1:1", "foo.lgo:10:0", "foo.lgo:11:0", "foo.lgo:30:5"} {
		pos := f.Forms[i].Pos
		if got := fmt.Sprintf("%s:%d:%d", pos.RelFilename(), pos.RelLine(), pos.RelCol()); got != want {
			t.Errorf("form %d: got %s; want %s", i, got, want)
		}
	}

	_, errs = lispReadString(";line foo.lgo:0\n(a)")
	if len(errs) != 1 || !strings.Contains(errs[0].Msg, "invalid line number") {
		t.Errorf("got %v; want invalid line number error", errs)
	}
}

// FuzzLispRead checks that reading arbitrary input never panics
// and that reading valid input reproduces its canonical dump.
func FuzzLispRead(f *testing.F) {
	for _, seed := range []string{
		"(package main)\n(func main [] [] (fmt.println \"hi\" 'x' 0x1p-2 -5))",
		"[a, b] {a 1} #{a} #go/imag 3 #_ x :=",
		";go:build linux\n;line x.lgo:10:2\n(a `raw` \"\\xff\")",
		"(a", "\"x", "'", "#", "0x", "(a\x00)", "\xff",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		file, errs := lispReadString(src)
		if len(errs) > 0 {
			return
		}
		// A valid file's dump must read back to the same dump.
		dump := lispDump(file.Forms)
		file2, errs := lispReadString(dump)
		if len(errs) > 0 {
			t.Fatalf("dump of %q does not read: %v\ndump: %s", src, errs[0], dump)
		}
		if dump2 := lispDump(file2.Forms); dump2 != dump {
			t.Fatalf("dump of %q is not stable:\n%s\n%s", src, dump, dump2)
		}
	})
}
