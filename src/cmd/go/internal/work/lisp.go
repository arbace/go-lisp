// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package work

import (
	"slices"
	"strings"

	"cmd/go/internal/load"
)

// hasLispFiles reports whether the package has go-lisp (.lgo) source
// files. Such packages are compiled, but not vetted: vet only reads Go.
func hasLispFiles(p *load.Package) bool {
	isLisp := func(name string) bool { return strings.HasSuffix(name, ".lgo") }
	return slices.ContainsFunc(p.GoFiles, isLisp) ||
		slices.ContainsFunc(p.TestGoFiles, isLisp) ||
		slices.ContainsFunc(p.XTestGoFiles, isLisp)
}
