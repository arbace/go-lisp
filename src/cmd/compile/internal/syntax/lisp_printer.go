// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements printing of syntax trees as go-lisp source
// (the Go -> Lisp direction), following golisp/SPEC.md.

package syntax

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// lispPrint writes the go-lisp form of the file f to w.
// Directives (//go: comments collected while parsing f) are
// printed as ;go: comments before the declaration that follows them.
func lispPrint(w io.Writer, f *File, dirs []lispDirective) (err error) {
	defer func() {
		if e := recover(); e != nil {
			if pe, ok := e.(lispPrintError); ok {
				err = pe
				return
			}
			panic(e)
		}
	}()

	var imports []string
	for _, d := range f.DeclList {
		if d, ok := d.(*ImportDecl); ok && d.Path != nil {
			path, _ := strconv.Unquote(d.Path.Value)
			alias := ""
			if d.LocalPkgName != nil {
				alias = d.LocalPkgName.Value
			}
			if name := lispImportName(alias, path); name != "" {
				imports = append(imports, name)
			}
		}
	}

	p := &lispPrinter{names: newLispNamer(imports)}
	p.dirs = append(p.dirs, dirs...)
	sort.SliceStable(p.dirs, func(i, j int) bool { return p.dirs[i].Pos.Cmp(p.dirs[j].Pos) < 0 })
	p.file(f)
	_, err = io.WriteString(w, p.buf.String())
	return err
}

// A lispPrintError reports a syntax tree that cannot be printed.
type lispPrintError struct {
	Pos Pos
	Msg string
}

func (e lispPrintError) Error() string { return fmt.Sprintf("%s: %s", e.Pos, e.Msg) }

type lispPrinter struct {
	buf    strings.Builder
	names  *lispNamer
	indent int
	dirs   []lispDirective // pending directives, in source order
}

func (p *lispPrinter) errorf(n Node, format string, args ...any) {
	panic(lispPrintError{n.Pos(), fmt.Sprintf(format, args...)})
}

func (p *lispPrinter) print(args ...string) {
	for _, s := range args {
		p.buf.WriteString(s)
	}
}

// nl starts a new line at the current indentation.
func (p *lispPrinter) nl() {
	p.buf.WriteByte('\n')
	for range p.indent {
		p.buf.WriteString("  ")
	}
}

// flushDirectives prints the pending directives positioned before pos,
// each on its own line.
func (p *lispPrinter) flushDirectives(pos Pos) {
	for len(p.dirs) > 0 && (!pos.IsKnown() || p.dirs[0].Pos.Cmp(pos) < 0) {
		p.print(";", p.dirs[0].Text)
		p.nl()
		p.dirs = p.dirs[1:]
	}
}

// ----------------------------------------------------------------------------
// Names

func (p *lispPrinter) bare(n *Name) string   { return p.names.lispName(n.Value, false, false, false) }
func (p *lispPrinter) decl(n *Name) string   { return p.names.lispName(n.Value, false, true, false) }
func (p *lispPrinter) member(n *Name) string { return p.names.lispName(n.Value, true, false, false) }

// ----------------------------------------------------------------------------
// Files and declarations

func (p *lispPrinter) file(f *File) {
	p.flushDirectives(f.Pos())
	p.print("(package ", f.PkgName.Value, ")")
	p.nl()

	list := f.DeclList
	for len(list) > 0 {
		n := lispDeclRun(list)
		p.nl()
		p.flushDirectives(list[0].Pos())
		p.decls(list[:n])
		p.nl()
		list = list[n:]
	}
	p.flushDirectives(Pos{}) // any directives after the last declaration
}

// lispDeclRun returns the length of the run of declarations at the start
// of list that belong to the same group (1 if list[0] is not grouped).
func lispDeclRun(list []Decl) int {
	g := lispDeclGroup(list[0])
	if g == nil {
		return 1
	}
	n := 1
	for n < len(list) && lispDeclGroup(list[n]) == g {
		n++
	}
	return n
}

func lispDeclGroup(d Decl) *Group {
	switch d := d.(type) {
	case *ImportDecl:
		return d.Group
	case *ConstDecl:
		return d.Group
	case *TypeDecl:
		return d.Group
	case *VarDecl:
		return d.Group
	}
	return nil
}

// decls prints a single declaration or a group of declarations.
func (p *lispPrinter) decls(list []Decl) {
	var kw string
	switch d := list[0].(type) {
	case *FuncDecl:
		p.funcDecl(d)
		return
	case *ImportDecl:
		kw = "import"
	case *ConstDecl:
		kw = "const"
	case *TypeDecl:
		kw = "type"
	case *VarDecl:
		kw = "var"
	default:
		p.errorf(d, "unexpected declaration %T", d)
	}

	p.print("(", kw)
	switch {
	case kw == "import" && len(list) == 1:
		// A single import spec is a single declaration (SPEC §2).
		p.print(" ")
		p.spec(list[0])
	case kw == "import":
		p.indent++
		for _, d := range list {
			p.nl()
			p.flushDirectives(d.Pos())
			p.spec(d)
		}
		p.indent--
	case lispDeclGroup(list[0]) == nil:
		p.print(" ")
		p.spec(list[0])
	default:
		// A group is a form whose arguments are all spec lists (D4).
		p.indent++
		for _, d := range list {
			p.nl()
			p.flushDirectives(d.Pos())
			p.print("(")
			p.spec(d)
			p.print(")")
		}
		p.indent--
	}
	p.print(")")
}

// spec prints the body of a declaration spec (without the keyword).
func (p *lispPrinter) spec(d Decl) {
	switch d := d.(type) {
	case *ImportDecl:
		if d.LocalPkgName != nil {
			// explicit import names map verbatim (SPEC A12)
			p.print("[", d.LocalPkgName.Value, " ", d.Path.Value, "]")
		} else {
			p.print(d.Path.Value)
		}

	case *ConstDecl:
		p.valueSpec(d.NameList, d.Type, d.Values)

	case *VarDecl:
		p.valueSpec(d.NameList, d.Type, d.Values)

	case *TypeDecl:
		p.print(p.decl(d.Name))
		if d.TParamList != nil {
			p.print(" ")
			p.fields(d.TParamList)
		}
		if d.Alias {
			p.print(" =")
		}
		p.print(" ")
		p.expr(d.Type)

	default:
		p.errorf(d, "unexpected declaration %T", d)
	}
}

// valueSpec prints names type? (= values...)?.
func (p *lispPrinter) valueSpec(names []*Name, typ, values Expr) {
	if len(names) == 1 {
		p.print(p.decl(names[0]))
	} else {
		p.print("[")
		for i, n := range names {
			if i > 0 {
				p.print(" ")
			}
			p.print(p.decl(n))
		}
		p.print("]")
	}
	if typ != nil {
		p.print(" ")
		p.expr(typ)
	}
	if values != nil {
		p.print(" =")
		for _, x := range lispUnpackList(values) {
			p.print(" ")
			p.expr(x)
		}
	}
}

func (p *lispPrinter) funcDecl(d *FuncDecl) {
	p.print("(func ")
	if d.Recv != nil {
		p.fields([]*Field{d.Recv})
		p.print(" ", p.member(d.Name)) // method names are member names
	} else {
		p.print(p.decl(d.Name))
	}
	if d.TParamList != nil {
		p.print(" ")
		p.fields(d.TParamList)
	}
	p.print(" ")
	p.signature(d.Type)
	if d.Body != nil {
		p.funcBody(d.Body)
	}
	p.print(")")
}

// funcBody prints a function body. An empty body is written () so that
// it is distinct from a missing body (SPEC A6).
func (p *lispPrinter) funcBody(b *BlockStmt) {
	if len(b.List) == 0 {
		p.print(" ()")
		return
	}
	p.body(b.List)
}

func (p *lispPrinter) signature(t *FuncType) {
	p.fields(t.ParamList)
	p.print(" ")
	p.fields(t.ResultList)
}

// fields prints a parameter, result, receiver, or type parameter list
// as flat name/type pairs separated by commas, or as a list of types
// if the fields are unnamed (D11).
func (p *lispPrinter) fields(list []*Field) {
	p.print("[")
	named := len(list) > 0 && list[0].Name != nil
	if !named && len(list)%2 == 0 && len(list) > 0 {
		// an even number of unnamed types needs the :_ marker
		p.print(":_ ")
	}
	for i, f := range list {
		if i > 0 {
			if named {
				p.print(", ")
			} else {
				p.print(" ")
			}
		}
		if named {
			if f.Name == nil {
				p.errorf(f, "mixed named and unnamed parameters")
			}
			p.print(p.decl(f.Name), " ")
		}
		p.expr(f.Type)
	}
	p.print("]")
}

// ----------------------------------------------------------------------------
// Expressions

// lispNaryOps are the binary operators written n-ary, folding to the
// left; comparisons are strictly binary (SPEC A9).
var lispNaryOps = map[Operator]bool{
	OrOr: true, AndAnd: true, Add: true, Sub: true, Or: true, Xor: true,
	Mul: true, Div: true, Rem: true, And: true, AndNot: true, Shl: true, Shr: true,
}

func lispUnparen(x Expr) Expr {
	for {
		p, ok := x.(*ParenExpr)
		if !ok {
			return x
		}
		x = p.X
	}
}

// lispUnpackList returns the elements of a ListExpr, or x itself.
func lispUnpackList(x Expr) []Expr {
	if l, ok := x.(*ListExpr); ok {
		return l.ElemList
	}
	return []Expr{x}
}

func (p *lispPrinter) exprList(list []Expr) {
	for _, x := range list {
		p.print(" ")
		p.expr(x)
	}
}

func (p *lispPrinter) expr(x Expr) {
	switch x := x.(type) {
	case nil:
		p.print(":_") // omitted (SPEC Q1)

	case *BadExpr:
		p.errorf(x, "cannot print BadExpr")

	case *Name:
		p.print(p.bare(x))

	case *BasicLit:
		p.print(x.Value)

	case *CompositeLit:
		p.print("(:lit ")
		if x.Type == nil {
			p.print(":_")
		} else {
			p.expr(x.Type)
		}
		multi := len(x.ElemList) > 4
		if multi {
			p.indent++
		}
		for _, e := range x.ElemList {
			if multi {
				p.nl()
			} else {
				p.print(" ")
			}
			p.expr(e)
		}
		if multi {
			p.indent--
		}
		p.print(")")

	case *KeyValueExpr:
		// Keys are printed with bare rules, which is always correct:
		// the printer cannot tell field names from map keys (SPEC §4).
		p.print("(:kv ")
		p.expr(x.Key)
		p.print(" ")
		p.expr(x.Value)
		p.print(")")

	case *FuncLit:
		p.print("(func ")
		p.signature(x.Type)
		p.funcBody(x.Body)
		p.print(")")

	case *ParenExpr:
		p.expr(x.X) // go-lisp has no parentheses (SPEC A8)

	case *SelectorExpr:
		p.selector(x)

	case *IndexExpr:
		p.print("(:index ")
		p.expr(x.X)
		p.exprList(lispUnpackList(x.Index))
		p.print(")")

	case *SliceExpr:
		p.print("(:slice ")
		p.expr(x.X)
		n := 2
		if x.Full {
			n = 3
		}
		for _, i := range x.Index[:n] {
			p.print(" ")
			p.expr(i)
		}
		p.print(")")

	case *AssertExpr:
		p.print("(:assert ")
		p.expr(x.X)
		p.print(" ")
		p.expr(x.Type)
		p.print(")")

	case *TypeSwitchGuard:
		// general form of x.(type) (SPEC F2)
		if x.Lhs != nil {
			p.print("(:= ", p.decl(x.Lhs), " ")
		}
		p.print("(:assert ")
		p.expr(x.X)
		p.print(" type)")
		if x.Lhs != nil {
			p.print(")")
		}

	case *Operation:
		p.operation(x)

	case *CallExpr:
		p.print("(")
		p.expr(x.Fun)
		p.exprList(x.ArgList)
		if x.HasDots {
			p.print(" ...")
		}
		p.print(")")

	case *ListExpr:
		for i, e := range x.ElemList {
			if i > 0 {
				p.print(" ")
			}
			p.expr(e)
		}

	case *ArrayType:
		p.print("(:array-of ")
		if x.Len == nil {
			p.print("...")
		} else {
			p.expr(x.Len)
		}
		p.print(" ")
		p.expr(x.Elem)
		p.print(")")

	case *SliceType:
		p.print("(:slice-of ")
		p.expr(x.Elem)
		p.print(")")

	case *DotsType:
		p.print("(... ")
		p.expr(x.Elem)
		p.print(")")

	case *StructType:
		p.structType(x)

	case *InterfaceType:
		p.interfaceType(x)

	case *FuncType:
		p.print("(func ")
		p.signature(x)
		p.print(")")

	case *MapType:
		p.print("(map ")
		p.expr(x.Key)
		p.print(" ")
		p.expr(x.Value)
		p.print(")")

	case *ChanType:
		switch x.Dir {
		case 0:
			p.print("(chan ")
		case SendOnly:
			p.print("(chan<- ")
		case RecvOnly:
			p.print("(<-chan ")
		}
		p.expr(x.Elem)
		p.print(")")

	default:
		panic(fmt.Sprintf("lispPrinter: unexpected expression %T", x))
	}
}

// selector prints x as a dotted symbol if its base is a name,
// and as (:sel base a.b...) otherwise (D12).
func (p *lispPrinter) selector(x *SelectorExpr) {
	var sels []string
	var base Expr = x
	for {
		s, ok := lispUnparen(base).(*SelectorExpr)
		if !ok {
			break
		}
		sels = append(sels, p.member(s.Sel))
		base = s.X
	}
	// sels are in reverse order
	for i, j := 0, len(sels)-1; i < j; i, j = i+1, j-1 {
		sels[i], sels[j] = sels[j], sels[i]
	}
	if n, ok := lispUnparen(base).(*Name); ok {
		p.print(p.bare(n), ".", strings.Join(sels, "."))
		return
	}
	p.print("(:sel ")
	p.expr(base)
	p.print(" ", strings.Join(sels, "."), ")")
}

func (p *lispPrinter) operation(x *Operation) {
	if x.Y == nil {
		p.print("(", x.Op.String(), " ")
		p.expr(x.X)
		p.print(")")
		return
	}
	// flatten left-nested chains of the same n-ary operator
	operands := []Expr{x.Y}
	l := x.X
	if lispNaryOps[x.Op] {
		for {
			o, ok := lispUnparen(l).(*Operation)
			if !ok || o.Op != x.Op || o.Y == nil {
				break
			}
			operands = append(operands, o.Y)
			l = o.X
		}
	}
	operands = append(operands, l)
	p.print("(", x.Op.String())
	for i := len(operands) - 1; i >= 0; i-- {
		p.print(" ")
		p.expr(operands[i])
	}
	p.print(")")
}

func (p *lispPrinter) structType(x *StructType) {
	if len(x.FieldList) == 0 {
		p.print("(struct)")
		return
	}
	p.print("(struct")
	p.indent++
	list := x.FieldList
	for i := 0; i < len(list); {
		f := list[i]
		p.nl()
		p.print("[")
		j := i + 1
		if f.Name != nil {
			// a field group shares one type (D13)
			for j < len(list) && list[j].Name != nil && list[j].Type == f.Type {
				j++
			}
			for _, g := range list[i:j] {
				p.print(p.member(g.Name), " ")
			}
		}
		p.expr(f.Type)
		if k := j - 1; k < len(x.TagList) && x.TagList[k] != nil {
			p.print(" ", x.TagList[k].Value)
		}
		p.print("]")
		i = j
	}
	p.indent--
	p.print(")")
}

func (p *lispPrinter) interfaceType(x *InterfaceType) {
	if len(x.MethodList) == 0 {
		p.print("(interface)")
		return
	}
	p.print("(interface")
	p.indent++
	for _, m := range x.MethodList {
		p.nl()
		if m.Name == nil {
			p.expr(m.Type) // embedded type or type set term
			continue
		}
		t, ok := m.Type.(*FuncType)
		if !ok {
			p.errorf(m, "interface method %s has no function type", m.Name.Value)
		}
		p.print("(", p.names.lispName(m.Name.Value, true, false, true), " ")
		p.signature(t)
		p.print(")")
	}
	p.indent--
	p.print(")")
}

// ----------------------------------------------------------------------------
// Statements

// body prints a statement list, one statement per line.
func (p *lispPrinter) body(list []Stmt) {
	p.indent++
	for _, s := range list {
		p.nl()
		p.stmt(s)
	}
	p.indent--
}

func (p *lispPrinter) stmt(s Stmt) {
	switch s := s.(type) {
	case *EmptyStmt:
		p.print("()")

	case *LabeledStmt:
		p.print("(:label ", p.decl(s.Label))
		p.body([]Stmt{s.Stmt})
		p.print(")")

	case *BlockStmt:
		p.print("(:block")
		p.body(s.List)
		p.print(")")

	case *ExprStmt:
		p.expr(s.X)

	case *SendStmt:
		p.print("(<- ")
		p.expr(s.Chan)
		p.print(" ")
		p.expr(s.Value)
		p.print(")")

	case *DeclStmt:
		list := s.DeclList
		if len(list) == 0 {
			// An empty group (var () in Go); the syntax tree does not
			// record the keyword, and any keyword gives the same tree.
			p.print("(var)")
		}
		for i := 0; len(list) > 0; i++ {
			if i > 0 {
				p.nl()
			}
			n := lispDeclRun(list)
			p.decls(list[:n])
			list = list[n:]
		}

	case *AssignStmt:
		p.assign(s)

	case *BranchStmt:
		p.print("(", s.Tok.String())
		if s.Label != nil {
			p.print(" ", p.bare(s.Label))
		}
		p.print(")")

	case *CallStmt:
		p.print("(", s.Tok.String(), " ")
		p.expr(s.Call)
		p.print(")")

	case *ReturnStmt:
		p.print("(return")
		if s.Results != nil {
			p.exprList(lispUnpackList(s.Results))
		}
		p.print(")")

	case *IfStmt:
		p.ifStmt(s)

	case *ForStmt:
		p.print("(for ")
		p.forHeader(s)
		p.body(s.Body.List)
		p.print(")")

	case *SwitchStmt:
		p.switchStmt(s)

	case *SelectStmt:
		p.print("(select")
		p.indent++
		for _, c := range s.Body {
			p.nl()
			if c.Comm == nil {
				p.print("(default")
			} else {
				p.print("(case ")
				p.stmt(c.Comm)
			}
			p.body(c.Body)
			p.print(")")
		}
		p.indent--
		p.print(")")

	default:
		panic(fmt.Sprintf("lispPrinter: unexpected statement %T", s))
	}
}

func (p *lispPrinter) assign(s *AssignStmt) {
	switch {
	case s.Rhs == nil:
		// x++ or x--
		p.print("(", s.Op.String(), s.Op.String(), " ")
		p.expr(s.Lhs)
		p.print(")")
		return
	case s.Op == 0:
		p.print("(= ")
	case s.Op == Def:
		p.print("(:= ")
	default:
		p.print("(", s.Op.String(), "= ")
	}

	lhs := lispUnpackList(s.Lhs)
	lhsExpr := p.expr
	if s.Op == Def {
		lhsExpr = p.defLhs
	}
	if len(lhs) == 1 {
		lhsExpr(lhs[0])
	} else {
		p.print("[")
		for i, x := range lhs {
			if i > 0 {
				p.print(" ")
			}
			lhsExpr(x)
		}
		p.print("]")
	}
	p.exprList(lispUnpackList(s.Rhs))
	p.print(")")
}

// defLhs prints an operand on the left side of :=; names are declared.
func (p *lispPrinter) defLhs(x Expr) {
	if n, ok := x.(*Name); ok {
		p.print(p.decl(n))
		return
	}
	p.expr(x)
}

func lispAllNames(list []Expr) bool {
	for _, x := range list {
		if _, ok := x.(*Name); !ok {
			return false
		}
	}
	return true
}

// initStmt prints an init statement in a one-element vector.
func (p *lispPrinter) initStmt(s SimpleStmt) {
	if s != nil {
		p.print(" [")
		p.stmt(s)
		p.print("]")
	}
}

func (p *lispPrinter) ifStmt(s *IfStmt) {
	p.print("(if")
	p.ifClause(s)
	p.indent++
	for e := s.Else; e != nil; {
		p.nl()
		switch x := e.(type) {
		case *IfStmt:
			// else if: a flat chain of trailing forms (D3)
			p.print("(else if")
			p.ifClause(x)
			p.print(")")
			e = x.Else
		case *BlockStmt:
			p.print("(else")
			p.body(x.List)
			p.print(")")
			e = nil
		default:
			p.errorf(s, "unexpected else branch %T", e)
		}
	}
	p.indent--
	p.print(")")
}

// ifClause prints [init]? cond stmts... of an if statement.
func (p *lispPrinter) ifClause(s *IfStmt) {
	p.initStmt(s.Init)
	if s.Cond == nil {
		p.errorf(s, "missing condition in if statement")
	}
	p.print(" ")
	p.expr(s.Cond)
	p.body(s.Then.List)
}

func (p *lispPrinter) forHeader(s *ForStmt) {
	if r, ok := s.Init.(*RangeClause); ok {
		switch {
		case r.Lhs == nil:
			p.print("[(range ")
			p.expr(r.X)
			p.print(")]")
		case r.Def && lispAllNames(lispUnpackList(r.Lhs)):
			// short form: [k v (range x)] means k, v := range x (D15)
			p.print("[")
			for _, x := range lispUnpackList(r.Lhs) {
				p.print(p.decl(x.(*Name)), " ")
			}
			p.print("(range ")
			p.expr(r.X)
			p.print(")]")
		default:
			// long form; also used for := with non-names on the left,
			// which the Go parser accepts (types2 reports them)
			op, lhsExpr := "=", p.expr
			if r.Def {
				op, lhsExpr = ":=", p.defLhs
			}
			p.print("[(", op, " ")
			if lhs := lispUnpackList(r.Lhs); len(lhs) == 1 {
				lhsExpr(lhs[0])
			} else {
				p.print("[")
				for i, x := range lhs {
					if i > 0 {
						p.print(" ")
					}
					lhsExpr(x)
				}
				p.print("]")
			}
			p.print(" (range ")
			p.expr(r.X)
			p.print("))]")
		}
		return
	}

	if s.Init == nil && s.Post == nil {
		if s.Cond == nil {
			p.print("[]")
		} else {
			p.print("[")
			p.expr(s.Cond)
			p.print("]")
		}
		return
	}
	p.print("[")
	p.simpleOrOmitted(s.Init)
	p.print(" ")
	p.expr(s.Cond) // nil prints as :_
	p.print(" ")
	p.simpleOrOmitted(s.Post)
	p.print("]")
}

func (p *lispPrinter) simpleOrOmitted(s SimpleStmt) {
	if s == nil {
		p.print(":_")
		return
	}
	p.stmt(s)
}

func (p *lispPrinter) switchStmt(s *SwitchStmt) {
	if g, ok := s.Tag.(*TypeSwitchGuard); ok {
		p.print("(:type-switch")
		p.initStmt(s.Init)
		p.print(" [")
		if g.Lhs != nil {
			p.print(p.decl(g.Lhs), " ")
		}
		p.expr(g.X)
		p.print("]")
	} else {
		p.print("(switch")
		p.initStmt(s.Init)
		if s.Tag != nil {
			p.print(" ")
			p.expr(s.Tag)
		}
	}
	p.indent++
	for _, c := range s.Body {
		p.nl()
		if c.Cases == nil {
			p.print("(default")
		} else {
			p.print("(case [")
			for i, x := range lispUnpackList(c.Cases) {
				if i > 0 {
					p.print(" ")
				}
				p.expr(x)
			}
			p.print("]")
		}
		p.body(c.Body)
		p.print(")")
	}
	p.indent--
	p.print(")")
}

// GoToLisp parses the Go source src and returns it as go-lisp source,
// keeping its //go: directives as ;go: directives.
// Comments other than directives are not kept.
func GoToLisp(filename string, src io.Reader) ([]byte, error) {
	var dirs []lispDirective
	pragh := func(pos Pos, blank bool, text string, current Pragma) Pragma {
		if text != "" {
			dirs = append(dirs, lispDirective{pos, blank, text})
		}
		return current
	}
	f, err := Parse(NewFileBase(filename), src, nil, pragh, 0)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	if err := lispPrint(&b, f, dirs); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}
