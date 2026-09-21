# go-lisp: design notes (living document)

Status: **draft v2**. The syntax is not settled. Every decision below that is
still open has an ID (D1, D2, ...) so we can talk about it and update it.
Settled decisions move to the "Decided" log at the bottom.

## 1. Goal

Add a second front end to the gc compiler. It accepts the whole Go language
written as s-expressions, in a Clojure/EDN flavor. The rest of the compiler
(types2, IR, SSA, linker) must not notice which syntax a file used.

## 2. Guiding principles

1. **Same AST, different reader.** The Lisp parser produces the same
   `*syntax.File` (`src/cmd/compile/internal/syntax/nodes.go`) as the Go
   parser. It has no separate semantics and no macro expansion inside the
   compiler. Go keeps its semantics exactly because the type checker sees an
   identical tree.
2. **Every Go program has a Lisp form, and the mapping can be reversed.**
   Every `syntax` AST can be printed as Lisp and parsed back to an equal AST.
   Every Lisp form can be printed as Go with the existing
   `syntax/printer.go`. This gives us:
   - a correctness test that runs over the whole corpus: `$GOROOT/src` plus
     `$GOROOT/test` (Go -> AST -> Lisp -> AST', then check AST == AST'),
   - a `go2lisp` / `lisp2go` converter pair for free,
   - an escape hatch for tools that only understand `.go` files.
3. **Every valid Go identifier must still be expressible.** A special-form
   name that is also a legal Go identifier (such as `slice`, `label` or
   `index`) would make some Go programs impossible to write (see D1).
4. **Small footprint in upstream files.** New code goes in new files and
   directories. Edits to existing upstream files are one-line hooks, marked
   `// go-lisp:`, so merging from golang/go stays easy.
5. **Stay close to EDN.** The reader is our own (the compiler can't have
   dependencies), so we could add anything. Every addition we make beyond EDN
   should be deliberate (see D2).

## 3. Architecture

```
foo.lgo ─┐
         ├─ lispsyntax.Parse ──┐
foo.go ──┴─ syntax.Parse ──────┴──> *syntax.File ──> types2 ──> unified IR ──> ...
```

- `src/cmd/compile/internal/lispsyntax/` (new package)
  - `reader.go`: EDN reader. Text -> forms (list, vector, map, set, symbol,
    keyword, string, char, number), each form with line and column.
  - `parser.go`: forms -> `syntax` nodes (`syntax.MakePos` for positions).
    Also calls `syntax.PragmaHandler` for directives (`go:noinline`, `go:embed`, ...).
  - `printer.go`: `syntax` AST -> Lisp text (the `go2lisp` direction).
  - `roundtrip_test.go`: runs the corpus test from §2.2.
- Hook in `noder/noder.go`: choose the parser from the file extension.
- A later phase makes `go build` understand the new files: `go/build`
  (extension, reading imports and build tags from the header) and `cmd/go`.
  gopls, vet, cgo and `go/types` stay Go-only. Until they support Lisp,
  tools can run on the converted `.go` output.

## 4. Syntax sketch (v2)

Conventions (decided): Go keywords and operators head special forms (D9).
Forms without a Go keyword use keyword heads (D1). Dotted symbols are
selectors. Directives are `;go:` comments (D7).

```clojure
;go:build linux || darwin

(package main)

(import "fmt"
        "os"
        [str "strings"]           ; import str "strings"
        [_ "embed"])

(type Shape
  (interface
    (Area [] [float64])))         ; method: name, params, results

(type Rect
  (struct
    [W H float64]                 ; field group: names... Type "tag"?
    [Name string "json:\"name\""]
    [io.Reader]                   ; embedded field
    [(* Base) "tag"]))            ; embedded pointer with tag

(type List [T any]               ; generic type: type params after the name
  (struct [head (* (:index node T))]))

(type Number
  (interface
    (| (~ int) (~ float64))       ; union / approximation elements
    fmt.Stringer                  ; embedded interface
    (String [] [string])))        ; method

(const
  (Sunday Weekday = iota)
  (Monday)
  (Tuesday))

;; method: the receiver pair comes before the name, as in Go
(func [r (* Rect)] Area [] [float64]
  (return (* r.W r.H)))

;; generics: three vectors after the name = type params, params, results
(func Map [T any, U any] [xs (:slice-of T), f (func [T] [U])] [(:slice-of U)]
  (:= out (make (:slice-of U) 0 (len xs)))
  (for [_ x (range xs)]
    (= out (append out (f x))))
  (return out))

(func Printf [format string, args (... any)] [n int, err error]
  (return (fmt.Fprintf os.Stdout format args ...)))

(func split [s string] [:_ string string]   ; unnamed results, even count: :_
  (return (:slice s _ 1) (:slice s 1 _)))

(func main [] []
  (:= r (:lit Rect (:kv W 3) (:kv H 4)))              ; Rect{W: 3, H: 4}
  (:= ps (:lit (:slice-of Point) (:lit _ 1 2) (:lit _ (:kv X 3))))
  (var n int = 0)
  (if [(:= [v ok] (:index m "k"))] ok
    (fmt.Println v)
    (else if (> n 0)
      (return))
    (else
      (panic "no")))
  (for [i p (range ps)]
    (fmt.Println i p))
  (defer (fmt.Println "bye"))
  (go (work (& r)))
  (switch n
    (case [1 2] (fmt.Println "small") (fallthrough))
    (default (fmt.Println "big")))
  (:type-switch [v x]
    (case [int string] (use v))
    (case [nil] (none)))
  (select
    (case (<- out v) (sent))
    (case (:= [y ok] (<- in)) (got y ok))
    (default (idle)))
  (:= re (regexp.MustCompile `\d+\.go`))   ; raw string
  (if (== c '\n') (++ lines)))
```

### 4.1 Mapping table

| Go | Lisp | Status |
|---|---|---|
| `x.f`, `pkg.F`, `a.b.c` | `x.f`, `pkg.F`, `a.b.c` | dotted symbol -> `SelectorExpr` chain |
| `f().X`, `(*p).X.Y` | `(:sel (f) X)`, `(:sel (* p) X.Y)` | D12 |
| `f(a, b)` | `(f a b)` | |
| `f(xs...)` | `(f xs ...)` | D6 |
| `a[i]`, `F[int, string]` | `(:index a i)`, `(:index F int string)` | both are `IndexExpr` |
| `a[lo:hi:max]`, `a[:n]` | `(:slice a lo hi max)`, `(:slice a _ n)` | D10 |
| `x.(T)` | `(:assert x T)` | |
| `T(x)` conversion | `(T x)`, `((* T) x)` | an ordinary call, as in Go |
| `*p`, `&x`, `&T{}` | `(* p)`, `(& x)`, `(& (:lit T))` | D14: no shorthand |
| `a + b + c`, `-x`, `!b` | `(+ a b c)`, `(- x)`, `(! b)` | n-ary ops fold to the left |
| `a == b` | `(== a b)` | Go spelling |
| `<-ch`, `ch <- v` | `(<- ch)`, `(<- ch v)` | 1 arg receives, 2 args send |
| `x := e`, `a, b := f()` | `(:= x e)`, `(:= [a b] (f))` | |
| `x = e`, `x += e`, `i++` | `(= x e)`, `(+= x e)`, `(++ i)` | |
| `var`/`const` | `(var x T = e)`, `(var x T)`, `(var x = e)`, `(var [a b] = (f))` | D4 |
| groups | `(const (A T = iota) (B) (C))` | D4: all arguments are lists |
| `type A B`, `type A = B` | `(type A B)`, `(type A = B)`, `(type (A B) (C = D))` | D4 |
| generic type decl `type L[T any] ...` | `(type L [T any] (struct ...))` | D13 |
| struct fields | `[a b T]`, `[f T "tag"]`, `[io.Reader]`, `[(* B) "tag"]` | D13 |
| interface elements | `(M [params] [results])`, `io.Reader`, `(| (~ int) string)` | D13 |
| `[]T`, `[N]T`, `[...]T` | `(:slice-of T)`, `(:array-of N T)`, `(:array-of ... T)` | D10 |
| `map[K]V`, `chan T`, `<-chan T`, `chan<- T` | `(map K V)`, `(chan T)`, `(<-chan T)`, `(chan<- T)` | |
| `*T` | `(* T)` | D14 |
| `func(int) error` | `(func [int] [error])` | odd length = unnamed types |
| `func(int, string) (n int, err error)` | `(func [:_ int string] [n int, err error])` | D11 |
| `func(x int) int { ... }` | `(func [x int] [int] stmts...)` | func literal = func without a name |
| `...T` param | `(... T)` | D6 |
| `T{...}` | `(:lit T elem...)`; `(:kv k v)`; elided type: `(:lit _ ...)` | D5 |
| `if` | `(if [init]? c stmts... (else if d stmts...)* (else stmts...)?)` | D3 |
| `for i := 0; i < n; i++ {}` | `(for [(:= i 0) (< i n) (++ i)] ...)`; `_` = omitted part | D15 |
| `for c {}` / `for {}` | `(for [c] ...)` / `(for [] ...)` | D15 |
| `for k, v := range xs {}` | `(for [k v (range xs)] ...)` | D15 |
| `for k, v = range xs {}`, `for range ch {}` | `(for [(= [k v] (range xs))] ...)`, `(for [(range ch)] ...)` | D15 |
| `switch init; tag {...}` | `(switch [init]? tag? (case [v...] stmts...) (default stmts...))` | D16 |
| `switch init; v := x.(type) {...}` | `(:type-switch [init]? [v x] (case [T...] ...) ...)`; `[x]` without a binding | D16 |
| `select {...}` | `(select (case (<- ch v) ...) (case (:= [x ok] (<- ch)) ...) (default ...))` | D16 |
| `fallthrough` | `(fallthrough)` | |
| `L: ...`, `break L`, `goto L` | `(:label L stmt)`, `(break L)`, `(goto L)` | |
| `{ ... }` block statement | `(:block stmts...)` | |
| `go f()`, `defer f()`, `return a, b` | `(go (f))`, `(defer (f))`, `(return a b)` | |
| `//go:noinline` | `;go:noinline` | D7 |
| literals | `".."` with Go escapes, `` `raw` ``, runes `'a'` `'\n'`, Go number formats | D2, D17 |

## 5. Open decisions

None of the syntax-level decisions are open. Smaller points to confirm while
writing `SPEC.md`:

- `select` cases hold a single communication form (`(<- ch v)`,
  `(:= [x ok] (<- ch))`, `(= x (<- ch))`, `(<- ch)`), with no vector.
- Imports: `(import "p" [name "p"] ...)`. One path = single decl, several =
  group.
- Lossless round-trip normalizations: expanded field groups (D11),
  single-spec groups (D4), and raw vs interpreted strings (compared by
  value).

## 6. Roadmap

0. Settle D1 to D9 using worked examples. Build a spec (`golisp/SPEC.md`) with
   one example for each `syntax` node type.
1. EDN reader with positions, plus tests.
2. Printer (AST -> Lisp) first. It forces a complete mapping for every node
   type and generates the test corpus.
3. Parser (Lisp -> AST), plus a round-trip test over `$GOROOT/src` and
   `$GOROOT/test`.
4. Compiler hook: `go tool compile foo.lgo`, hello world. Then `go run` on a
   converted test suite, checking behavior against the `.go` originals.
5. `go/build` + `cmd/go` support for mixed packages (`.go` and `.lgo` in one
   package).
6. Tooling: `go2lisp`/`lisp2go` commands, error messages, maybe an editor mode.

## Decided

- **D1 (2026-09-21): keyword heads.** Forms without a Go keyword use EDN
  keyword heads: `(:index a i)`, `(:assert x T)`, `(:lit T ...)`,
  `(:label L ...)`. Keywords can never be Go identifiers, so every Go
  identifier stays expressible without escapes.
- **D2 (2026-09-21): EDN plus minimal Go lexemes.** Strict EDN (lists,
  vectors, maps, sets, `#_`, `#tag`), extended only with what Go literals need:
  raw strings, hex floats, `_` digit separators, imaginary literals,
  `0o`/`0b` prefixes, and rune literals (see D17 for the exact forms).
  No `^`, `@`, `'` or `#"..."`.
- **D9 (2026-09-21): Go vocabulary, Lisp shape.** Heads are Go keywords and
  operators (`func`, `var`, `for`, `range`, `:=`, `==`). No Clojure aliases
  (`defn`, `let`, ...).
- **Scope (2026-09-21):** milestone 1 is the compiler (`go tool compile`
  accepts Lisp files) plus the round-trip test over `$GOROOT/src` and
  `$GOROOT/test`. `go build` support comes later.
- **D3 (2026-09-21): if/else follows Go's shape.** `(if [init]? cond stmts...
  (else if cond2 stmts...)* (else stmts...)?)`. Statements go directly in
  the body, with no implicit block form. The else-if chain is flat. `else`
  is a Go keyword, so a trailing `(else ...)` can't be mistaken for a call.
- **D4 (2026-09-21): `=` marker in declarations.** `(var names type? = values...)`,
  where names is a symbol or a vector of symbols. The same shape works for
  `const`, and `(type Name T)` / `(type Name = T)` for types. A group is a
  form whose arguments are all lists: `(const (A T = iota) (B) (C))`. A group
  with a single spec may print as a plain decl. That changes no semantics.
- **D7 (2026-09-21): directives are `;go:` comments.** They mirror Go's
  `//go:` rule (column 1, no space). The text goes to `syntax.PragmaHandler`
  unchanged, and `;go:build` at the top of the file mirrors `//go:build`.
- **D10 (2026-09-21): slices.** The types are `(:slice-of T)`,
  `(:array-of N T)` and `(:array-of ... T)`. The expression is
  `(:slice a lo hi max?)`, with `_` for an omitted bound.
- **D5 (2026-09-21): composite-literal elements are explicit.**
  `(:lit T elem...)`. A keyed element is `(:kv key value)`. An inner literal
  with its type omitted is `(:lit _ elem...)` (Go's bare `{...}`).
- **D6 (2026-09-21): Go-style variadics.** The param type is `(... T)`
  (`DotsType`). A call spreads with a trailing `...` symbol: `(f a xs ...)`.
- **D8 (2026-09-21): extension `.lgo`.**
- **D11 (2026-09-21): flat name/type pairs** for params, results and type
  params: `[a int, b int]` (commas are whitespace). A list of unnamed types
  starts with `:_`. An odd-length list can only be unnamed types, so the
  marker is optional there (`[float64]`). Pitfall: `[int error]` means *one
  named* result `int` of type `error`, which is valid Go. Go's grouping
  (`a, b int`) is not kept, so the round-trip test compares field lists
  after expanding groups.
- **D12 (2026-09-21): `(:sel expr X)`** selects on a non-name expression.
  A dotted tail chains: `(:sel (f) X.Y)`.
- **D13 (2026-09-21): struct field groups are vectors**, `[names... Type "tag"?]`.
  A group with no name is an embedded field: `[io.Reader]`,
  `[(* Base) "tag"]`. A trailing string is always a tag, and a list or dotted
  symbol can't be a name, so nothing is ambiguous. Generic types are
  `(type Name [T any] Type)`. Interface elements are method lists
  `(M [params] [results])`, embedded types, and `(| (~ int) string)` unions.
- **D14 (2026-09-21): no `*`/`&` shorthand.** Always `(* x)` / `(& x)`. With
  one argument, `*` is deref or a pointer type. With two or more, it's
  multiplication.
- **D15 (2026-09-21): for.** Header vector: `[init cond post]` (`_` =
  omitted), `[cond]`, `[]`. Range: the short form `[k v (range xs)]` means
  `:=`. The Go-shaped `[(= [k v] (range xs))]` and `[(range xs)]` cover the
  rest. `range` is a Go keyword, so none of these is ambiguous.
- **D16 (2026-09-21): switch.** Case values always go in a vector. The type
  switch has its own form, `(:type-switch [init]? [v x] clauses...)`
  (`[x]` when there's no binding). `select` cases hold one communication form.
- **D17 (2026-09-21): string and rune lexemes.** `"..."` accepts Go's full
  escape set (`\a \v \x41 \101 \u \U`). Go backtick raw strings are
  allowed. Runes are Go literals: `'a'`, `'\n'`, `'\x07'`, `'\U0001F600'`.
  This departs from EDN on purpose: there are no EDN `\c` chars, and D2
  left `'` unused.
