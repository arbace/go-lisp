// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modindex

import (
	"fmt"
	"internal/golisp"

	"cmd/go/internal/cfg"
	"cmd/go/internal/fsys"
	"cmd/go/internal/str"
)

// errLispFiles reports that a package directory has go-lisp (.lgo) files.
// The module index does not read them, so such packages are loaded with
// go/build instead, which does.
var errLispFiles = fmt.Errorf("%w: package has go-lisp files", ErrNotIndexed)

// hasLispFiles reports whether dir contains go-lisp source files.
// Directories in GOROOT are not checked: the standard library and the
// commands are written in Go, and they are loaded often.
func hasLispFiles(dir string) bool {
	if str.HasFilePathPrefix(dir, cfg.GOROOTsrc) {
		return false
	}
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && golisp.IsFile(e.Name()) {
			return true
		}
	}
	return false
}
