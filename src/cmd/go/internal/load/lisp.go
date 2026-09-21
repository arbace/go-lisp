// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package load

import (
	"go/ast"
	"go/parser"
	"go/token"
	"internal/golisp"
	"io"
	"strings"
)

// parseTestFile parses a test file for the discovery of tests,
// benchmarks, fuzz targets, examples, and TestMain. Go files are parsed
// with parseGo (parser.ParseFile). For go-lisp files, it returns a syntax
// tree with just
// the package clause and the top-level functions, their Go names, and
// enough of their signatures for isTestFunc and checkTestFunc. go-lisp
// files have no comments in the tree, so examples are compiled but
// have no output to check.
func parseTestFile(fset *token.FileSet, filename string, src io.Reader,
	parseGo func(*token.FileSet, string, any, parser.Mode) (*ast.File, error)) (*ast.File, error) {
	if !golisp.IsFile(filename) {
		return parseGo(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}
	tf := fset.AddFile(filename, -1, len(data))
	tf.SetLinesForContent(data)
	s := golisp.NewScanner(data)
	h, err := golisp.ReadHeader(data)
	if h == nil {
		return nil, lispError(tf, err)
	}
	f := &ast.File{Package: tf.Pos(h.PackageOff), Name: &ast.Ident{NamePos: tf.Pos(h.PackageOff), Name: h.Package}}
	for {
		x, err := s.Next()
		if err == io.EOF {
			return f, nil
		}
		if err != nil {
			return nil, lispError(tf, err)
		}
		if d := lispFuncDecl(tf, x); d != nil {
			f.Decls = append(f.Decls, d)
		}
	}
}

func lispError(tf *token.File, err error) error {
	if e, ok := err.(*golisp.Error); ok {
		return scannerError(tf.Position(tf.Pos(e.Off)), e.Msg)
	}
	return err
}

type lispSyntaxError struct {
	pos token.Position
	msg string
}

func (e *lispSyntaxError) Error() string { return e.pos.String() + ": " + e.msg }

func scannerError(pos token.Position, msg string) error { return &lispSyntaxError{pos, msg} }

// lispFuncDecl returns the declaration of the function (func name [tparams]?
// [params] [results] body...) or method (func [recv] name ...), or nil.
func lispFuncDecl(tf *token.File, x *golisp.Form) *ast.FuncDecl {
	if x.Head() != "func" || len(x.Elems) < 4 {
		return nil
	}
	elems := x.Elems[1:]
	d := &ast.FuncDecl{}
	if elems[0].Kind == '[' {
		d.Recv = lispFieldList(tf, elems[0])
		elems = elems[1:]
	}
	if len(elems) < 3 || elems[0].Kind != 'a' {
		return nil
	}
	name := golisp.GoName(elems[0].Text, d.Recv != nil)
	if name == "" {
		return nil
	}
	d.Name = &ast.Ident{NamePos: tf.Pos(elems[0].Off), Name: name}
	elems = elems[1:]
	if len(elems) >= 3 && elems[0].Kind == '[' && elems[1].Kind == '[' && elems[2].Kind == '[' {
		elems = elems[1:] // type parameters
	}
	if elems[0].Kind != '[' || elems[1].Kind != '[' {
		return nil
	}
	d.Type = &ast.FuncType{
		Func:    tf.Pos(x.Off),
		Params:  lispFieldList(tf, elems[0]),
		Results: lispFieldList(tf, elems[1]),
	}
	d.Body = &ast.BlockStmt{Lbrace: tf.Pos(x.Off), Rbrace: tf.Pos(x.Off)}
	return d
}

// lispFieldList converts a parameter vector: flat name/type pairs, or
// types only if it starts with :_ or has odd length (golisp/SPEC.md D11).
func lispFieldList(tf *token.File, v *golisp.Form) *ast.FieldList {
	list := &ast.FieldList{Opening: tf.Pos(v.Off), Closing: tf.Pos(v.Off)}
	elems := v.Elems
	unnamed := len(elems)%2 == 1
	if len(elems) > 0 && elems[0].Kind == 'a' && elems[0].Text == ":_" {
		unnamed, elems = true, elems[1:]
	}
	if unnamed {
		for _, t := range elems {
			list.List = append(list.List, &ast.Field{Type: lispType(tf, t)})
		}
		return list
	}
	for i := 0; i+1 < len(elems); i += 2 {
		n := &ast.Ident{NamePos: tf.Pos(elems[i].Off), Name: golisp.GoName(elems[i].Text, false)}
		list.List = append(list.List, &ast.Field{Names: []*ast.Ident{n}, Type: lispType(tf, elems[i+1])})
	}
	return list
}

// lispType converts a type as far as test discovery needs:
// (* T), pkg.T, and T. Other types become _.
func lispType(tf *token.File, t *golisp.Form) ast.Expr {
	pos := tf.Pos(t.Off)
	switch {
	case t.Head() == "*" && len(t.Elems) == 2:
		return &ast.StarExpr{Star: pos, X: lispType(tf, t.Elems[1])}
	case t.Kind == 'a' && strings.Contains(t.Text, "."):
		i := strings.LastIndex(t.Text, ".")
		// The package name is kept as written (import names map verbatim).
		return &ast.SelectorExpr{
			X:   &ast.Ident{NamePos: pos, Name: t.Text[:i]},
			Sel: &ast.Ident{NamePos: pos, Name: golisp.GoName(t.Text[i+1:], true)},
		}
	case t.Kind == 'a':
		return &ast.Ident{NamePos: pos, Name: golisp.GoName(t.Text, false)}
	}
	return &ast.Ident{NamePos: pos, Name: "_"}
}
