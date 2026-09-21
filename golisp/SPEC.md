# go-lisp syntax specification (draft)

This is the normative companion to `DESIGN.md`. For every node in
`src/cmd/compile/internal/syntax/nodes.go` it gives:
- the canonical Lisp form,
- the rule that tells it apart from every other form,
- how it is compared in the round-trip test.

The decisions it relies on are D1–D18 (see DESIGN.md). Points marked
**[A#]** came out of the ambiguity audit and still need the user's approval.

Notation: `x?` means optional, `x*` means zero or more, and `x+` means one or
more. `stmt*` is a statement list.

## 0. Global invariants

- **I1. Go order.** Every form lists its components in Go's source order:
  receiver before name, `k v` before `range`, names before type before
  values, and so on. Only operators move in front of their operands.
  Positions therefore increase in the same order as in Go, which matters
  because types2 uses positions for scope decisions (for example, in
  `(:= x x)` the right-hand `x` is the outer `x`).
- **I2. Classification is by position and head, never by type.** The parser
  decides what a form is from its head symbol or keyword, the number and
  shape of its elements (symbol, vector, list, string, keyword), and the
  syntactic context (expression, statement, type, decl, field list, header).
  It never needs name resolution.
- **I3. Special heads never denote identifiers.** A list head that is one of:
  - one of Go's 25 keywords,
  - an operator symbol,
  - a keyword (`:index`, `:=`, ...),
  - `else` (in an if body),
  - `...`

  is always the special form. **[A1]** Under D18 a bare Lisp symbol that is a
  Go keyword (`map`, `type`, `func`, `range`, `select`, `else`, ...) always
  means the keyword. The exported Go identifier is written with its capital
  (`Map`, `Type`). Member positions (after a dot, a field name, a method
  name, a keyword key) are never confused with keywords, so `v.type`,
  `(:kv :type 1)` and methods named `type` map to `Type` as usual.
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
| string `"..."` | Go interpreted-string grammar exactly: the Go escape set, and **[A3]** no literal newline (use a raw string). The BasicLit Value is the source text, verbatim. |
| raw string `` `...` `` | Go raw string. The Value is verbatim. |
| rune `'x'` | Go rune grammar exactly, and the Value is verbatim. `'` never occurs inside symbols. |
| number | Go number grammar exactly: decimal, `0x`/`0o`/`0b`/legacy `0` octal, `_` separators, floats including `.5`, hex floats, and the `i` suffix. The Value is verbatim. EDN `N`/`M` suffixes and ratios are errors. **[A4]** A leading sign (`-5`, `+1.5`) reads as the unary operation `(- 5)`, because Go has no negative literals. |
| keyword | `:` followed by symbol characters: `:index`, `:=`, `:_`, `:w`. Used only as special heads, the `:_` marker, and field keys. |
| symbol | any other run of symbol characters: Unicode letters and digits, `_`, and `+ - * / % & \| ^ < > = ! ~ . ?`. The reader does **not** turn `true`, `false` or `nil` into literals **[A5]**. They are predeclared, shadowable Go identifiers. |
| `...`, `.`, `_` | symbols with fixed roles (spread/dots, dot import, blank) |

Symbol classes used by the parser:
- **dotted symbol** `a.b.c`: split on `.`. Segments must be non-empty and
  must not be numbers. Each segment is mapped by D18 (the first as a bare
  name, the others as member names).
- **operator symbol**: one of the operators in §4.
- **name**: everything else, mapped by D18. A name may not contain `/`
  (reserved).

## 2. File and declarations

```
file      := directive* (package NAME) decl*        ; imports first, as in Go
```

| Node | Lisp | Disambiguation / notes |
|---|---|---|
| `File.PkgName` | `(package p)` | Must be the first form. `p` is never mapped by D18. |
| `File.Pragma`, `GoVersion` | `;go:...` directives before `(package ...)` | `GoVersion` comes from `;go:build` exactly as from `//go:build` |
| `ImportDecl` | `(import spec*)`, where spec is `"path"`, `[name "path"]`, `[. "path"]` or `[_ "path"]` | One spec is a single decl. Zero or 2+ specs form a group. |
| `ConstDecl` | `(const names type? (= values+)?)` | names is a symbol or `[sym+]`. The type is omitted when the next element is `=`. |
| `VarDecl` | `(var names type? (= values+)?)` | same as const |
| `TypeDecl` | `(type name [tparams]? type)` / `(type name [tparams]? = type)` | A vector after the name is the type params (I4). `=` makes it an alias. |
| decl group | `(const spec-list*)`, where each spec is a list: `(const (a t = iota) (b) (c))` | A group iff every argument is a list. A single decl's first argument is a symbol or vector. `(var)`, `(const)` and `(type)` are empty groups. |
| `FuncDecl` | `(func name [tparams]? [params] [results] body?)` | The 2nd element is a symbol. 2 leading vectors = params and results; 3 = tparams, params, results. |
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
| struct fields | `(struct group*)`, group = `[name+ T "tag"?]` or `[T "tag"?]` (embedded) | D13. A trailing string literal is a tag, and a string is never a type. If one element remains it's embedded, so its field name derives from the type. Names are plain symbols (member rules). |
| interface elements | `(interface elem*)` | **[A7]** An element is a **method** iff it's a list of exactly 3 items whose head is a plain symbol and whose items 2 and 3 are vectors: `(name [params] [results])`. Any other element is an embedded type or constraint expression: `fmt.stringer`, `(\| (~ int) string)`, `(:index c t)`. |

Round-trip: Go's shared-type grouping (`a, b int`) is expanded to pairs.

## 4. Expressions

| Node | Lisp | Notes |
|---|---|---|
| `Name` | `sym` | D18 mapping (bare) |
| `BasicLit` | number / string / raw / rune token | Value verbatim |
| `CompositeLit` | `(:lit T elem*)`, `(:lit _ elem*)` | `_` = no type (an elided inner literal). `NKeys` = the number of `:kv` elements. |
| `KeyValueExpr` | `(:kv key value)` | only as a `:lit` element. A keyword key `:w` is a field name (member rules). A bare key is an expression (bare rules). |
| `FuncLit` | `(func [params] [results] stmt+)` | at least one form after the results vector [A6] |
| `ParenExpr` | none | **[A8]** Lisp has no need for it. The parser never produces it. Round-trip removes ParenExpr before comparing (types2 unwraps it too). |
| `SelectorExpr` | `a.b.c` (name chains), `(:sel expr name)`, `(:sel expr a.b)` | D12. `name` uses member rules, so `x.-f` is the unexported `f`. |
| `IndexExpr` | `(:index x i)`, `(:index f T1 T2 ...)` | Several indices make a `ListExpr` (instantiation). |
| `SliceExpr` | `(:slice x lo hi)`, `(:slice x lo hi max)` | exactly 2 or 3 indices, with `_` meaning omitted. `Full` iff there are 3. `x[:]` is `(:slice x _ _)`. |
| `AssertExpr` | `(:assert x T)` | |
| `TypeSwitchGuard` | only inside `:type-switch` (§5) | |
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
| `CallStmt` | `(go call)`, `(defer call)` | |
| `ReturnStmt` | `(return expr*)` | several results make a `ListExpr` |
| `IfStmt` | `(if [init]? cond stmt* else-if* else?)` | `else-if` is `(else if [init]? cond stmt*)` and `else` is `(else stmt*)`, as trailing forms only. `(else (if ...))` is a block containing an if, which is distinct from `(else if ...)`. |
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
| `[init cond post]` | classic loop. Each part may be `_` for omitted. init and post are simple statements. |
| `[(range x)]` | `for range x` |
| `[k (range x)]`, `[k v (range x)]` | `k, v := range x` (short form). **The last element is a `range` form**, and since `range` is a keyword a classic post statement can't look like this. |
| `[(:= lhs (range x))]`, `[(= lhs (range x))]` | long forms. lhs is a name/expression or a vector. |

`init` in `if`/`switch`/`:type-switch` is always a one-element vector
holding a simple statement: an expression, send, inc/dec, assignment or `:=`.
It is never confused with a condition, tag or guard, because those are
expressions (I4) or, for the guard, the last vector.

## 6. Names (D18), with the audit's additions

The rules are in DESIGN.md §4.2. The audit added these:
- **[A1]** Bare symbols equal to Go keywords are never names (see I3).
- **[A12]** The import exemption list covers **all import names in the
  file, explicit aliases included**, and they map verbatim. Declaration and
  use stay consistent, and the generated Go reads better (`str.NewReader`,
  not `Str.NewReader`).
- **[A13]** The exemptions are *per file* (imports are file-scoped), so a
  bare `path` can mean the import `path` in one file and the package-level
  `Path` in another. The meaning is well defined but can surprise. Write
  `Path` to be explicit.
- **[A5]** `true`, `false` and `nil` are symbols on the predeclared
  exemption list.

## 7. Directives

`;go:NAME args` at column 1, with no space after `;`, goes to
`PragmaHandler` as the text `go:NAME args`, attached to the next decl exactly
as Go does. That includes `;go:build` and `;go:debug` before
`(package ...)`, and `;go:embed`, `;go:linkname`, `;go:noinline`, ... on
declarations. `;line` is covered by [A2]. Directive text uses Go spellings
verbatim.

## 8. Round-trip equivalence

`Go → AST₁ → Lisp → AST₂`. The two trees must be equal after the following
normalizations, each of which is semantically neutral:
1. Remove `ParenExpr` (A8).
2. Remove `EmptyStmt` from statement lists, but not as a label target (A10).
3. Expand field groups into single fields (D11).
4. Ignore `Group` for single-spec declarations (D4).
5. Compare string BasicLits by value, not spelling (raw vs interpreted).
6. Treat `for ;; {}` as `for {}` (Go already produces the same AST).
7. Compare `Operation{Sub, lit}` produced from `-5` with the one from
   `(- 5)`: they are the same AST.
8. Ignore positions, and ignore comments except directives.

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
2. **Corpus round-trip:** all of `$GOROOT/src/**/*.go` and `$GOROOT/test/**/*.go`
   that parse as Go, compared per §8.
3. **Fuzzing:** `FuzzRoundTrip`, seeded with the corpus. Any Go source that
   parses must round-trip.
4. **Ambiguity tests:** a table of Lisp snippets for each disambiguation rule
   above (A1–A13, I2–I4), asserting the node kind the parser chooses.
5. **Behavioral:** compile converted `$GOROOT/test` run-tests as `.lgo` and
   compare their output with the `.go` originals.
