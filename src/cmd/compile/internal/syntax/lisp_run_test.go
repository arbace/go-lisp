// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syntax

import (
	"bytes"
	"context"
	"flag"
	"internal/testenv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var lispRun = flag.Bool("lisprun", false, "run TestLispRunCorpus (slow: compiles and runs $GOROOT/test programs)")

// TestLispRunCorpus converts the single-file "// run" programs in
// $GOROOT/test to go-lisp, compiles and runs both versions with the
// current toolchain, and checks that they behave identically (SPEC §10.5).
func TestLispRunCorpus(t *testing.T) {
	if !*lispRun {
		t.Skip("use -lisprun to run")
	}
	testenv.MustHaveGoBuild(t)
	goTool := testenv.GoToolPath(t)
	dir := t.TempDir()

	out, err := exec.Command(goTool, "list", "-export", "-f",
		"{{if .Export}}packagefile {{.ImportPath}}={{.Export}}{{end}}", "std").Output()
	if err != nil {
		t.Fatal(err)
	}
	importcfg := filepath.Join(dir, "importcfg")
	if err := os.WriteFile(importcfg, out, 0o644); err != nil {
		t.Fatal(err)
	}

	// build compiles and links a single main-package file.
	build := func(src, exe string) error {
		obj := exe + ".o"
		for _, args := range [][]string{
			{"tool", "compile", "-p", "main", "-importcfg", importcfg, "-o", obj, src},
			{"tool", "link", "-importcfg", importcfg, "-o", exe, obj},
		} {
			if out, err := exec.Command(goTool, args...).CombinedOutput(); err != nil {
				return &buildError{err, string(out)}
			}
		}
		return nil
	}
	run := func(exe string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, exe).CombinedOutput()
		s := string(out)
		if err != nil {
			s += "\n[" + err.Error() + "]"
		}
		// The binaries differ in name, which appears in some panics.
		return strings.ReplaceAll(s, exe, "EXE")
	}

	files, _ := filepath.Glob(filepath.Join(testenv.GOROOT(t), "test", "*.go"))
	more, _ := filepath.Glob(filepath.Join(testenv.GOROOT(t), "test", "*", "*.go"))
	files = append(files, more...)
	var ran, skipped, failed int
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		// only plain "// run" tests without build constraints or extra files
		if !bytes.HasPrefix(src, []byte("// run\n")) || bytes.Contains(src, []byte("//go:build")) {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(file), ".go")
		if lispLineDependent[base] {
			continue // checks its own source line numbers, which differ in go-lisp (SPEC F12)
		}
		if d := filepath.Base(filepath.Dir(file)); d != "test" {
			base = d + "_" + base
		}
		lisp, err := GoToLisp(file, bytes.NewReader(src))
		if err != nil {
			t.Errorf("%s: GoToLisp: %v", base, err)
			failed++
			continue
		}
		lgo := filepath.Join(dir, base+".lgo")
		if err := os.WriteFile(lgo, lisp, 0o644); err != nil {
			t.Fatal(err)
		}
		goExe, lispExe := filepath.Join(dir, base+"_go"), filepath.Join(dir, base+"_lisp")
		if err := build(file, goExe); err != nil {
			skipped++ // needs more than a plain compile; not a go-lisp issue
			continue
		}
		if err := build(lgo, lispExe); err != nil {
			t.Errorf("%s: go builds, go-lisp does not: %v", base, err)
			failed++
			continue
		}
		want, got := run(goExe), run(lispExe)
		if want != got && want == run(goExe) { // ignore nondeterministic programs
			t.Errorf("%s: output differs:\n--- go ---\n%s\n--- go-lisp ---\n%s", base, want, got)
			failed++
			continue
		}
		ran++
	}
	t.Logf("%d programs behave identically, %d failed, %d skipped (Go version does not build alone)", ran, failed, skipped)
}

// lispLineDependent are run tests that check the source line numbers
// of their own code (via runtime.Caller, panics, or PC tables). The
// go-lisp version has a different line layout, so they cannot agree.
var lispLineDependent = map[string]bool{
	"devirtualization_nil_panics": true, // panic line numbers
	"inline_literal":              true, // PC-to-line tables
	"bug347":                      true, // runtime.Caller, FuncForPC
	"bug348":                      true, // runtime.Caller, FuncForPC
	"issue14646":                  true, // runtime.Caller
	"issue18149":                  true, // //line, runtime.Caller
	"issue22083":                  true, // debug.Stack
	"issue22662":                  true, // //line, runtime.Caller
	"issue27201":                  true, // runtime.Stack
	"issue29504":                  true, // runtime.Callers
	"issue34123":                  true, // runtime.Callers
	"issue4562":                   true, // runtime.Caller
	"issue5856":                   true, // runtime.Caller
	"issue7690":                   true, // runtime.Stack
	"issue79762":                  true, // debug.Stack
}

type buildError struct {
	err error
	out string
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.out }
