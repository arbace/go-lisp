// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file reads the header of go-lisp (.lgo) source files, the
// s-expression syntax of Go described in golisp/SPEC.md: the
// counterpart of readGoInfo.

package build

import (
	"errors"
	"fmt"
	"go/ast"
	"internal/golisp"
	"io"
	"strings"
)

// isGoOrLispFile reports whether name is a Go or go-lisp source file.
func isGoOrLispFile(name string) bool {
	return strings.HasSuffix(name, ".go") || golisp.IsFile(name)
}

// isLispFile reports whether name is a go-lisp source file.
func isLispFile(name string) bool {
	return golisp.IsFile(name)
}

// goNameExt is nameExt, except that go-lisp files count as Go files.
func goNameExt(name string) string {
	if golisp.IsFile(name) {
		return ".go"
	}
	return nameExt(name)
}

// readSourceInfo reads the header of a Go or go-lisp file.
func readSourceInfo(name string, f io.Reader, info *fileInfo) error {
	if golisp.IsFile(name) {
		return readLispInfo(f, info)
	}
	return readGoInfo(f, info)
}

// readLispInfo is readGoInfo for go-lisp files. It records the leading
// comments as a Go-style header (;go:build becomes //go:build, so that
// build constraints work as for Go files), the //go: directives before
// the package clause, the package clause itself (as info.parsed), the
// imports, and, if the file imports "embed", the ;go:embed patterns.
// Syntax errors are recorded in info.parseErr, as by readGoInfo.
func readLispInfo(f io.Reader, info *fileInfo) error {
	src, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	tf := info.fset.AddFile(info.name, -1, len(src))
	tf.SetLinesForContent(src)
	errorf := func(off int, format string, args ...any) error {
		return fmt.Errorf("%s: %s", tf.Position(tf.Pos(off)), fmt.Sprintf(format, args...))
	}

	var n int
	info.header, n = golisp.GoHeader(src)
	for _, c := range golisp.Comments(src, n) {
		if strings.HasPrefix(c.Text, "//go:") {
			info.directives = append(info.directives, Directive{c.Text, tf.Position(tf.Pos(c.Off))})
		}
	}

	h, err := golisp.ReadHeader(src)
	if h == nil {
		var e *golisp.Error
		if errors.As(err, &e) {
			err = errorf(e.Off, "%s", e.Msg)
		}
		info.parseErr = err
		return nil
	}
	pos := tf.Pos(h.PackageOff)
	info.parsed = &ast.File{Package: pos, Name: &ast.Ident{NamePos: pos, Name: h.Package}}

	hasEmbed := false
	for _, imp := range h.Imports {
		switch {
		case !isValidImport(imp.Path):
			info.parseErr = errorf(imp.Off, "invalid import path: %q", imp.Path)
			return nil
		case imp.Path == "C":
			info.parseErr = errorf(imp.Off, "go-lisp files cannot use cgo")
			return nil
		case imp.Path == "embed":
			hasEmbed = true
		}
		info.imports = append(info.imports, fileImport{imp.Path, tf.Pos(imp.Off), nil})
	}
	if err != nil {
		var e *golisp.Error
		if errors.As(err, &e) {
			err = errorf(e.Off, "%s", e.Msg)
		}
		info.parseErr = err
		return nil
	}

	// The compiler checks where ;go:embed directives are;
	// go/build only needs their patterns.
	if hasEmbed {
		for _, c := range golisp.Comments(src, len(src)) {
			if strings.HasPrefix(c.Text, "//go:embed") {
				// c.Text has "//" for ";": shift the position so that
				// the patterns' positions come out right.
				if embs, err := parseGoEmbed(info.fset, tf.Pos(c.Off)-1, c.Text); err == nil {
					info.embeds = append(info.embeds, embs...)
				}
			}
		}
	}
	return nil
}
