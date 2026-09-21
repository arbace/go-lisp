// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package work

import (
	"path/filepath"
	"strings"

	"cmd/go/internal/base"
	"cmd/go/internal/cfg"
)

// lispCoverSource prepares a go-lisp (.lgo) file for coverage
// instrumentation, which reads Go: it has the compiler convert the file
// to Go with line directives that map back to the go-lisp source, and
// replaces *sourceFile with the name of the Go file (x.lgo.go in the
// object directory). Other files are left alone.
func lispCoverSource(b *Builder, a *Action, sourceFile *string) error {
	if !strings.HasSuffix(*sourceFile, ".lgo") {
		return nil
	}
	out := a.Objdir + filepath.Base(*sourceFile) + ".go"
	if err := b.Shell(a).run(a.Objdir, "", nil, cfg.BuildToolexec, base.Tool("compile"), "-lisp2go", out, *sourceFile); err != nil {
		return err
	}
	*sourceFile = out
	return nil
}
