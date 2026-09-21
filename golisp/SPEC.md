# go-lisp syntax specification (draft)

This is the normative companion to `DESIGN.md`. For every node in
`src/cmd/compile/internal/syntax/nodes.go` it gives:
- the canonical Lisp form,
- the rule that tells it apart from every other form,
- how it is compared in the round-trip test.

The decisions it relies on are D1–D18 (see DESIGN.md). Points marked
**[A#]** came out of the first ambiguity audit, and **[F#]** marks fixes from
the second, adversarial audit. Both still need the user's approval.

Notation: `x?` means optional, `x*` means zero or more, and `x+` means one or
more. `stmt*` is a statement list.

## 0. Global invariants

- **I1. Go order.** Every form lists its components in Go's source order:
  receiver before name, `k v` before `range`, names before type before
  values, and so on. Only operators move in front of their operands.
  Positions therefore increase in the same order as in Go. **[F10]**
  types2 resolves names by declaration order, so positions don't change
  binding. They do feed diagnostics, DWARF scopes, the implicit-return line
  and `types2.Info` scope extents, so the parser must also fill in the end
  positions: `Rbrace` (BlockStmt, CompositeLit, SwitchStmt, SelectStmt) is
  the owning form's closing `)`. `CaseClause.Colon` / `CommClause.Colon` is
  the position right after the case vector or comm form. `File.EOF` is the
  end of the file.
- **I2. Classification is by position and head, never by type.** The parser
  decides what a form is from its head symbol or keyword, the number and
  shape of its elements (symbol, vector, list, string, keyword), and the
  syntactic context (expression, statement, type, decl, field list, header).
  It never needs name resolution.
- **I3. Special heads never denote identifiers.** **[F8]** A list head that is
  one of the following is always the special form:
  - one of Go's 25 keywords (including `range`, and `else` in an if body)
  - an operator symbol from §4
  - `=`, `++`, `--`, or an op-assign (`+=` ... `&^=`)
  - `<-chan`, `chan<-`
  - `...`
  - any keyword (`:index`, `:=`, ...)

  **[A1]** Under D18 a bare Lisp symbol that is a
  Go keyword (`map`, `type`, `func`, `range`, `select`, `else`, ...) always
  means the keyword. The exported Go identifier is written with its capital
  (`Map`, `Type`). Member positions (after a dot, a struct field name, a
  method name in a method decl, a keyword key) are never confused with
  keywords, so `v.type`, `(:kv :type 1)` and `(func [r t] type ...)` map to
  `Type` as usual. **[F1]** The exception is interface method names, which are
  list heads. There, a keyword-named method is written capitalized:
  `(interface (Type [] [reflect.type]))`.
- **I4. Vectors are never expressions.** A vector is always syntax: a
  binding list, a parameter list, a field group, a header, a case list, or a
  multi-name list. Types are never vectors either, which resolves several
  positional choices below.

## 1. Lexical syntax (reader)

| Token | Rule |
|---|---|
| whitespace | space, tab, newline, CR, and **comma** |
| `;` comment | to the end of the line. `;go:...` at column 1 is a directive (§9). **[A2]** `;line file:line[:col]` at column 1 is a line directive, mirroring `//line`. |
| `#_ form` | discards the next form (EDN) |
| `( )` `[ ]` | list, vector |
| `{ }` `#{ }` `#tag` | read, but **reserved**: the parser rejects them (none is used yet) |
| string `"..."` | Go interpreted-string grammar exactly: the Go escape set, and **[A3]** no literal newline (use a raw string). The BasicLit Value is the source text, verbatim. **[F6]** The reader validates literals as Go's scanner does (types2 trusts that). |
| raw string `` `...` `` | Go raw string. The Value is verbatim. |
| rune `'x'` | Go rune grammar exactly, and the Value is verbatim. `'` never occurs inside symbols. |
| number | Go number grammar exactly: decimal, `0x`/`0o`/`0b`/legacy `0` octal, `_` separators, floats including `.5`, hex floats, and the `i` suffix. The Value is verbatim. EDN `N`/`M` suffixes and ratios are errors. **[A4]** A leading sign (`-5`, `+1.5`) reads as the unary operation `(- 5)`, because Go has no negative literals. |
| keyword | `:` followed by symbol characters: `:index`, `:=`, `:_`, `:w`. Used only as special heads, the `:_` marker, and field keys. |
| symbol | any other run of symbol characters: Unicode letters and digits, `_`, and `+ - * / % & \| ^ < > = ! ~ . ?`. The reader does **not** turn `true`, `false` or `nil` into literals **[A5]**. They are predeclared, shadowable Go identifiers. |
| `...`, `.`, `_` | symbols with fixed roles (spread/dots, dot import, blank) |
| `()` | the empty statement (A10). **[F22]** It's an error in any expression, type or header position. |

Symbol classes used by the parser:
- **dotted symbol** `a.b.c`: split on `.`. Segments must be non-empty and
  must not be numbers. Each segment is mapped by D18 (the first as a bare
  name, the others as member names).
- **operator symbol**: one of the operators in §4.
- **name**: everything else, mapped by D18. A name may not contain `/`
  (reserved).
- **[F7] Validation.** Every name, *after* mapping, must match Go's
  identifier grammar (a letter or `_`, then letters, digits or `_`). So
  `empty?` and `my-pkg` in unmapped positions are errors, and this applies to
  the package name and import aliases too.

## 2. File and declarations

```
file      := directive* (package NAME) decl*        ; imports first, as in Go
```

| Node | Lisp | Disambiguation / notes |
|---|---|---|
| `File.PkgName` | `(package p)` | Must be the first form. `p` is never mapped by D18, but it must be a valid Go identifier (F7). |
| `File.Pragma`, `GoVersion` | `;go:...` directives before `(package ...)` | `GoVersion` comes from `;go:build` exactly as from `//go:build` |
| `ImportDecl` | `(import spec*)`, where spec is `"path"`, `[name "path"]`, `[. "path"]` or `[_ "path"]` | One spec is a single decl. Zero or 2+ specs form a group. |
| `ConstDecl` | `(const names type? (= values+)?)` | names is a symbol or `[sym+]`. The type is omitted when the next element is `=`. |
| `VarDecl` | `(var names type? (= values+)?)` | same as const |
| `TypeDecl` | `(type name [tparams]? type)` / `(type name [tparams]? = type)` | A vector after the name is the type params (I4). `=` makes it an alias. **[F6]** An empty tparams vector `[]` is an error. |
| decl group | `(const spec-list*)`, where each spec is a list: `(const (a t = iota) (b) (c))` | A group iff every argument is a list. A single decl's first argument is a symbol or vector. `(var)`, `(const)` and `(type)` are empty groups. |
| `FuncDecl` | `(func name [tparams]? [params] [results] body?)` | The 2nd element is a symbol. 2 leading vectors = params and results; 3 = tparams, params, results. **[F6]** In the 3-vector case the tparams vector must not be empty. |
| method `FuncDecl` | `(func [recv] name [tparams]? [params] [results] body?)` | The 2nd element is a vector and the 3rd a symbol. `recv` is `[r t]` or `[t]`. |
| body / no body | **[A6]** A body exists iff at least one form follows the results vector. `(func f [] [])` is a declaration without a body (asm or linkname). `(func f [] [] ())` has an empty body (see `()` below). | This distinction matters for both FuncDecl and FuncLit vs FuncType. |

Decl-spec grammar, precisely: `names`, then optionally a type (any type
form, which is never the symbol `=`), then optionally `=` followed by one or
more value expressions. Several values make a `ListExpr`.

## 3. Parameter and field lists

| Where | Lisp | Rule |
|---|---|---|
| params, results, type params, receiver | `[n1 T1, n2 T2 ...]` | Flat name/type pairs (D11). Names are symbols. |
| unnamed types | `[:_ T1 T2 ...]`, or `[T]` / `[T1 T2 T3]` (odd length) | A leading `:_` or an odd length means every element is a type. An even length without `:_` means pairs. |
| variadic | `[args (... T)]`, `[:_ (... T)]` | `DotsType`, allowed only in the last param |
| struct fields | `(struct group*)`, group = `[name+ T tag?]` or `[T tag?]` (embedded) | D13. **[F4]** A trailing literal token (string, raw string, number, rune) is a tag. No literal is ever a type. (Go accepts any literal syntactically, and types2 rejects non-strings.) If one element remains it's embedded, so its field name derives from the type **using bare rules** **[F18]**: `[error]` gives the field `error` and `[-base]` gives `base`, while `:error` / `x.error` mean `Error`. The printer emits `x.-error` for such fields. **[F27]** TagList is built as Go's `addField` does: padded with nil up to the last tagged field. |
| interface elements | `(interface elem*)` | **[A7/F1]** An element is a **method** iff it's a list of exactly 3 items whose head is a plain, non-special symbol (I3) and whose items 2 and 3 are vectors: `(name [params] [results])`. Any other element is an embedded type or constraint term, including func and struct types: `fmt.stringer`, `(\| (~ int) string)`, `(:index c t)`, `(func [:_ int] [string])`. |

Round-trip: Go's shared-type grouping (`a, b int`) is expanded to pairs.

## 4. Expressions

| Node | Lisp | Notes |
|---|---|---|
| `Name` | `sym` | D18 mapping (bare) |
| `BasicLit` | number / string / raw / rune token | Value verbatim |
| `CompositeLit` | `(:lit T elem*)`, `(:lit _ elem*)` | `_` = no type (an elided inner literal). `NKeys` = the number of `:kv` elements. |
| `KeyValueExpr` | `(:kv key value)` | only as a `:lit` element. A keyword key `:w` is a field name (member rules). A bare key is an expression (bare rules). |
| `FuncLit` | `(func [params] [results] stmt+)` | at least one form after the results vector [A6] |
| `ParenExpr` | none | **[A8/F9]** Lisp has no need for it, and the parser never produces it. Removing it is neutral for **valid** programs. A few invalid ones become valid (`switch (x.(type))`, `(a) := 1`). Go needs parentheses when printing, so `lisp2go` must run a precedence-aware pass that inserts them (`syntax/printer.go` doesn't). |
| `SelectorExpr` | `a.b.c` (name chains), `(:sel expr name)`, `(:sel expr a.b)` | D12. `name` uses member rules, so `x.-f` is the unexported `f`. |
| `IndexExpr` | `(:index x i)`, `(:index f T1 T2 ...)` | Several indices make a `ListExpr` (instantiation). |
| `SliceExpr` | `(:slice x lo hi)`, `(:slice x lo hi max)` | exactly 2 or 3 indices, with the omitted marker (see **Q1**) for missing ones. `Full` iff there are 3. |
| `AssertExpr` | `(:assert x T)` | |
| `TypeSwitchGuard` | **[F2]** `(:assert x type)`, and `(:= v (:assert x type))` sets Lhs | The keyword `type` is never a type expression, so this can't be confused with an AssertExpr. This is the general form, which also covers the (invalid) `.(type)` outside a switch. `:type-switch` guards `[v x]` / `[x]` are sugar for it. |
| unary `Operation` | `(op x)` for `! - + ^ * & <- ~` | `*` covers both deref and pointer type (same AST). `~` appears in constraints. |
| binary `Operation` | `(op x y)` | ops: `\|\| && == != < <= > >= + - \| ^ * / % & &^ << >>` |
| n-ary chains | `(op a b c ...)` means `((a op b) op c)` | **[A9]** n-ary only for `+ - * / % & \| ^ &^ << >> && \|\|`. **Comparisons are strictly binary**, because Clojure's `(< a b c)` means something different. |
| `CallExpr` | `(f arg*)`, `(f arg* ...)` | Any list whose head isn't special (I3). A trailing `...` sets `HasDots`. `f` may be any expression: `((:index f int) x)`, `((* t) x)`, `((func [] [] ...))`. |
| `ListExpr` | implicit | Created where Go has comma lists: several indices, several LHS/RHS values, several results, case lists, several values in var/const. A 1-element list never becomes a `ListExpr`. |
| `ArrayType` | `(:array-of n T)`, `(:array-of ... T)` | `...` means Len nil |
| `SliceType` | `(:slice-of T)` | |
| `DotsType` | `(... T)` | param lists only |
| `StructType` | `(struct group*)` | §3 |
| `InterfaceType` | `(interface elem*)` | §3 |
| `FuncType` | `(func [params] [results])` | Exactly two vectors and nothing after them [A6]. |
| `MapType` | `(map K V)` | |
| `ChanType` | `(chan T)`, `(<-chan T)`, `(chan<- T)` | |
| `BadExpr` | none | only produced on errors |

## 5. Statements

In statement context, a form is a statement if its head is a statement
head. Otherwise it's an expression statement.

| Node | Lisp | Notes |
|---|---|---|
| `EmptyStmt` | `()` | **[A10]** The empty list is the empty statement. In a statement list it contributes nothing and only marks that a body exists (A6). As the target of a label, `(:label L ())` is `L:` with an EmptyStmt. Round-trip removes EmptyStmts from lists. |
| `LabeledStmt` | `(:label L stmt)` | |
| `BlockStmt` | `(:block stmt*)` | |
| `ExprStmt` | any expression form | |
| `SendStmt` | `(<- ch v)` | 2 arguments. With 1 argument it's a receive expression. |
| `DeclStmt` | `(var ...)`, `(const ...)`, `(type ...)` | same forms as §2 |
| `AssignStmt` `=` | `(= lhs rhs+)` | **[A11]** lhs is one expression or a vector `[e1 e2 ...]`. The rhs values follow as rest arguments: `(= [a b] b a)`, `(= [v ok] (f))`. |
| `AssignStmt` `:=` | `(:= lhs rhs+)` | same shape |
| op-assign | `(op= lhs rhs)` for `+= -= *= /= %= &= \|= ^= <<= >>= &^=` | |
| inc/dec | `(++ x)`, `(-- x)` | Rhs nil |
| `BranchStmt` | `(break L?)`, `(continue L?)`, `(goto L)`, `(fallthrough)` | |
| `CallStmt` | `(go expr)`, `(defer expr)` | **[F23]** Any expression is accepted, as in Go's parser. types2 reports non-calls. |
| `ReturnStmt` | `(return expr*)` | several results make a `ListExpr` |
| `IfStmt` | `(if [init]? cond stmt* else-if* else?)` | **[F6]** cond is required. |
| | | `else-if` is `(else if [init]? cond stmt*)` and `else` is `(else stmt*)`, as trailing forms only. `(else (if ...))` is a block containing an if, which is distinct from `(else if ...)`. |
| `ForStmt` | `(for header stmt*)` | header below |
| `RangeClause` | in the header | |
| `SwitchStmt` | `(switch [init]? tag? clause*)` | Clauses are the lists headed `case` or `default`. Anything else before them is the tag. |
| `CaseClause` | `(case [e+] stmt*)`, `(default stmt*)` | The values are always a vector. |
| type switch | `(:type-switch [init]? guard clause*)` | guard is `[v x]` (Lhs v) or `[x]`. With two leading vectors, the first is init. |
| `SelectStmt` | `(select clause*)` | |
| `CommClause` | `(case comm stmt*)`, `(default stmt*)` | comm is one simple statement: `(<- ch v)`, `(<- ch)`, `(:= x (<- ch))`, `(:= [x ok] (<- ch))`, `(= ...)` |

**For header** (a vector):

| Header | Meaning |
|---|---|
| `[]` | `for {}` |
| `[cond]` | `for cond {}` (unless the element is a range clause, below) |
| `[init cond post]` | classic loop. Each part may be the omitted marker (**Q1**). init and post are simple statements. **[F6]** post must not be `:=`. |
| `[(range x)]` | `for range x` |
| `[k (range x)]`, `[k v (range x)]` | `k, v := range x` (short form). **The last element is a `range` form**, and since `range` is a keyword a classic post statement can't look like this. |
| `[(:= lhs (range x))]`, `[(= lhs (range x))]` | long forms. lhs is a name/expression or a vector. |
| anything else | **[F25]** an error. A 2-element header without a final `range` form is an error, and a final `(range x)` always makes it a range clause. |

`init` in `if`/`switch`/`:type-switch` is always a one-element vector
holding a simple statement: an expression, send, inc/dec, assignment or `:=`.
It is never confused with a condition, tag or guard, because those are
expressions (I4) or, for the guard, the last vector.

## 6. Names (D18), with the audit's additions

The rules are in DESIGN.md §4.2. The audits added these:
- **[A1]** Bare symbols equal to Go keywords are never names (see I3).
- **[A5]** `true`, `false` and `nil` are symbols on the predeclared
  exemption list.
- **[F15] Frozen exemption list.** The predeclared exemptions are listed
  here and don't follow the toolchain's universe. A future Go builtin can't
  silently rebind existing `.lgo` code:
  `any append bool byte cap clear close comparable complex complex128
  complex64 copy delete error false float32 float64 imag int int16 int32
  int64 int8 iota len make max min new nil panic print println real recover
  rune string true uint uint16 uint32 uint64 uint8 uintptr`, plus `main`
  and `init`.
- **[F14] The package name is *not* exempt.** A package's own name is never
  in scope, so exempting it only makes `(package time)` +
  `(type time ...)` declare an unexported `time` by accident.
- **[F16] Declaring an exempt name requires `-`.** A package-level or local
  declaration of an exempt name written bare (`(type error ...)`,
  `(func string ...)`, `(:= len 3)`) is an error. Write `-error` to shadow on
  purpose, or `Error` to export. References are unaffected. This is still
  purely lexical.
- **[A12] Import names are exempt, in this file.** That covers explicit
  aliases (verbatim, validated by F7) and implicit names. **[F17]** The
  implicit name is guessed precisely:
  1. Take the last path element.
  2. If that element is `vN` (N a number) and there's an earlier element,
     use the earlier element instead.
  3. Remove a `.vN` suffix.

  If the result isn't a valid Go identifier (`go-yaml`), the import must
  have an alias, or it's an error. Blank and dot imports add no names. If the
  real package name differs from the guess, Go reports the undefined name,
  so nothing is silently misbound.
- **[A13]** Import exemptions are per file, so a bare `path` can mean the
  import in one file and the package-level `Path` in another. Write `Path`
  to be explicit. (We keep this rule rather than exempting only dotted
  first segments. That alternative would make a local `path` map to `Path`
  while `path.x` still meant the import, which captures names silently.)
- **[F18]** Embedded field names follow the type's bare mapping. See §3.
- **[F24] Sign gotchas:** `(-5)` is a *call* of `(- 5)`, and `(-x)` calls the
  unexported `x`. Negation is `(- x)`. `-_` is `_`.
- **[F26]** A keyword key must be a single segment (no dots). `:_` is not a
  key.
- **Lint (not errors) [F19, F20]:** warn when a local or param shadows a
  same-file package-level or type-param name that has the same Go spelling
  (`[reader reader]` makes `Reader Reader`). The same applies to names
  declared by dot imports. Remember that exported-by-default is visible at
  runtime: methods named `string`/`error` satisfy `fmt.Stringer`/`error`,
  fields are seen by `encoding/json` and `reflect`, and interfaces are no
  longer sealed unless they have a `-` method.

## 7. Directives

`;go:NAME args` at column 1, with no space after `;`, goes to
`PragmaHandler` as the text `go:NAME args`, with Go's attachment rules
**[F11]**:
- directives before `(package ...)` go to `File.Pragma` (`;go:build`,
  `;go:debug`)
- directives right before a decl form attach to that decl
- directives before a **group** form are an error ("misplaced
  directive"), as with Go's `var (`. Directives before a spec list
  *inside* a group attach to that spec (`;go:embed` on a grouped var).
- directives before statements, or inside expressions, are errors
- directive text uses Go spellings verbatim

`;line` (A2) mirrors `//line` at column 1. **[F28]** An ordinary comment
that starts `;line ` or `;go:` at column 1 *is* a directive, as in Go.
Write `; line` or `;; go:` to avoid that. **[F12]** Only column-1 line
directives exist. `/*line*/` forms can't be spelled, and `lisp2go` can't
rebuild position bases, so behavioral tests must not compare line numbers
across a conversion.

## 7a. The parser must reject [F5, F6]

types2 trusts the parser for these, so the Lisp parser must check them
itself:
- **Branches:** run Go's own `checkBranches` on every function body (the
  compiler sets `IgnoreBranchErrors`). This catches `break`/`continue`
  outside a loop, `fallthrough` in the last case or in a type switch,
  `goto` into a block or over a var decl, and undefined or unused labels.
  `checkBranches` is unexported, which leads to question **Q2**.
- `:=` as a for post statement.
- empty type-parameter vectors
- `if`, `for`, or a switch clause missing a required part
- malformed literals (A3, F6)
- any form in a position where §2–§5 don't allow it (vectors as
  expressions, `()` outside statement lists, `{}`, `#{}`, `#tag`, `/` in
  names)
- names that aren't valid Go identifiers after mapping (F7)

## 8. Round-trip equivalence

`Go → AST₁ → Lisp → AST₂`. The two trees must be equal after the following
normalizations, each of which is semantically neutral:
1. Remove `ParenExpr` (A8).
2. Remove `EmptyStmt` from statement lists, but not as a label target (A10).
3. Expand field groups into single fields (D11).
4. Ignore `Group` for single-spec declarations (D4).
5. Treat `for ;; {}` as `for {}` (Go already produces the same AST).
6. Compare `Operation{Sub, lit}` produced from `-5` with the one from
   `(- 5)`: they are the same AST.
7. Positions: don't compare them exactly, but **[F10]** check that they
   increase in source order and that `Rbrace`/`Colon`/`EOF` are set.
   Ignore comments except directives.

String literals are compared **exactly** **[F13]**. The reader keeps
Values verbatim, so any difference is a bug.

**[F9] The reverse direction** is tested too:
`Lisp → AST → Go (lisp2go, with parens inserted) → AST'`. It must give the
same tree. This catches precedence bugs in the Go printer. `syntax/printer.go`
never adds parentheses, so without that pass `(* (+ a b) c)` would print as
`a + b * c`.

Anything else that differs is a bug in the spec or the implementation.

## 9. Out of scope (for now)

- cgo (`import "C"` and the preamble). cgo preprocesses Go source itself.
  Use `C.-name` in the meantime.
- `/*line ...*/` block-comment line directives.
- Doc comments (the syntax AST doesn't carry them).

## 10. How coverage is established

1. **Exhaustive printer:** the AST → Lisp printer has a type switch over
   every node type in `nodes.go`, with a panic default. A new node type
   upstream breaks the build or tests, which forces a spec update.
2. **Corpus round-trip, in both directions:** all of `$GOROOT/src/**/*.go` and `$GOROOT/test/**/*.go`
   that parse as Go, compared per §8.
3. **Fuzzing:** `FuzzRoundTrip`, seeded with the corpus. Any Go source that
   the Go parser accepts **without syntax errors** must round-trip. F2
   makes that true even for `.(type)` outside a switch. Programs that are
   only rejected later, by types2, still have a spelling. The exception is
   the invalid-only cases listed in A8/F9.
4. **Ambiguity tests:** a table of Lisp snippets for each disambiguation rule
   above (A1–A13, I2–I4), asserting the node kind the parser chooses.
5. **Behavioral:** compile converted `$GOROOT/test` run-tests as `.lgo` and
   compare their output with the `.go` originals.

## 11. Open questions

- **Q1. The omitted marker.** D10/D15 use `_` for "omitted" (`(:slice x _ n)`,
  `[(:= i 0) _ (++ i)]`), and D5 uses it for "no type" (`(:lit _ ...)`). But
  `_` is also the blank identifier, so Go texts like `x[_:n]`, `_{}` and
  `for _ {}` have no spelling. Go's parser accepts all three, and only
  types2 rejects them. The range short form `[_ v (range x)]` also uses `_`
  as the blank. Options: keep `_` (and narrow the "every parseable Go
  source" claim to exclude these), or use `:_` as the marker everywhere
  (`(:slice x :_ n)`, `(:lit :_ ...)`, `[:_ c :_]`).
- **Q2. Where the parser lives.** `checkBranches` and Go's literal scanner
  are unexported in `syntax`. Options:
  - (a) Put the Lisp parser *inside* package `syntax` as new files
    (`syntax/lisp_*.go`). No existing upstream file is edited, and it gets
    full access.
  - (b) A separate `lispsyntax` package, plus one new file in `syntax` that
    exports small wrappers.
