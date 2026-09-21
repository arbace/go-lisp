// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// isLispGo reports whether the input file is Go source generated from a
// go-lisp file for coverage (x.lgo.go, see cmd/go). Its /*line*/
// directives map positions back to the go-lisp source, so they are
// honored for such files, unlike for other files.
func isLispGo(name string) bool {
	return strings.HasSuffix(name, ".lgo.go")
}

// lispSourceName returns the file name to record for a function at
// fnpos of the input file name: the go-lisp source file for go-lisp
// files, the name itself otherwise.
func lispSourceName(name string, fnpos token.Position) string {
	if isLispGo(name) {
		return fnpos.Filename
	}
	return name
}

// lispSource returns the source to parse for the file name: nil (read
// the file) for Go files, and for go-lisp (.lgo) files the Go source the
// compiler generates from them, whose /*line*/ directives map positions
// back to the go-lisp file. On failure, it returns a reader that reports
// the error.
func lispSource(name string) any {
	if !strings.HasSuffix(name, ".lgo") {
		return nil
	}
	tmp, err := os.MkdirTemp("", "cover-golisp")
	if err != nil {
		return errReader{err}
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, filepath.Base(name)+".go")
	compile := "compile"
	if exe, err := os.Executable(); err == nil {
		compile = filepath.Join(filepath.Dir(exe), "compile")
		if runtime.GOOS == "windows" {
			compile += ".exe"
		}
	}
	if msg, err := exec.Command(compile, "-lisp2go", out, name).CombinedOutput(); err != nil {
		return errReader{errors.New(strings.TrimSpace(string(msg)))}
	}
	src, err := os.ReadFile(out)
	if err != nil {
		return errReader{err}
	}
	return src
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }
