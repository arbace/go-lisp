// Copyright 2026 The go-lisp Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements the go-lisp parser: it turns the forms read by
// the go-lisp reader into the same syntax tree the Go parser produces,
// following golisp/SPEC.md.

package syntax

import (
	"fmt"
	"go/build/constraint"
	"io"
	"os"
	"strings"
)

// ParseLisp parses a single go-lisp source file from src and returns the
// corresponding syntax tree. It behaves like Parse: errors are reported
// through errh (if errh is nil, parsing stops at the first error), the
// pragma handler pragh is called for each ;go: directive, and mode
// selects the same optional checks.
func ParseLisp(base *PosBase, src io.Reader, errh ErrorHandler, pragh PragmaHandler, mode Mode) (_ *File, first error) {
	defer func() {
		if p := recover(); p != nil {
			if err, ok := p.(Error); ok {
				first = err
				return
			}
			panic(p)
		}
	}()

	lf, err := lispRead(base, src, errh)
	if lf == nil {
		return nil, err
	}
	var p lispParser
	p.file = base
	p.base = base
	p.errh = errh
	p.pragh = pragh
	p.mode = mode
	// The reader's errors count as this parse's errors.
	if err != nil {
		p.first = err
		p.errcnt++
	}
	p.dirs = lf.Directives
	return p.fileOrNil(lf), p.first
}

// ParseLispFile behaves like ParseLisp but reads the source from the named file.
func ParseLispFile(filename string, errh ErrorHandler, pragh PragmaHandler, mode Mode) (*File, error) {
	f, err := os.Open(filename)
	if err != nil {
		if errh != nil {
			errh(err)
		}
		return nil, err
	}
	defer f.Close()
	return ParseLisp(NewFileBase(filename), f, errh, pragh, mode)
}

// IsLispFile reports whether filename names a go-lisp source file (D8).
func IsLispFile(filename string) bool {
	return strings.HasSuffix(filename, ".lgo")
}

type lispParser struct {
	// The embedded parser provides error reporting and pragma state
	// (errh, first, errcnt, pragh, pragma, mode) with Go's semantics.
	parser

	names *lispNamer
	dirs  []lispDirective // directives not yet passed to the pragma handler
}

func (p *lispParser) errorf(pos Pos, format string, args ...any) {
	p.syntaxErrorAt(pos, fmt.Sprintf(format, args...))
}

// syntaxErrorAt reports a syntax error at pos, like the Go parser.
func (p *lispParser) syntaxErrorAt(pos Pos, msg string) {
	p.errorAt(pos, "syntax error: "+msg)
}

func (p *lispParser) bad(pos Pos) *BadExpr {
	b := new(BadExpr)
	b.pos = pos
	return b
}

// ----------------------------------------------------------------------------
// Directives (SPEC §7)

// feedDirectives passes the directives positioned before pos
// to the pragma handler, accumulating the current pragma.
func (p *lispParser) feedDirectives(pos Pos) {
	for len(p.dirs) > 0 && (!pos.IsKnown() || p.dirs[0].Pos.Cmp(pos) < 0) {
		d := p.dirs[0]
		p.dirs = p.dirs[1:]
		if p.top && strings.HasPrefix(d.Text, "go:build") {
			if x, err := constraint.Parse("//" + d.Text); err == nil {
				p.goVersion = constraint.GoVersion(x)
			}
		}
		if p.pragh != nil {
			p.pragma = p.pragh(d.Pos, d.Blank, d.Text, p.pragma)
		}
	}
}

// clearPragmaAt reports the current pragma, if any, as unused at pos.
func (p *lispParser) clearPragmaAt(pos Pos) {
	if p.pragma != nil {
		p.pragh(pos, false, "", p.pragma)
		p.pragma = nil
	}
}

// ----------------------------------------------------------------------------
// Form helpers

// lispHead returns the symbol or keyword at the head of the list x
// (keywords with their ':'), or "" if x is not a list with such a head.
func lispHead(x *lispForm) string {
	if x.Kind != lispList || len(x.Elems) == 0 {
		return ""
	}
	switch h := x.Elems[0]; h.Kind {
	case lispSymbol:
		return h.Text
	case lispKeyword:
		return ":" + h.Text
	}
	return ""
}

func lispIsSym(x *lispForm, name string) bool {
	return x.Kind == lispSymbol && x.Text == name
}

func lispIsOmitted(x *lispForm) bool {
	return x.Kind == lispKeyword && x.Text == "_"
}

// lispEnd returns the closing position of x, or its start if x has none.
func lispEnd(x *lispForm) Pos {
	if x.End.IsKnown() {
		return x.End
	}
	return x.Pos
}

// lispOperators maps operator symbols to operators (SPEC §4).
var lispOperators = map[string]Operator{}

// lispAssignOps maps op-assign symbols (+= ...) to operators.
var lispAssignOps = map[string]Operator{}

// lispUnaryOps are the operators with a unary form.
var lispUnaryOps = map[Operator]bool{
	Not: true, Recv: true, Tilde: true, Add: true, Sub: true, Xor: true, Mul: true, And: true,
}

func init() {
	for op := Not; op <= Shr; op++ {
		lispOperators[op.String()] = op
	}
	for _, op := range []Operator{Add, Sub, Or, Xor, Mul, Div, Rem, And, AndNot, Shl, Shr} {
		lispAssignOps[op.String()+"="] = op
	}
}

// lispStmtHeads are the symbols that head statements only.
var lispStmtHeads = map[string]bool{
	"=": true, "++": true, "--": true, "if": true, "for": true, "switch": true, "select": true,
	"go": true, "defer": true, "return": true, "break": true, "continue": true, "goto": true,
	"fallthrough": true, "var": true, "const": true, "type": true, "else": true,
	"case": true, "default": true, "range": true, "import": true, "package": true,
}

// ----------------------------------------------------------------------------
// Names (SPEC §6)

// name returns the Go name for the symbol x. If decl is set, x is being
// declared; bare declarations of predeclared names need a '-' (F16).
func (p *lispParser) name(x *lispForm, member, decl bool) *Name {
	if x.Kind != lispSymbol {
		p.errorf(x.Pos, "expected name, found %s", lispDescribe(x))
		return NewName(x.Pos, "_")
	}
	if !member && lispOperators[x.Text] != 0 || lispAssignOps[x.Text] != 0 || strings.Contains(x.Text, ".") && x.Text != "." {
		p.errorf(x.Pos, "expected name, found %s", x.Text)
		return NewName(x.Pos, "_")
	}
	g, err := p.names.goName(x.Text, member)
	if err != nil {
		p.errorf(x.Pos, "%v", err)
		return NewName(x.Pos, "_")
	}
	if decl && !member && lispPredeclared[x.Text] {
		p.errorf(x.Pos, "cannot declare predeclared name %s without '-' (write -%s to shadow it, or %s to export)",
			x.Text, x.Text, lispUpperFirst(x.Text))
	}
	return NewName(x.Pos, g)
}

// lispDescribe describes the form x for error messages.
func lispDescribe(x *lispForm) string {
	switch x.Kind {
	case lispSymbol:
		return "symbol " + x.Text
	case lispKeyword:
		return "keyword :" + x.Text
	case lispLit:
		return "literal " + x.Text
	case lispList:
		if h := lispHead(x); h != "" {
			return "(" + h + " ...)"
		}
		if len(x.Elems) == 0 {
			return "()"
		}
	}
	return x.Kind.String()
}

// dotted returns the selector chain for the dotted symbol x (a.b.c):
// the first segment is a bare name, the others are member names.
func (p *lispParser) dotted(x *lispForm) Expr {
	segs := strings.Split(x.Text, ".")
	col := x.Pos.Col()
	pos := func(off int) Pos { return MakePos(x.Pos.Base(), x.Pos.Line(), col+uint(off)) }
	off := 0
	var e Expr
	for i, s := range segs {
		if s == "" {
			p.errorf(x.Pos, "invalid dotted name %s", x.Text)
			return p.bad(x.Pos)
		}
		seg := &lispForm{Kind: lispSymbol, Pos: pos(off), Text: s}
		if i == 0 {
			e = p.name(seg, false, false)
		} else {
			sel := new(SelectorExpr)
			sel.pos = pos(off - 1) // position of the '.'
			sel.X = e
			sel.Sel = p.name(seg, true, false)
			e = sel
		}
		off += len(s) + 1
	}
	return e
}

func lispIsDotted(x *lispForm) bool {
	return x.Kind == lispSymbol && strings.Contains(x.Text, ".") && x.Text != "." && x.Text != "..."
}

// ----------------------------------------------------------------------------
// Files and declarations (SPEC §2)

func (p *lispParser) fileOrNil(lf *lispFile) *File {
	f := new(File)
	p.top = true
	forms := lf.Forms
	if len(forms) == 0 || lispHead(forms[0]) != "package" {
		pos := lf.EOF
		if len(forms) > 0 {
			pos = forms[0].Pos
		}
		p.errorf(pos, "package statement must be first")
		return nil
	}

	// imports need to be known before any name is mapped
	var imports []string
	for _, x := range forms[1:] {
		if lispHead(x) != "import" {
			continue
		}
		for _, s := range x.Elems[1:] {
			switch {
			case s.Kind == lispLit && s.Lit == StringLit:
				imports = append(imports, lispImportName("", lispUnquote(s.Text)))
			case s.Kind == lispVector && len(s.Elems) == 2 && s.Elems[0].Kind == lispSymbol:
				imports = append(imports, lispImportName(s.Elems[0].Text, lispUnquote(s.Elems[1].Text)))
			}
		}
	}
	p.names = newLispNamer(imports)

	pkg := forms[0]
	p.feedDirectives(pkg.Pos)
	f.pos = pkg.Elems[0].Pos
	f.GoVersion = p.goVersion
	p.top = false
	if len(pkg.Elems) != 2 || pkg.Elems[1].Kind != lispSymbol {
		p.errorf(pkg.Pos, "package clause must be (package name)")
		return nil
	}
	f.PkgName = NewName(pkg.Elems[1].Pos, pkg.Elems[1].Text) // never mapped
	if !lispIsIdent(f.PkgName.Value) {
		p.errorf(pkg.Elems[1].Pos, "invalid package name %s", f.PkgName.Value)
	}
	f.Pragma = p.takePragma()

	prev := "import"
	for _, x := range forms[1:] {
		p.feedDirectives(x.Pos)
		head := lispHead(x)
		if head == "import" && prev != "import" {
			p.errorf(x.Pos, "imports must appear before other declarations")
		}
		prev = head
		switch head {
		case "import", "const", "type", "var":
			f.DeclList = p.declForm(f.DeclList, x)
		case "func":
			if d := p.funcDecl(x); d != nil {
				f.DeclList = append(f.DeclList, d)
			}
		default:
			p.errorf(x.Pos, "non-declaration statement outside function body: %s", lispDescribe(x))
		}
		p.clearPragmaAt(lispEnd(x))
	}
	p.feedDirectives(Pos{}) // remaining directives
	p.clearPragmaAt(lf.EOF)
	f.EOF = lf.EOF
	return f
}

// lispUnquote returns the value of a string literal (for import paths).
func lispUnquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '`') {
		// import paths are simple; strconv semantics are not needed here
		return s[1 : len(s)-1]
	}
	return s
}

// declForm parses an import, const, type, or var form and appends
// its declarations to list. A form whose arguments are all lists is
// a group (D4); an import form with several specs is a group.
func (p *lispParser) declForm(list []Decl, x *lispForm) []Decl {
	kw := x.Elems[0].Text
	args := x.Elems[1:]

	grouped := len(args) == 0
	if kw == "import" {
		grouped = len(args) != 1
	} else if len(args) > 0 {
		grouped = true
		for _, a := range args {
			if a.Kind != lispList {
				grouped = false
				break
			}
		}
	}
	if !grouped {
		if kw == "import" {
			return append(list, p.importSpec(nil, args[0]))
		}
		return append(list, p.spec(kw, nil, x.Pos, args))
	}

	g := new(Group)
	p.clearPragmaAt(x.Pos) // directives before a group are misplaced (F11)
	for _, s := range args {
		p.feedDirectives(s.Pos)
		if kw == "import" {
			list = append(list, p.importSpec(g, s))
		} else {
			list = append(list, p.spec(kw, g, s.Pos, s.Elems))
		}
	}
	return list
}

func (p *lispParser) importSpec(g *Group, x *lispForm) Decl {
	d := new(ImportDecl)
	d.pos = x.Pos
	d.Group = g
	d.Pragma = p.takePragma()
	path := x
	if x.Kind == lispVector {
		if len(x.Elems) != 2 {
			p.errorf(x.Pos, "import spec must be \"path\" or [name \"path\"]")
			return d
		}
		n := x.Elems[0]
		if n.Kind != lispSymbol || !(n.Text == "." || lispIsIdent(n.Text)) {
			p.errorf(n.Pos, "invalid import name %s", lispDescribe(n))
		}
		d.LocalPkgName = NewName(n.Pos, n.Text) // verbatim (A12)
		path = x.Elems[1]
	}
	if path.Kind != lispLit || path.Lit != StringLit {
		p.errorf(path.Pos, "import path must be a string")
		d.Path = &BasicLit{Value: `"_"`, Kind: StringLit, Bad: true}
		d.Path.pos = path.Pos
		return d
	}
	d.Path = p.basicLit(path)
	return d
}

// spec parses the elements of a const, var, or type spec.
func (p *lispParser) spec(kw string, g *Group, pos Pos, elems []*lispForm) Decl {
	if len(elems) == 0 {
		p.errorf(pos, "missing name in %s declaration", kw)
		return nil
	}
	if kw == "type" {
		return p.typeSpec(g, pos, elems)
	}

	names := p.declNames(elems[0])
	rest := elems[1:]
	var typ, values Expr
	eq := -1
	for i, e := range rest {
		if lispIsSym(e, "=") {
			eq = i
			break
		}
	}
	typeForms := rest
	if eq >= 0 {
		typeForms = rest[:eq]
		vals := rest[eq+1:]
		if len(vals) == 0 {
			p.errorf(rest[eq].Pos, "missing values after = in %s declaration", kw)
		} else {
			values = p.exprList(vals)
		}
	}
	switch len(typeForms) {
	case 0:
	case 1:
		typ = p.expr(typeForms[0])
	default:
		p.errorf(typeForms[1].Pos, "unexpected %s in %s declaration (missing =?)", lispDescribe(typeForms[1]), kw)
	}

	if kw == "const" {
		d := new(ConstDecl)
		d.pos = elems[0].Pos
		d.Group = g
		d.Pragma = p.takePragma()
		d.NameList, d.Type, d.Values = names, typ, values
		return d
	}
	if typ == nil && values == nil {
		p.errorf(elems[0].Pos, "missing type or = values in var declaration")
	}
	d := new(VarDecl)
	d.pos = elems[0].Pos
	d.Group = g
	d.Pragma = p.takePragma()
	d.NameList, d.Type, d.Values = names, typ, values
	return d
}

// declNames parses a declared name or a vector of declared names.
func (p *lispParser) declNames(x *lispForm) []*Name {
	if x.Kind == lispVector {
		if len(x.Elems) == 0 {
			p.errorf(x.Pos, "missing names")
		}
		names := make([]*Name, len(x.Elems))
		for i, e := range x.Elems {
			names[i] = p.name(e, false, true)
		}
		return names
	}
	return []*Name{p.name(x, false, true)}
}

func (p *lispParser) typeSpec(g *Group, pos Pos, elems []*lispForm) Decl {
	d := new(TypeDecl)
	d.pos = elems[0].Pos
	d.Group = g
	d.Pragma = p.takePragma()
	d.Name = p.name(elems[0], false, true)
	rest := elems[1:]
	if len(rest) > 0 && rest[0].Kind == lispVector {
		d.TParamList = p.fieldList(rest[0], fieldsTypeParams)
		rest = rest[1:]
	}
	if len(rest) > 0 && lispIsSym(rest[0], "=") {
		d.Alias = true
		rest = rest[1:]
	}
	if len(rest) != 1 {
		p.errorf(pos, "type declaration must be (type name [tparams]? =? type)")
		d.Type = p.bad(pos)
		return d
	}
	d.Type = p.expr(rest[0])
	return d
}

func (p *lispParser) funcDecl(x *lispForm) Decl {
	d := new(FuncDecl)
	d.pos = x.Elems[0].Pos
	d.Pragma = p.takePragma()
	rest := x.Elems[1:]
	if len(rest) > 0 && rest[0].Kind == lispVector {
		recv := p.fieldList(rest[0], fieldsParams)
		if len(recv) != 1 {
			p.errorf(rest[0].Pos, "method has %d receivers; want exactly one", len(recv))
		}
		if len(recv) > 0 {
			d.Recv = recv[0]
		}
		rest = rest[1:]
		if len(rest) == 0 {
			p.errorf(x.Pos, "missing method name")
			return nil
		}
		d.Name = p.name(rest[0], true, false) // method names are member names
	} else {
		if len(rest) == 0 {
			p.errorf(x.Pos, "missing function name")
			return nil
		}
		d.Name = p.name(rest[0], false, true)
	}
	rest = rest[1:]

	nvec := 0
	for nvec < len(rest) && rest[nvec].Kind == lispVector {
		nvec++
	}
	switch nvec {
	case 2:
	case 3:
		d.TParamList = p.fieldList(rest[0], fieldsTypeParams)
		rest = rest[1:]
	default:
		p.errorf(x.Pos, "function declaration needs [params] [results] (found %d vectors)", nvec)
		return nil
	}
	d.Type = p.funcType(x.Elems[0].Pos, rest[0], rest[1])
	if body := rest[2:]; len(body) > 0 {
		d.Body = p.funcBody(body, x)
	}
	return d
}

func (p *lispParser) funcType(pos Pos, params, results *lispForm) *FuncType {
	t := new(FuncType)
	t.pos = pos
	t.ParamList = p.fieldList(params, fieldsParams)
	t.ResultList = p.fieldList(results, fieldsResults)
	return t
}

// funcBody parses the statements of a function body; a body
// consisting of () only is empty (SPEC A6, A10).
func (p *lispParser) funcBody(forms []*lispForm, fn *lispForm) *BlockStmt {
	errcnt := p.errcnt
	body := p.block(forms, forms[0].Pos, lispEnd(fn))
	if p.mode&CheckBranches != 0 && errcnt == p.errcnt {
		checkBranches(body, p.errh)
	}
	return body
}

// ----------------------------------------------------------------------------
// Parameter and field lists (SPEC §3)

type lispFieldsKind int

const (
	fieldsParams lispFieldsKind = iota
	fieldsResults
	fieldsTypeParams
)

// fieldList parses a parameter, result, receiver, or type parameter
// vector: flat name/type pairs, or types only if the vector starts
// with :_ or has odd length (D11).
func (p *lispParser) fieldList(x *lispForm, kind lispFieldsKind) []*Field {
	if x.Kind != lispVector {
		p.errorf(x.Pos, "expected parameter vector, found %s", lispDescribe(x))
		return nil
	}
	elems := x.Elems
	unnamed := len(elems)%2 == 1
	if len(elems) > 0 && lispIsOmitted(elems[0]) {
		unnamed = true
		elems = elems[1:]
	}
	if kind == fieldsTypeParams {
		if len(elems) == 0 {
			p.errorf(x.Pos, "empty type parameter list")
			return nil
		}
		if unnamed {
			p.errorf(x.Pos, "type parameters must be name/constraint pairs")
			return nil
		}
	}

	var list []*Field
	add := func(name *Name, t *lispForm) {
		f := new(Field)
		f.pos = t.Pos
		if name != nil {
			f.pos = name.pos
		}
		f.Name = name
		if lispHead(t) == "..." {
			f.Type = p.dotsType(t)
		} else {
			f.Type = p.expr(t)
		}
		list = append(list, f)
	}
	if unnamed {
		for _, t := range elems {
			add(nil, t)
		}
	} else {
		for i := 0; i+1 < len(elems); i += 2 {
			add(p.name(elems[i], false, true), elems[i+1])
		}
	}

	for i, f := range list {
		if _, ok := f.Type.(*DotsType); ok && (kind != fieldsParams || i != len(list)-1) {
			p.errorf(f.Type.Pos(), "can only use ... with final parameter in list")
		}
	}
	return list
}

func (p *lispParser) dotsType(x *lispForm) Expr {
	if len(x.Elems) != 2 {
		p.errorf(x.Pos, "(... T) needs exactly one type")
		return p.bad(x.Pos)
	}
	t := new(DotsType)
	t.pos = x.Pos
	t.Elem = p.expr(x.Elems[1])
	return t
}

func (p *lispParser) structType(x *lispForm) Expr {
	t := new(StructType)
	t.pos = x.Pos
	for _, g := range x.Elems[1:] {
		if g.Kind != lispVector || len(g.Elems) == 0 {
			p.errorf(g.Pos, "struct field group must be a vector [names... Type tag?]")
			continue
		}
		elems := g.Elems
		var tag *BasicLit
		if last := elems[len(elems)-1]; last.Kind == lispLit && len(elems) > 1 {
			tag = p.basicLit(last) // any literal is a tag (F4)
			elems = elems[:len(elems)-1]
		}
		typ := elems[len(elems)-1]
		if len(elems) == 1 {
			// embedded field
			p.addField(t, typ.Pos, nil, p.expr(typ), tag)
			continue
		}
		ftyp := p.expr(typ)
		for _, n := range elems[:len(elems)-1] {
			p.addField(t, n.Pos, p.name(n, true, false), ftyp, tag)
		}
	}
	return t
}

func (p *lispParser) interfaceType(x *lispForm) Expr {
	t := new(InterfaceType)
	t.pos = x.Pos
	for _, e := range x.Elems[1:] {
		f := new(Field)
		f.pos = e.Pos
		h := lispHead(e)
		if len(e.Elems) == 3 && e.Elems[0].Kind == lispSymbol && !lispIsSpecialHead(h) &&
			e.Elems[1].Kind == lispVector && e.Elems[2].Kind == lispVector {
			// method (A7/F1): a keyword-named method is written capitalized
			f.Name = p.name(e.Elems[0], true, false)
			if lispGoKeywords[h] {
				p.errorf(e.Elems[0].Pos, "method name %s is a keyword; write %s", h, lispUpperFirst(h))
			}
			f.Type = p.funcType(e.Pos, e.Elems[1], e.Elems[2])
		} else {
			f.Type = p.expr(e) // embedded type or type set term
		}
		t.MethodList = append(t.MethodList, f)
	}
	return t
}

// lispIsSpecialHead reports whether the list head h is always a
// special form (SPEC I3).
func lispIsSpecialHead(h string) bool {
	return lispGoKeywords[h] || lispOperators[h] != 0 || lispAssignOps[h] != 0 || lispStmtHeads[h] ||
		h == "<-chan" || h == "chan<-" || h == "..." || strings.HasPrefix(h, ":")
}

// ----------------------------------------------------------------------------
// Expressions (SPEC §4)

func (p *lispParser) exprList(list []*lispForm) Expr {
	if len(list) == 1 {
		return p.expr(list[0])
	}
	l := new(ListExpr)
	l.pos = list[0].Pos
	for _, x := range list {
		l.ElemList = append(l.ElemList, p.expr(x))
	}
	return l
}

func (p *lispParser) basicLit(x *lispForm) *BasicLit {
	b := new(BasicLit)
	b.pos = x.Pos
	b.Value = x.Text
	b.Kind = x.Lit
	b.Bad = x.Bad
	return b
}

func (p *lispParser) expr(x *lispForm) Expr {
	switch x.Kind {
	case lispLit:
		return p.basicLit(x)
	case lispSymbol:
		switch {
		case lispIsDotted(x):
			return p.dotted(x)
		case x.Text == "..." || lispOperators[x.Text] != 0 || lispAssignOps[x.Text] != 0 || x.Text == "=":
			p.errorf(x.Pos, "unexpected %s in expression", x.Text)
			return p.bad(x.Pos)
		}
		return p.name(x, false, false)
	case lispList:
		if len(x.Elems) == 0 {
			p.errorf(x.Pos, "unexpected () in expression")
			return p.bad(x.Pos)
		}
		return p.listExpr(x)
	}
	p.errorf(x.Pos, "unexpected %s in expression", lispDescribe(x))
	return p.bad(x.Pos)
}

func (p *lispParser) listExpr(x *lispForm) Expr {
	head := lispHead(x)
	args := x.Elems[1:]
	nargs := func(n int) bool {
		if len(args) != n {
			p.errorf(x.Pos, "(%s ...) takes %d argument(s), found %d", head, n, len(args))
			return false
		}
		return true
	}

	switch head {
	case "func":
		return p.funcTypeOrLit(x)

	case "map":
		if !nargs(2) {
			return p.bad(x.Pos)
		}
		t := new(MapType)
		t.pos = x.Pos
		t.Key = p.expr(args[0])
		t.Value = p.expr(args[1])
		return t

	case "chan", "<-chan", "chan<-":
		if !nargs(1) {
			return p.bad(x.Pos)
		}
		t := new(ChanType)
		t.pos = x.Pos
		t.Dir = map[string]ChanDir{"chan": 0, "<-chan": RecvOnly, "chan<-": SendOnly}[head]
		t.Elem = p.expr(args[0])
		return t

	case "struct":
		return p.structType(x)

	case "interface":
		return p.interfaceType(x)

	case "...":
		p.errorf(x.Pos, "(... T) is only allowed as the type of a final parameter")
		return p.bad(x.Pos)

	case ":index":
		if len(args) < 2 {
			p.errorf(x.Pos, "(:index x i...) needs an operand and at least one index")
			return p.bad(x.Pos)
		}
		e := new(IndexExpr)
		e.pos = x.Pos
		e.X = p.expr(args[0])
		e.Index = p.exprList(args[1:])
		return e

	case ":slice":
		if len(args) != 3 && len(args) != 4 {
			p.errorf(x.Pos, "(:slice x lo hi max?) needs 2 or 3 indices")
			return p.bad(x.Pos)
		}
		e := new(SliceExpr)
		e.pos = x.Pos
		e.X = p.expr(args[0])
		for i, a := range args[1:] {
			if !lispIsOmitted(a) {
				e.Index[i] = p.expr(a)
			}
		}
		if len(args) == 4 {
			e.Full = true
			if e.Index[1] == nil {
				p.errorf(x.Pos, "middle index required in 3-index slice")
			}
			if e.Index[2] == nil {
				p.errorf(x.Pos, "final index required in 3-index slice")
			}
		}
		return e

	case ":assert":
		if !nargs(2) {
			return p.bad(x.Pos)
		}
		if lispIsSym(args[1], "type") {
			// x.(type) (F2)
			g := new(TypeSwitchGuard)
			g.pos = x.Pos
			g.X = p.expr(args[0])
			return g
		}
		e := new(AssertExpr)
		e.pos = x.Pos
		e.X = p.expr(args[0])
		e.Type = p.expr(args[1])
		return e

	case ":sel":
		if !nargs(2) {
			return p.bad(x.Pos)
		}
		e := p.expr(args[0])
		sel := args[1]
		if sel.Kind != lispSymbol {
			p.errorf(sel.Pos, "(:sel x name) needs a name, found %s", lispDescribe(sel))
			return p.bad(sel.Pos)
		}
		off := 0
		for _, s := range strings.Split(sel.Text, ".") {
			seg := &lispForm{Kind: lispSymbol, Pos: MakePos(sel.Pos.Base(), sel.Pos.Line(), sel.Pos.Col()+uint(off)), Text: s}
			if s == "" {
				p.errorf(sel.Pos, "invalid selector %s", sel.Text)
				return p.bad(sel.Pos)
			}
			se := new(SelectorExpr)
			se.pos = seg.Pos
			se.X = e
			se.Sel = p.name(seg, true, false)
			e = se
			off += len(s) + 1
		}
		return e

	case ":lit":
		return p.compositeLit(x)

	case ":slice-of":
		if !nargs(1) {
			return p.bad(x.Pos)
		}
		t := new(SliceType)
		t.pos = x.Pos
		t.Elem = p.expr(args[0])
		return t

	case ":array-of":
		if !nargs(2) {
			return p.bad(x.Pos)
		}
		t := new(ArrayType)
		t.pos = x.Pos
		if !lispIsSym(args[0], "...") {
			t.Len = p.expr(args[0])
		}
		t.Elem = p.expr(args[1])
		return t

	case ":kv":
		p.errorf(x.Pos, "(:kv key value) is only allowed in (:lit ...)")
		return p.bad(x.Pos)
	}

	if op, ok := lispOperators[head]; ok {
		return p.operation(x, op)
	}
	if lispStmtHeads[head] || lispAssignOps[head] != 0 || strings.HasPrefix(head, ":") {
		p.errorf(x.Pos, "unexpected %s in expression", lispDescribe(x))
		return p.bad(x.Pos)
	}
	return p.call(x)
}

// operation parses unary and (n-ary, left-folding) binary operations (A9).
func (p *lispParser) operation(x *lispForm, op Operator) Expr {
	args := x.Elems[1:]
	pos := x.Elems[0].Pos
	switch {
	case len(args) == 0:
		p.errorf(x.Pos, "operator %s needs operands", op)
		return p.bad(x.Pos)
	case len(args) == 1:
		if !lispUnaryOps[op] {
			p.errorf(x.Pos, "operator %s is not unary", op)
			return p.bad(x.Pos)
		}
		e := new(Operation)
		e.pos = pos
		e.Op = op
		e.X = p.expr(args[0])
		return e
	case op == Not || op == Recv || op == Tilde:
		if op == Recv {
			p.errorf(x.Pos, "send (<- ch v) is a statement, not an expression")
		} else {
			p.errorf(x.Pos, "operator %s is unary", op)
		}
		return p.bad(x.Pos)
	case len(args) > 2 && !lispNaryOps[op]:
		p.errorf(x.Pos, "comparison %s takes exactly 2 operands", op)
		return p.bad(x.Pos)
	}
	var e Expr = p.expr(args[0])
	for _, a := range args[1:] {
		o := new(Operation)
		o.pos = pos
		o.Op = op
		o.X = e
		o.Y = p.expr(a)
		e = o
	}
	return e
}

func (p *lispParser) call(x *lispForm) Expr {
	c := new(CallExpr)
	c.pos = x.Pos
	c.Fun = p.expr(x.Elems[0])
	args := x.Elems[1:]
	if n := len(args); n > 0 && lispIsSym(args[n-1], "...") {
		c.HasDots = true
		args = args[:n-1]
		if len(args) == 0 {
			p.errorf(x.Pos, "... needs an argument to spread")
		}
	}
	for _, a := range args {
		c.ArgList = append(c.ArgList, p.expr(a))
	}
	return c
}

// funcTypeOrLit parses (func [params] [results] body...): a function
// type if nothing follows the results, a function literal otherwise (A6).
func (p *lispParser) funcTypeOrLit(x *lispForm) Expr {
	args := x.Elems[1:]
	if len(args) < 2 || args[0].Kind != lispVector || args[1].Kind != lispVector {
		p.errorf(x.Pos, "function type or literal needs [params] [results]")
		return p.bad(x.Pos)
	}
	t := p.funcType(x.Pos, args[0], args[1])
	if len(args) == 2 {
		return t
	}
	f := new(FuncLit)
	f.pos = x.Pos
	f.Type = t
	f.Body = p.funcBody(args[2:], x)
	return f
}

func (p *lispParser) compositeLit(x *lispForm) Expr {
	args := x.Elems[1:]
	if len(args) == 0 {
		p.errorf(x.Pos, "(:lit T elems...) needs a type or :_")
		return p.bad(x.Pos)
	}
	c := new(CompositeLit)
	c.pos = x.Pos
	if !lispIsOmitted(args[0]) {
		c.Type = p.expr(args[0])
	}
	for _, e := range args[1:] {
		if lispHead(e) != ":kv" {
			c.ElemList = append(c.ElemList, p.expr(e))
			continue
		}
		if len(e.Elems) != 3 {
			p.errorf(e.Pos, "(:kv key value) takes 2 arguments")
			continue
		}
		kv := new(KeyValueExpr)
		kv.pos = e.Pos
		if k := e.Elems[1]; k.Kind == lispKeyword {
			// a keyword key is a field name (member rules, F26)
			if k.Text == "_" || strings.Contains(k.Text, ".") {
				p.errorf(k.Pos, "invalid field key :%s", k.Text)
			}
			kv.Key = p.name(&lispForm{Kind: lispSymbol, Pos: k.Pos, Text: k.Text}, true, false)
		} else {
			kv.Key = p.expr(k)
		}
		kv.Value = p.expr(e.Elems[2])
		c.ElemList = append(c.ElemList, kv)
		c.NKeys++
	}
	c.Rbrace = lispEnd(x)
	return c
}

// ----------------------------------------------------------------------------
// Statements (SPEC §5)

// block parses a statement list into a block; () statements
// contribute nothing (A10).
func (p *lispParser) block(forms []*lispForm, pos, rbrace Pos) *BlockStmt {
	b := new(BlockStmt)
	b.pos = pos
	b.List = p.stmtList(forms)
	b.Rbrace = rbrace
	return b
}

func (p *lispParser) stmtList(forms []*lispForm) []Stmt {
	var list []Stmt
	for _, x := range forms {
		p.feedDirectives(x.Pos)
		if x.Kind == lispList && len(x.Elems) == 0 {
			continue // () is the empty statement
		}
		if s := p.stmt(x); s != nil {
			list = append(list, s)
		}
		p.clearPragmaAt(lispEnd(x))
	}
	return list
}

func (p *lispParser) stmt(x *lispForm) Stmt {
	if x.Kind == lispList && len(x.Elems) == 0 {
		s := new(EmptyStmt)
		s.pos = x.Pos
		return s
	}
	head := lispHead(x)
	var args []*lispForm
	if x.Kind == lispList && len(x.Elems) > 0 {
		args = x.Elems[1:]
	}

	switch head {
	case "=", ":=":
		return p.assign(x, head)
	case "++", "--":
		if len(args) != 1 {
			p.errorf(x.Pos, "(%s x) takes one argument", head)
			return nil
		}
		s := new(AssignStmt)
		s.pos = x.Pos
		s.Op = Add
		if head == "--" {
			s.Op = Sub
		}
		s.Lhs = p.expr(args[0])
		return s
	case "<-":
		if len(args) == 2 {
			s := new(SendStmt)
			s.pos = x.Pos
			s.Chan = p.expr(args[0])
			s.Value = p.expr(args[1])
			return s
		}
	case "var", "const", "type":
		s := new(DeclStmt)
		s.pos = x.Pos
		s.DeclList = p.declForm(nil, x)
		return s
	case "if":
		return p.ifStmt(x)
	case "for":
		return p.forStmt(x)
	case "switch":
		return p.switchStmt(x, false)
	case ":type-switch":
		return p.switchStmt(x, true)
	case "select":
		return p.selectStmt(x)
	case "go", "defer":
		if len(args) != 1 {
			p.errorf(x.Pos, "(%s call) takes one argument", head)
			return nil
		}
		s := new(CallStmt)
		s.pos = x.Pos
		s.Tok = map[string]token{"go": _Go, "defer": _Defer}[head]
		s.Call = p.expr(args[0])
		return s
	case "return":
		s := new(ReturnStmt)
		s.pos = x.Pos
		if len(args) > 0 {
			s.Results = p.exprList(args)
		}
		return s
	case "break", "continue", "goto", "fallthrough":
		s := new(BranchStmt)
		s.pos = x.Pos
		s.Tok = map[string]token{"break": _Break, "continue": _Continue, "goto": _Goto, "fallthrough": _Fallthrough}[head]
		switch {
		case len(args) == 1 && head != "fallthrough":
			s.Label = p.name(args[0], false, false)
		case len(args) > 1 || len(args) == 1 && head == "fallthrough" || len(args) == 0 && head == "goto":
			p.errorf(x.Pos, "invalid (%s ...) statement", head)
		}
		return s
	case ":label":
		if len(args) != 2 {
			p.errorf(x.Pos, "(:label name stmt) takes 2 arguments")
			return nil
		}
		s := new(LabeledStmt)
		s.pos = x.Pos
		s.Label = p.name(args[0], false, true)
		s.Stmt = p.stmt(args[1])
		return s
	case ":block":
		return p.block(args, x.Pos, lispEnd(x))
	case "else":
		p.errorf(x.Pos, "else must be a trailing form of an if statement")
		return nil
	case "case", "default":
		p.errorf(x.Pos, "%s outside switch or select", head)
		return nil
	case "range":
		p.errorf(x.Pos, "range is only allowed in a for header")
		return nil
	case "import", "package", "func":
		if head != "func" {
			p.errorf(x.Pos, "%s is only allowed at top level", head)
			return nil
		}
	}
	if op, ok := lispAssignOps[head]; ok {
		if len(args) != 2 {
			p.errorf(x.Pos, "(%s x y) takes 2 arguments", head)
			return nil
		}
		s := new(AssignStmt)
		s.pos = x.Pos
		s.Op = op
		s.Lhs = p.expr(args[0])
		s.Rhs = p.expr(args[1])
		return s
	}

	s := new(ExprStmt)
	s.pos = x.Pos
	s.X = p.expr(x)
	return s
}

// assign parses (= lhs rhs...) and (:= lhs rhs...) (A11).
func (p *lispParser) assign(x *lispForm, head string) Stmt {
	args := x.Elems[1:]
	if len(args) < 2 {
		p.errorf(x.Pos, "(%s lhs rhs...) needs a left and a right side", head)
		return nil
	}
	s := new(AssignStmt)
	s.pos = x.Pos
	def := head == ":="
	if def {
		s.Op = Def
	}
	lhs := func(e *lispForm) Expr {
		// Names on the left of := are declared. Other expressions are
		// accepted, as by the Go parser; types2 reports them.
		if def && e.Kind == lispSymbol && !lispIsDotted(e) {
			return p.name(e, false, true)
		}
		return p.expr(e)
	}
	if l := args[0]; l.Kind == lispVector {
		if len(l.Elems) == 0 {
			p.errorf(l.Pos, "empty left side")
			return nil
		}
		if len(l.Elems) == 1 {
			s.Lhs = lhs(l.Elems[0])
		} else {
			list := new(ListExpr)
			list.pos = l.Pos
			for _, e := range l.Elems {
				list.ElemList = append(list.ElemList, lhs(e))
			}
			s.Lhs = list
		}
	} else {
		s.Lhs = lhs(l)
	}
	s.Rhs = p.exprList(args[1:])
	return s
}

// simpleStmt parses the single simple statement in an init vector.
func (p *lispParser) simpleStmt(x *lispForm) SimpleStmt {
	s := p.stmt(x)
	if s == nil {
		return nil
	}
	ss, ok := s.(SimpleStmt)
	if !ok {
		p.errorf(x.Pos, "%s is not a simple statement", lispDescribe(x))
		return nil
	}
	return ss
}

// initVector parses an init vector [stmt] if x is a vector.
func (p *lispParser) initVector(x *lispForm) SimpleStmt {
	if len(x.Elems) != 1 {
		p.errorf(x.Pos, "init vector must contain exactly one statement")
		return nil
	}
	return p.simpleStmt(x.Elems[0])
}

// bodyBlock parses the statements forms of a compound statement
// as a block ending at rbrace.
func (p *lispParser) bodyBlock(forms []*lispForm, pos, rbrace Pos) *BlockStmt {
	if len(forms) > 0 {
		pos = forms[0].Pos
	}
	return p.block(forms, pos, rbrace)
}

func (p *lispParser) ifStmt(x *lispForm) Stmt {
	return p.ifClause(x.Elems[0].Pos, x.Elems[1:], lispEnd(x))
}

// ifClause parses [init]? cond stmts... else-forms* of an if (D3).
func (p *lispParser) ifClause(pos Pos, args []*lispForm, end Pos) *IfStmt {
	s := new(IfStmt)
	s.pos = pos
	if len(args) > 0 && args[0].Kind == lispVector {
		s.Init = p.initVector(args[0])
		args = args[1:]
	}
	if len(args) == 0 {
		p.errorf(pos, "missing condition in if statement")
		s.Cond = p.bad(pos)
		s.Then = p.block(nil, pos, end)
		return s
	}
	s.Cond = p.expr(args[0])
	args = args[1:]

	// split off the trailing else forms
	n := len(args)
	for n > 0 && lispHead(args[n-1]) == "else" {
		n--
	}
	body, elses := args[:n], args[n:]
	for _, b := range body {
		if lispHead(b) == "else" {
			p.errorf(b.Pos, "else must be a trailing form of an if statement")
		}
	}
	thenEnd := end
	if len(elses) > 0 {
		thenEnd = elses[0].Pos
	}
	s.Then = p.bodyBlock(body, s.Cond.Pos(), thenEnd)

	// build the else chain
	cur := s
	for i, e := range elses {
		eargs := e.Elems[1:]
		if len(eargs) > 0 && lispIsSym(eargs[0], "if") {
			next := p.ifClause(eargs[0].Pos, eargs[1:], lispEnd(e))
			cur.Else = next
			cur = next
			continue
		}
		if i != len(elses)-1 {
			p.errorf(e.Pos, "(else ...) must be the last else form")
		}
		cur.Else = p.bodyBlock(eargs, e.Pos, lispEnd(e))
	}
	return s
}

func (p *lispParser) forStmt(x *lispForm) Stmt {
	s := new(ForStmt)
	s.pos = x.Elems[0].Pos
	args := x.Elems[1:]
	if len(args) == 0 || args[0].Kind != lispVector {
		p.errorf(x.Pos, "for statement needs a header vector")
		s.Body = p.block(nil, x.Pos, lispEnd(x))
		return s
	}
	h := args[0]
	hs := h.Elems
	n := len(hs)
	isRange := func(e *lispForm) bool { return lispHead(e) == "range" }

	switch {
	case n == 0:
	case isRange(hs[n-1]) && n <= 3:
		// (range x), [k (range x)], [k v (range x)] (D15)
		r := new(RangeClause)
		r.pos = hs[n-1].Pos
		r.X = p.rangeExpr(hs[n-1])
		if n > 1 {
			r.Def = true
			names := hs[:n-1]
			if len(names) == 1 {
				r.Lhs = p.name(names[0], false, true)
			} else {
				l := new(ListExpr)
				l.pos = names[0].Pos
				for _, e := range names {
					l.ElemList = append(l.ElemList, p.name(e, false, true))
				}
				r.Lhs = l
			}
		}
		s.Init = r
	case n == 1 && (lispHead(hs[0]) == ":=" || lispHead(hs[0]) == "=") && len(hs[0].Elems) == 3 && isRange(hs[0].Elems[2]):
		// [(:= lhs (range x))], [(= lhs (range x))]
		a := hs[0]
		r := new(RangeClause)
		r.pos = a.Pos
		r.Def = lispHead(a) == ":="
		var lhs []Expr
		elems := []*lispForm{a.Elems[1]}
		if a.Elems[1].Kind == lispVector {
			elems = a.Elems[1].Elems
		}
		for _, e := range elems {
			if r.Def && e.Kind == lispSymbol && !lispIsDotted(e) {
				lhs = append(lhs, p.name(e, false, true))
			} else {
				lhs = append(lhs, p.expr(e)) // non-names: types2 reports them
			}
		}
		if len(lhs) == 1 {
			r.Lhs = lhs[0]
		} else if len(lhs) > 1 {
			l := new(ListExpr)
			l.pos = a.Elems[1].Pos
			l.ElemList = lhs
			r.Lhs = l
		}
		r.X = p.rangeExpr(a.Elems[2])
		s.Init = r
	case n == 1:
		if !lispIsOmitted(hs[0]) {
			s.Cond = p.expr(hs[0])
		}
	case n == 3:
		if !lispIsOmitted(hs[0]) {
			s.Init = p.simpleStmt(hs[0])
		}
		if !lispIsOmitted(hs[1]) {
			s.Cond = p.expr(hs[1])
		}
		if !lispIsOmitted(hs[2]) {
			s.Post = p.simpleStmt(hs[2])
			if a, ok := s.Post.(*AssignStmt); ok && a.Op == Def {
				p.errorf(hs[2].Pos, "cannot declare in post statement of for loop")
			}
		}
	default:
		p.errorf(h.Pos, "invalid for header (F25): want [], [cond], [init cond post], or a range clause")
	}
	s.Body = p.bodyBlock(args[1:], lispEnd(h), lispEnd(x))
	return s
}

func (p *lispParser) rangeExpr(x *lispForm) Expr {
	if len(x.Elems) != 2 {
		p.errorf(x.Pos, "(range x) takes one argument")
		return p.bad(x.Pos)
	}
	return p.expr(x.Elems[1])
}

func (p *lispParser) switchStmt(x *lispForm, typeSwitch bool) Stmt {
	s := new(SwitchStmt)
	s.pos = x.Elems[0].Pos
	args := x.Elems[1:]
	isClause := func(e *lispForm) bool { h := lispHead(e); return h == "case" || h == "default" }

	if typeSwitch {
		// (:type-switch [init]? [v x] clauses...)
		if len(args) > 1 && args[0].Kind == lispVector && args[1].Kind == lispVector {
			s.Init = p.initVector(args[0])
			args = args[1:]
		}
		if len(args) == 0 || args[0].Kind != lispVector || len(args[0].Elems) < 1 || len(args[0].Elems) > 2 {
			p.errorf(x.Pos, "type switch needs a guard vector [v x] or [x]")
			return nil
		}
		gv := args[0].Elems
		g := new(TypeSwitchGuard)
		g.pos = args[0].Pos
		if len(gv) == 2 {
			g.Lhs = p.name(gv[0], false, true)
		}
		g.X = p.expr(gv[len(gv)-1])
		s.Tag = g
		args = args[1:]
	} else {
		if len(args) > 0 && args[0].Kind == lispVector {
			s.Init = p.initVector(args[0])
			args = args[1:]
		}
		if len(args) > 0 && !isClause(args[0]) {
			s.Tag = p.expr(args[0])
			args = args[1:]
		}
	}

	for _, c := range args {
		if !isClause(c) {
			p.errorf(c.Pos, "expected (case ...) or (default ...), found %s", lispDescribe(c))
			continue
		}
		cc := new(CaseClause)
		cc.pos = c.Pos
		body := c.Elems[1:]
		if lispHead(c) == "case" {
			if len(body) == 0 || body[0].Kind != lispVector || len(body[0].Elems) == 0 {
				p.errorf(c.Pos, "case values must be a non-empty vector")
				continue
			}
			cc.Cases = p.exprList(body[0].Elems)
			cc.Colon = lispEnd(body[0])
			body = body[1:]
		} else {
			cc.Colon = c.Elems[0].Pos
		}
		cc.Body = p.stmtList(body)
		s.Body = append(s.Body, cc)
	}
	s.Rbrace = lispEnd(x)
	return s
}

func (p *lispParser) selectStmt(x *lispForm) Stmt {
	s := new(SelectStmt)
	s.pos = x.Elems[0].Pos
	for _, c := range x.Elems[1:] {
		h := lispHead(c)
		if h != "case" && h != "default" {
			p.errorf(c.Pos, "expected (case ...) or (default ...), found %s", lispDescribe(c))
			continue
		}
		cc := new(CommClause)
		cc.pos = c.Pos
		body := c.Elems[1:]
		if h == "case" {
			if len(body) == 0 {
				p.errorf(c.Pos, "select case needs a communication")
				continue
			}
			cc.Comm = p.simpleStmt(body[0])
			cc.Colon = lispEnd(body[0])
			body = body[1:]
		} else {
			cc.Colon = c.Elems[0].Pos
		}
		cc.Body = p.stmtList(body)
		s.Body = append(s.Body, cc)
	}
	s.Rbrace = lispEnd(x)
	return s
}
