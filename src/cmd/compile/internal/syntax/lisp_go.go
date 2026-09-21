// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements the go-lisp -> Go direction: syntax trees from
// ParseLisp have no parentheses, so they are added where Go needs them
// before the tree is printed as Go (SPEC F9).

package syntax

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
)

// LispToGo parses the go-lisp source src and returns it as Go source,
// keeping its ;go: directives as //go: directives. The result is valid
// Go but not gofmt-formatted.
func LispToGo(filename string, src io.Reader) ([]byte, error) {
	var dirs []lispDirective
	pragh := func(pos Pos, blank bool, text string, current Pragma) Pragma {
		if text != "" {
			dirs = append(dirs, lispDirective{pos, blank, text})
		}
		return current
	}
	f, err := ParseLisp(NewFileBase(filename), src, nil, pragh, 0)
	if err != nil {
		return nil, err
	}
	LispParenthesize(f)
	return lispWriteGo(f, dirs)
}

// lispWriteGo prints f as Go source with the given directives.
func lispWriteGo(f *File, dirs []lispDirective) ([]byte, error) {
	sort.SliceStable(dirs, func(i, j int) bool { return dirs[i].Pos.Cmp(dirs[j].Pos) < 0 })
	var b strings.Builder
	flush := func(pos Pos, indent string) {
		for len(dirs) > 0 && (!pos.IsKnown() || dirs[0].Pos.Cmp(pos) < 0) {
			b.WriteString(indent + "//" + dirs[0].Text + "\n")
			dirs = dirs[1:]
		}
	}
	print := func(n Node) error {
		_, err := Fprint(&b, n, 0)
		return err
	}

	if len(dirs) > 0 && dirs[0].Pos.Cmp(f.Pos()) < 0 {
		flush(f.Pos(), "")
		b.WriteString("\n") // a //go:build line must be followed by a blank line
	}
	b.WriteString("package " + f.PkgName.Value + "\n")
	for list := f.DeclList; len(list) > 0; {
		n := lispDeclRun(list)
		b.WriteString("\n")
		flush(list[0].Pos(), "")
		if lispDeclGroup(list[0]) == nil {
			if err := print(list[0]); err != nil {
				return nil, err
			}
		} else {
			var kw string
			switch list[0].(type) {
			case *ImportDecl:
				kw = "import"
			case *ConstDecl:
				kw = "const"
			case *TypeDecl:
				kw = "type"
			case *VarDecl:
				kw = "var"
			}
			b.WriteString(kw + " (\n")
			for _, d := range list[:n] {
				flush(d.Pos(), "\t")
				b.WriteString("\t")
				if err := print(d); err != nil {
					return nil, err
				}
				b.WriteString("\n")
			}
			b.WriteString(")")
		}
		b.WriteString("\n")
		list = list[n:]
	}
	flush(Pos{}, "")
	return []byte(b.String()), nil
}

// LispParenthesize prepares a syntax tree without parentheses (such as
// one produced by ParseLisp) for printing as Go: it adds the parentheses
// that Go syntax needs, and replaces the few constructs the syntax
// printer cannot print by equivalent ones.
func LispParenthesize(root Node) {
	Inspect(root, func(n Node) bool {
		switch n := n.(type) {
		case *Operation:
			if n.Y == nil {
				if lispUnaryNeedsParens(n.Op, n.X) {
					n.X = lispParen(n.X)
				}
				break
			}
			p := lispPrec(n.Op)
			if lispExprPrec(n.X) < p {
				n.X = lispParen(n.X)
			}
			if lispExprPrec(n.Y) <= p {
				n.Y = lispParen(n.Y)
			}
		case *SelectorExpr:
			n.X = lispParenOperand(n.X)
		case *IndexExpr:
			n.X = lispParenOperand(n.X)
		case *SliceExpr:
			n.X = lispParenOperand(n.X)
		case *AssertExpr:
			n.X = lispParenOperand(n.X)
		case *TypeSwitchGuard:
			n.X = lispParenOperand(n.X)
		case *CallExpr:
			n.Fun = lispParenOperand(n.Fun)
			// f(1 ...) must not print as f(1...), which lexes as 1. and ..
			if k := len(n.ArgList) - 1; n.HasDots && k >= 0 && lispEndsWithNumber(n.ArgList[k]) {
				n.ArgList[k] = lispParen(n.ArgList[k])
			}
		case *ExprStmt:
			n.X = lispParenLeadingBrace(n.X)
		case *AssignStmt:
			n.Lhs = lispParenLeadingBrace(n.Lhs)
		case *SendStmt:
			n.Chan = lispParenLeadingBrace(n.Chan)
		case *BlockStmt:
			n.List = lispDropEmptyDecls(n.List)
		case *CaseClause:
			n.Body = lispDropEmptyDecls(n.Body)
		case *CommClause:
			n.Body = lispDropEmptyDecls(n.Body)
		case *LabeledStmt:
			if d, ok := n.Stmt.(*DeclStmt); ok && len(d.DeclList) == 0 {
				n.Stmt = lispEmptyStmt(d)
			}
		case *ChanType:
			// chan (<-chan T): without parentheses, chan <-chan T is chan<- (chan T)
			if c, ok := n.Elem.(*ChanType); ok && n.Dir == 0 && c.Dir == RecvOnly {
				n.Elem = lispParen(n.Elem)
			}
		case *FuncDecl:
			lispParenTerms(n.TParamList)
		case *InterfaceType:
			lispParenTerms(n.MethodList)
		case *TypeDecl:
			lispParenTerms(n.TParamList)
			// type T [N]E: an array length that the parser would read
			// as a type parameter list ([P C]) needs parentheses. Only
			// invalid array lengths can look like that, but the Go
			// parser accepts them.
			if a, ok := n.Type.(*ArrayType); ok && n.TParamList == nil && a.Len != nil {
				if name, rest := extractName(a.Len, false); name != nil && rest != nil || lispStartsWithIndexedName(a.Len) {
					a.Len = lispParen(a.Len)
				}
			}
		case *IfStmt:
			lispParenComplits(&n.Init, &n.Cond)
		case *ForStmt:
			lispParenComplits(&n.Init, &n.Cond, &n.Post)
		case *SwitchStmt:
			lispParenComplits(&n.Init, &n.Tag)
		}
		return true
	})
}

// lispStartsWithIndexedName reports whether x, printed as Go, starts
// with a name followed by '[' (as in a[i] + 1), which after "type T ["
// reads as the start of a type parameter list.
func lispStartsWithIndexedName(x Expr) bool {
	for {
		switch e := x.(type) {
		case *Operation:
			if e.Y == nil {
				return false
			}
			x = e.X
		case *CallExpr:
			x = e.Fun
		case *SelectorExpr:
			x = e.X
		case *AssertExpr:
			x = e.X
		case *IndexExpr:
			if _, ok := e.X.(*Name); ok {
				return true
			}
			x = e.X
		case *SliceExpr:
			if _, ok := e.X.(*Name); ok {
				return true
			}
			x = e.X
		default:
			return false
		}
	}
}

func lispParen(x Expr) Expr {
	p := new(ParenExpr)
	p.pos = x.Pos()
	p.X = x
	return p
}

// lispPrec returns the precedence of the binary operator op.
func lispPrec(op Operator) int {
	switch op {
	case OrOr:
		return precOrOr
	case AndAnd:
		return precAndAnd
	case Eql, Neq, Lss, Leq, Gtr, Geq:
		return precCmp
	case Add, Sub, Or, Xor:
		return precAdd
	}
	return precMul
}

// lispExprPrec returns the precedence of x as an operand:
// binary operations have their operator's, everything else binds tighter.
func lispExprPrec(x Expr) int {
	if o, ok := x.(*Operation); ok && o.Y != nil {
		return lispPrec(o.Op)
	}
	return precMul + 1
}

// lispUnaryNeedsParens reports whether the operand x of the unary
// operator op must be parenthesized: binary operations, and unary
// operations whose printed operators would combine into one token
// (- -x is not --x, & &x is not &&x, & ^x is not &^x).
func lispUnaryNeedsParens(op Operator, x Expr) bool {
	o, ok := x.(*Operation)
	if !ok {
		return false
	}
	if o.Y != nil {
		return true
	}
	switch op {
	case Sub, Add:
		return o.Op == op
	case And:
		return o.Op == And || o.Op == Xor
	}
	return false
}

// lispParenOperand parenthesizes x if it is the operand of a selector,
// index, slice, type assertion, or call and cannot stand there bare:
// operations ((*p).f, (*T)(x), (a+b)[i]), function and channel types
// used in conversions ((func())(f), (<-chan int)(c)), and numbers
// ((1).f, since 1.f lexes as 1. f).
func lispParenOperand(x Expr) Expr {
	switch x.(type) {
	case *Operation, *FuncType, *ChanType:
		return lispParen(x)
	}
	if lispIsNumber(x) {
		return lispParen(x)
	}
	return x
}

// lispParenTerms parenthesizes the literals among the union terms of
// type parameter constraints and interface elements: the Go parser
// expects a type there and accepts a literal only in parentheses. (Only
// invalid programs have such terms, but the Go parser accepts them.)
func lispParenTerms(list []*Field) {
	var term func(x Expr) Expr
	term = func(x Expr) Expr {
		switch e := x.(type) {
		case *BasicLit:
			return lispParen(e)
		case *Operation:
			if e.Op == Or && e.Y != nil {
				e.X, e.Y = term(e.X), term(e.Y)
			}
		}
		return x
	}
	for _, f := range list {
		if _, method := f.Type.(*FuncType); method && f.Name != nil {
			continue
		}
		f.Type = term(f.Type)
	}
}

// lispEndsWithNumber reports whether x, printed as Go, ends with a
// number literal (as 1, a + 1, or -1 do).
func lispEndsWithNumber(x Expr) bool {
	for {
		switch e := x.(type) {
		case *Operation:
			if e.Y != nil {
				x = e.Y
			} else {
				x = e.X
			}
		default:
			return lispIsNumber(x)
		}
	}
}

// lispParenLeadingBrace parenthesizes x if, printed as Go, it would start
// with '{' (a composite literal without a type, as in {}.f), which at the
// start of a statement reads as a block, or with '~', which cannot start
// a statement.
func lispParenLeadingBrace(x Expr) Expr {
	for e := x; ; {
		switch y := e.(type) {
		case *CompositeLit:
			if y.Type == nil {
				return lispParen(x)
			}
			return x
		case *Operation:
			if y.Y == nil {
				if y.Op == Tilde {
					return lispParen(x)
				}
				return x // starts with the operator
			}
			e = y.X
		case *CallExpr:
			e = y.Fun
		case *SelectorExpr:
			e = y.X
		case *IndexExpr:
			e = y.X
		case *SliceExpr:
			e = y.X
		case *AssertExpr:
			e = y.X
		case *ListExpr:
			if len(y.ElemList) == 0 {
				return x
			}
			// only the first element starts the statement
			y.ElemList[0] = lispParenLeadingBrace(y.ElemList[0])
			return x
		default:
			return x
		}
	}
}

func lispIsNumber(x Expr) bool {
	b, ok := x.(*BasicLit)
	return ok && (b.Kind == IntLit || b.Kind == FloatLit || b.Kind == ImagLit)
}

// lispDropEmptyDecls replaces empty declaration statements (Go's
// var ()) by empty statements, which mean the same: the syntax
// printer cannot print a DeclStmt without declarations.
func lispDropEmptyDecls(list []Stmt) []Stmt {
	for i, s := range list {
		if d, ok := s.(*DeclStmt); ok && len(d.DeclList) == 0 {
			list[i] = lispEmptyStmt(d)
		}
	}
	return list
}

func lispEmptyStmt(n Node) *EmptyStmt {
	s := new(EmptyStmt)
	s.pos = n.Pos()
	return s
}

// lispParenComplits parenthesizes the composite literals with a type
// name in if, for, and switch headers, where T{ would be read as the
// start of the statement's block. Function literal bodies are skipped.
func lispParenComplits(fields ...any) {
	for _, f := range fields {
		lispRewriteExprs(reflect.ValueOf(f).Elem(), func(x Expr) Expr {
			if c, ok := x.(*CompositeLit); ok && c.Type != nil {
				return lispParen(c)
			}
			return x
		})
	}
}

var (
	lispExprType     = reflect.TypeFor[Expr]()
	lispFuncLitType  = reflect.TypeFor[*FuncLit]()
	lispParenExprTyp = reflect.TypeFor[*ParenExpr]()
)

// lispRewriteExprs replaces every expression e reachable from v (an
// addressable value) by f(e), without descending into function literals,
// into composite literals that f replaced, or into parentheses.
func lispRewriteExprs(v reflect.Value, f func(Expr) Expr) {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		e := v.Elem()
		if v.Type() == lispExprType {
			if e.Type() == lispFuncLitType || e.Type() == lispParenExprTyp {
				return
			}
			if y := f(v.Interface().(Expr)); y != v.Interface() {
				v.Set(reflect.ValueOf(y))
				return
			}
		}
		lispRewriteExprs(e, f)
	case reflect.Pointer:
		if !v.IsNil() && v.Type() != lispFuncLitType {
			lispRewriteExprs(v.Elem(), f)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				lispRewriteExprs(v.Field(i), f)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			lispRewriteExprs(v.Index(i), f)
		}
	}
}

// LispToGoLines is like LispToGo, but the Go source it returns has
// /*line*/ directives that map declarations, statements, clauses,
// function literals, and closing braces back to their positions in the
// go-lisp source src (named filename). Tools that read Go, such as the
// coverage tool, can then report positions in the go-lisp file.
func LispToGoLines(filename string, src io.Reader) ([]byte, error) {
	var dirs []lispDirective
	pragh := func(pos Pos, blank bool, text string, current Pragma) Pragma {
		if text != "" {
			dirs = append(dirs, lispDirective{pos, blank, text})
		}
		return current
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}
	fl, err := ParseLisp(NewFileBase(filename), bytes.NewReader(data), nil, pragh, 0)
	if err != nil {
		return nil, err
	}
	LispParenthesize(fl)
	out, err := lispWriteGo(fl, dirs)
	if err != nil {
		return nil, err
	}
	fg, err := Parse(NewFileBase(filename+".go"), bytes.NewReader(out), nil, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("go-lisp to Go: generated Go does not parse: %v", err)
	}

	// Pair up the positions of corresponding nodes of the two trees.
	var marks []lispMark
	lispPairPositions(reflect.ValueOf(fl), reflect.ValueOf(fg), &marks)

	goOffset := lispOffsetFunc(out)
	lispOffset := lispOffsetFunc(data)
	offset := func(pos Pos) int { return goOffset(pos.Line(), pos.Col()) }
	sort.SliceStable(marks, func(i, j int) bool { return offset(marks[i].goPos) < offset(marks[j].goPos) })

	var b bytes.Buffer
	last := -1
	for _, m := range marks {
		off := offset(m.goPos)
		if off <= last || off > len(out) || !m.lispPos.IsKnown() {
			continue // at most one directive per position
		}
		// Point at a form's '(' rather than at its head symbol.
		line, col := m.lispPos.RelLine(), m.lispPos.RelCol()
		if lo := lispOffset(m.lispPos.Line(), m.lispPos.Col()); lo > 0 && lo <= len(data) && data[lo-1] == '(' && col > 1 {
			col--
		}
		b.Write(out[max(last, 0):off])
		fmt.Fprintf(&b, "/*line %s:%d:%d*/", filename, line, col)
		last = off
	}
	if last < 0 {
		last = 0
	}
	b.Write(out[last:])
	return b.Bytes(), nil
}

// lispOffsetFunc returns a function that maps a line and column
// of src to a byte offset, or -1.
func lispOffsetFunc(src []byte) func(line, col uint) int {
	lineStart := []int{0}
	for i, c := range src {
		if c == '\n' {
			lineStart = append(lineStart, i+1)
		}
	}
	return func(line, col uint) int {
		if line < 1 || int(line) > len(lineStart) || col < 1 {
			return -1
		}
		return lineStart[line-1] + int(col) - 1
	}
}

// A lispMark pairs the position of a node in generated Go source with the
// position of the corresponding node in go-lisp source.
type lispMark struct {
	goPos, lispPos Pos
}

// lispPairPositions walks the go-lisp tree l and the tree g of the Go
// source generated from it in lockstep and records the positions of
// corresponding declarations, statements, clauses, function literals,
// and closing braces. The trees have the same shape; where they do not
// (which would be a bug), the walk stops for that subtree.
func lispPairPositions(l, g reflect.Value, marks *[]lispMark) {
	if l.Kind() != g.Kind() {
		return
	}
	switch l.Kind() {
	case reflect.Interface, reflect.Pointer:
		if l.IsNil() || g.IsNil() {
			return
		}
		if l.Kind() == reflect.Interface && l.Elem().Type() != g.Elem().Type() {
			return
		}
		if ln, ok := l.Interface().(Node); ok && l.Kind() == reflect.Pointer {
			gn := g.Interface().(Node)
			switch ln := ln.(type) {
			case *FuncDecl:
				// The func keyword, where the coverage tool takes a
				// function's position from: the generated Go starts
				// function declarations at the beginning of a line.
				p := gn.Pos()
				*marks = append(*marks, lispMark{MakePos(p.Base(), p.Line(), colbase), ln.Pos()})
			case *FuncLit:
				*marks = append(*marks, lispMark{gn.Pos(), ln.Pos()})
			case Stmt, *CaseClause, *CommClause:
				*marks = append(*marks, lispMark{StartPos(gn), ln.Pos()})
			}
			switch ln := ln.(type) {
			case *BlockStmt:
				*marks = append(*marks, lispMark{gn.(*BlockStmt).Rbrace, ln.Rbrace})
			case *SwitchStmt:
				*marks = append(*marks, lispMark{gn.(*SwitchStmt).Rbrace, ln.Rbrace})
			case *SelectStmt:
				*marks = append(*marks, lispMark{gn.(*SelectStmt).Rbrace, ln.Rbrace})
			}
		}
		lispPairPositions(l.Elem(), g.Elem(), marks)
	case reflect.Struct:
		for i := range l.NumField() {
			if l.Type().Field(i).IsExported() {
				lispPairPositions(l.Field(i), g.Field(i), marks)
			}
		}
	case reflect.Slice:
		ls, gs := lispSliceNodes(l), lispSliceNodes(g)
		if len(ls) != len(gs) {
			return
		}
		for i := range ls {
			lispPairPositions(ls[i], gs[i], marks)
		}
	case reflect.Array:
		for i := range l.Len() {
			lispPairPositions(l.Index(i), g.Index(i), marks)
		}
	}
}

// lispSliceNodes returns the elements of a slice value, without the empty
// statements in statement lists, which Go's printer prints as nothing.
func lispSliceNodes(v reflect.Value) []reflect.Value {
	var list []reflect.Value
	for i := range v.Len() {
		e := v.Index(i)
		if _, ok := e.Interface().(*EmptyStmt); ok {
			continue
		}
		list = append(list, e)
	}
	return list
}
