// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package noder

import (
	"flag"
	"os"

	"cmd/compile/internal/base"
	"cmd/compile/internal/syntax"
)

// Lisp2Go implements the -lisp2go flag: it writes the go-lisp file named
// by the (single) file argument as Go source with line directives that
// map back to the go-lisp source, and exits. The go command uses it to
// instrument go-lisp files for coverage.
func Lisp2Go() {
	out := base.Flag.Lisp2Go
	if out == "" {
		return
	}
	if flag.NArg() != 1 || !syntax.IsLispFile(flag.Arg(0)) {
		base.Fatalf("-lisp2go needs exactly one go-lisp (.lgo) file argument")
	}
	in := flag.Arg(0)
	f, err := os.Open(in)
	if err != nil {
		base.Fatalf("%v", err)
	}
	defer f.Close()
	src, err := syntax.LispToGoLines(in, f)
	if err != nil {
		base.Fatalf("%v", err)
	}
	if err := os.WriteFile(out, src, 0o666); err != nil {
		base.Fatalf("%v", err)
	}
	base.Exit(0)
}
