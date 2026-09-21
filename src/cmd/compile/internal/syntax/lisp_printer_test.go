// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syntax

import (
	"bytes"
	"flag"
	"fmt"
	"internal/testenv"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var lispSrc = flag.String("lispsrc", "", "Go source file to print as go-lisp (TestLispPrintFile)")

// lispPrintGo parses Go source and prints it as go-lisp,
// keeping the //go: directives.
func lispPrintGo(filename string, src string) (string, error) {
	var dirs []lispDirective
	pragh := func(pos Pos, blank bool, text string, current Pragma) Pragma {
		if text != "" {
			dirs = append(dirs, lispDirective{pos, blank, text})
		}
		return current
	}
	f, err := Parse(NewFileBase(filename), strings.NewReader(src), nil, pragh, 0)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := lispPrint(&b, f, dirs); err != nil {
		return "", err
	}
	return b.String(), nil
}

// TestLispPrintFile prints the file named by -lispsrc as go-lisp.
func TestLispPrintFile(t *testing.T) {
	if *lispSrc == "" {
		t.Skip("no -lispsrc file")
	}
	src, err := os.ReadFile(*lispSrc)
	if err != nil {
		t.Fatal(err)
	}
	out, err := lispPrintGo(*lispSrc, string(src))
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString(out)
}

var lispUpdate = flag.Bool("lispupdate", false, "update go-lisp golden files")

// TestLispPrintGolden prints testdata/lisp/print.go and compares
// the result with testdata/lisp/print.golden.
func TestLispPrintGolden(t *testing.T) {
	const (
		input  = "testdata/lisp/print.go"
		golden = "testdata/lisp/print.golden"
	)
	src, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	got, err := lispPrintGo(input, string(src))
	if err != nil {
		t.Fatal(err)
	}
	if *lispUpdate {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("go-lisp output differs from %s (run with -lispupdate to update):\n%s", golden, lispDiff(string(want), got))
	}

	// The output must be valid go-lisp for the reader.
	if _, errs := lispReadString(got); len(errs) > 0 {
		t.Errorf("printed go-lisp does not read: %v", errs[0])
	}
}

// lispDiff returns the first differing line of want and got.
func lispDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return fmt.Sprintf("line %d:\nwant %q\ngot  %q", i+1, wl, gl)
		}
	}
	return ""
}

// TestLispPrintCorpus prints every Go file in $GOROOT/src and $GOROOT/test
// that parses, checking that printing succeeds, that the output reads
// without errors, and that it has one form per declaration (group).
func TestLispPrintCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping corpus test in short mode")
	}
	goroot := testenv.GOROOT(t)
	var files, printed int
	for _, dir := range []string{filepath.Join(goroot, "src"), filepath.Join(goroot, "test")} {
		filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			files++
			src, err := os.ReadFile(path)
			if err != nil {
				t.Error(err)
				return nil
			}
			var dirs []lispDirective
			pragh := func(pos Pos, blank bool, text string, current Pragma) Pragma {
				if text != "" {
					dirs = append(dirs, lispDirective{pos, blank, text})
				}
				return current
			}
			f, err := Parse(NewFileBase(path), bytes.NewReader(src), nil, pragh, 0)
			if err != nil {
				return nil // not valid Go syntax; nothing to print
			}
			var b strings.Builder
			if err := lispPrint(&b, f, dirs); err != nil {
				t.Errorf("%s: %v", path, err)
				return nil
			}
			printed++
			lf, errs := lispReadString(b.String())
			if len(errs) > 0 {
				t.Errorf("%s: printed go-lisp does not read: %v", path, errs[0])
				return nil
			}
			want := 1 // package clause
			for list := f.DeclList; len(list) > 0; {
				n := lispDeclRun(list)
				want++
				list = list[n:]
			}
			if len(lf.Forms) != want {
				t.Errorf("%s: printed %d top-level forms; want %d", path, len(lf.Forms), want)
			}
			return nil
		})
	}
	t.Logf("printed %d of %d files", printed, files)
}
