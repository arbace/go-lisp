// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package imports

import (
	"internal/golisp"
	"io"
	"strconv"
)

// readImports is ReadImports (without syntax error reporting) for Go and
// go-lisp files. For go-lisp files, it returns the leading comments as a
// Go-style header, so that ShouldBuild sees ;go:build lines as //go:build
// lines, and appends the quoted import paths to list. Syntax errors are
// left to the compiler, as for Go files.
func readImports(name string, r io.Reader, list *[]string) ([]byte, error) {
	if !golisp.IsFile(name) {
		return ReadImports(r, false, list)
	}
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	header, _ := golisp.GoHeader(src)
	if h, _ := golisp.ReadHeader(src); h != nil {
		for _, imp := range h.Imports {
			*list = append(*list, strconv.Quote(imp.Path))
		}
	}
	return header, nil
}
