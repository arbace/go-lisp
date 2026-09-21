# go-lisp: design notes (living document)

Status: **draft v0**. The syntax is not settled. Every decision below that is
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

## 4. Candidate syntax (v0 sketch, for discussion)

Base convention (D1/D9 decided): Go keywords and Go operators are the heads
of special forms. Forms that Go spells without a keyword use **keyword heads**
(`:index`, `:lit`, ...). Vectors hold bindings, parameters and groups. Dotted
symbols are selectors.

```clojure
(package main)

(import "fmt"
        [str "strings"]          ; import str "strings"
        [_ "embed"])

(type Shape
  (interface
    (Area [] [float64])))        ; method: name, params, results

(type Rect
  (struct
    [W H float64]
    [Name string "json:\"name\""]))   ; optional tag

;; method: the receiver vector comes before the name, as in Go
(func [r *Rect] Area [] [float64]
  (return (* r.W r.H)))

;; generics: type-parameter vector, then params, then results
(func Map [T any, U any] [xs (:slice-of T), f (func [T] [U])] [(:slice-of U)]
  (:= out (make (:slice-of U) 0 (len xs)))
  (for [_ x (range xs)]
    (= out (append out (f x))))
  (return out))

(func main [] []
  (:= r (:lit Rect {W 3 H 4}))   ; Rect{W: 3, H: 4}
  (var n int = 0)
  (if [(:= [v ok] (lookup r))] ok
    (fmt.Println v)
    (else if (> n 0)
      (return))
    (else
      (panic "no")))
  (defer (fmt.Println "bye"))
  (go (work &r))
  (switch x
    (case [1 2] (fmt.Println "small"))
    (default (fallthrough))))
```

### 4.1 Mapping table (v0)

| Go | Lisp (v0 proposal) | Notes |
|---|---|---|
| `x.f`, `pkg.F` | `x.f`, `pkg.F` | dotted symbol -> chain of `SelectorExpr` |
| `f(a, b)` | `(f a b)` | |
| `f(xs...)` | `(f & xs)` or `(f (... xs))` | D6 |
| `a[i]` | `(:index a i)` | |
| `a[lo:hi:max]`, `a[:n]` | `(:slice a lo hi max)`, `(:slice a _ n)` | `_` marks an omitted bound |
| `F[int, string]` (instantiation) | `(:index F int string)` | Go parses this as `IndexExpr` too |
| `x.(T)` | `(:assert x T)` | |
| `T(x)` conversion | `(T x)` / `((* T) x)` | just a call, same as in Go |
| `*p`, `&x` | `(* p)`, `(& x)`; sugar `*p`, `&x` | symbols with `*`/`&` can't be Go identifiers |
| `a + b + c` | `(+ a b c)` | n-ary ops fold to the left |
| `a == b` | `(== a b)` | Go spelling: `=` is assignment, `==` is equality |
| `<-ch` / `ch <- v` | `(<- ch)` / `(<- ch v)` | 1 arg receives, 2 args send |
| `x := e` / `a, b := f()` | `(:= x e)` / `(:= [a b] (f))` | |
| `x = e`, `x += e`, `i++` | `(= x e)`, `(+= x e)`, `(++ i)` | |
| `var x T = e` | `(var x T = e)`, `(var x T)`, `(var x = e)`, `(var [a b] = (f))` | `=` separates type and values, as in Go |
| `const ( A T = iota; B; C )` | `(const (A T = iota) (B) (C))` | a group is a list of spec lists (iota, repeated values) |
| `type A B`, `type A = B` | `(type A B)`, `(type A = B)`, group `(type (A B) (C = D))` | |
| `[]T`, `[N]T`, `[...]T` | `(:slice-of T)`, `(:array-of N T)`, `(:array-of ... T)` | |
| `map[K]V`, `chan T`, `<-chan T`, `chan<- T` | `(map K V)`, `(chan T)`, `(<-chan T)`, `(chan<- T)` | `map` and `chan` are Go keywords, so no collisions |
| `func(int) error` | `(func [int] [error])` | type: params without names |
| `func(x int) int { ... }` | `(func [x int] [int] ...)` | func literal = func with no name |
| `T{...}` | `(:lit T ...)` | D5: elided inner types as `[..]`, keyed items as `{k v}` |
| `if init; c {} else if d {} else {}` | `(if [init] c stmts... (else if d stmts...) (else stmts...))` | else-ifs are a flat chain of trailing forms |
| `for i := 0; i < n; i++ {}` | `(for [(:= i 0) (< i n) (++ i)] ...)` | |
| `for c {}` / `for {}` | `(for [c] ...)` / `(for [] ...)` | |
| `for k, v := range xs {}` | `(for [k v (range xs)] ...)`; `(for [k v (= range xs)] ...)`? | `=` vs `:=` range |
| `switch` / type switch / `select` | `(switch ...)` / `(switch [v (type x)] ...)` / `(select (case (<- ch v) ...))` | |
| `L: for ...`, `break L`, `goto L` | `(:label L (for ...))`, `(break L)`, `(goto L)` | |
| `//go:noinline`, `//go:build ...` | `;go:noinline`, `;go:build ...` | a `;go:` line comment at column 1 is a directive |
| literals | EDN strings, `\a` chars, numbers; Go-only number formats `0x1p-2`, `1_000`, `3i`, `0o17` | D2 |

## 5. Open decisions

- **D5: composite literals.** The head is decided: `(:lit T ...)`. Still
  open: how elements are written (a vector for an inner literal with its type
  omitted, a map for keyed elements?), and whether evaluation order is kept.
- **D6: variadics.** Clojure's `&` in params and calls, or `(... T)`?
- **D8: file extension** (`.lgo`, `.gol`, `.golisp`, `.edn`?). The scope
  part is decided (see below).

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
  raw strings (for example `#go/raw "..."`), hex floats, `_` digit
  separators, imaginary literals, `0o`/`0b` prefixes, and rune characters.
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
