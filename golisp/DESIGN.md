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

Base convention for this sketch: Go keywords and Go operators are the heads
of special forms. Vectors hold bindings, parameters and groups. Dotted
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
(func Map [T any, U any] [xs ([] T), f (func [T] [U])] [([] U)]
  (:= out (make ([] U) 0 (len xs)))
  (for [_ x (range xs)]
    (= out (append out (f x))))
  (return out))

(func main [] []
  (:= r (lit Rect {W 3 H 4}))    ; Rect{W: 3, H: 4}
  (if [(:= [v ok] (lookup r))] ok
    (fmt.Println v)
    (else (panic "no")))
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
| `a[i]` | `(aget a i)` / `(. a i)` / ? | D1: needs a form name |
| `a[lo:hi:max]` | `(slice a lo hi max)` / ? | D1 |
| `F[int, string]` (instantiation) | `(F int string)`? no, that is a call | `IndexExpr`, same form as `a[i]` |
| `x.(T)` | `(assert x T)` / ? | D1 |
| `T(x)` conversion | `(T x)` / `((* T) x)` | just a call, same as in Go |
| `*p`, `&x` | `(* p)`, `(& x)`; sugar `*p`, `&x` | symbols with `*`/`&` can't be Go identifiers |
| `a + b + c` | `(+ a b c)` | n-ary ops fold to the left |
| `a == b` | `(== a b)` | Go spelling: `=` is assignment, `==` is equality |
| `<-ch` / `ch <- v` | `(<- ch)` / `(<- ch v)` | 1 arg receives, 2 args send |
| `x := e` / `a, b := f()` | `(:= x e)` / `(:= [a b] (f))` | |
| `x = e`, `x += e`, `i++` | `(= x e)`, `(+= x e)`, `(++ i)` | |
| `var x T = e` | `(var x T e)`, `(var x T)`, `(var x _ e)`? | D4 |
| `const ( A T = iota; B; C )` | `(const [A T iota] [B] [C])` | groups matter (iota, repeated values) |
| `type A = B` | `(type A = B)` | alias |
| `[]T`, `[N]T`, `[...]T` | `([] T)`, `([N] T)`, `([...] T)`? | D1 |
| `map[K]V`, `chan T`, `<-chan T`, `chan<- T` | `(map K V)`, `(chan T)`, `(<-chan T)`, `(chan<- T)` | `map` and `chan` are Go keywords, so no collisions |
| `func(int) error` | `(func [int] [error])` | type: params without names |
| `func(x int) int { ... }` | `(func [x int] [int] ...)` | func literal = func with no name |
| `T{...}` | `(lit T ...)` / `(T. ...)` / ? | D5: elided inner types as `[..]`, keyed items as `{k v}` |
| `if init; c {} else {}` | `(if [init] c then... (else ...))` | D3 |
| `for i := 0; i < n; i++ {}` | `(for [(:= i 0) (< i n) (++ i)] ...)` | |
| `for c {}` / `for {}` | `(for [c] ...)` / `(for [] ...)` | |
| `for k, v := range xs {}` | `(for [k v (range xs)] ...)`; `(for [k v (= range xs)] ...)`? | `=` vs `:=` range |
| `switch` / type switch / `select` | `(switch ...)` / `(switch [v (type x)] ...)` / `(select (case (<- ch v) ...))` | |
| `L: for ...`, `break L`, `goto L` | `(label L (for ...))`? `(break L)` `(goto L)` | D1 |
| `//go:noinline` | `;go:noinline` (same rule as Go comments) or `^{:go/noinline true}` | D7 |
| literals | EDN strings, `\a` chars, numbers; Go-only number formats `0x1p-2`, `1_000`, `3i`, `0o17` | D2 |

## 5. Open decisions

- **D1: names for non-keyword forms.** Go has 25 keywords, and they are safe
  to use as heads. We still need about 10 more forms: index, slice
  expression, type assertion, slice/array type, composite literal, label,
  block, and possibly others. Options:
  - (a) Reserve plain words (`index`, `slice`, `lit`, `label`, `do`, ...) and
    add an escape for Go identifiers that collide, for example `|slice|`.
  - (b) Use heads that can't be Go identifiers: punctuation (`([] T)`),
    names with `-`, `!` or `?`, or leading-dot Clojure interop (`.-field`,
    `.method`).
  - (c) Use keyword heads (`(:index a i)`). No collisions and valid EDN, but
    visually noisy.
  - (d) Use namespaced symbols (`go/index`). `/` never appears in Go
    identifiers.
- **D2: reader strictness.** Strict EDN (only `#tag`, `#_` and `#{}`) or the
  Clojure reader subset (`^meta`, `@x`, `'x`, `#"re"`)? Go-only lexical
  things such as raw strings, hex floats, `_` in numbers, imaginary literals
  and `0o` literals need an answer either way.
- **D3: statement shape of `if`/`else`.** Should `(if c then else)` behave
  like Clojure, with `(do ...)` for blocks? Or should the form be Go-shaped?
  Where does the `init` go?
- **D4: optional types in `var`/`const`, grouped names.** `var a, b int`
  versus `var a, b = 1, 2`. Positional forms become ambiguous here. Keyword
  markers could fix it, for example `(var [a b] :- int := [1 2])`.
- **D5: composite literals.** `(lit T ...)`, the Clojure constructor
  `(T. ...)`, or a tagged literal `#T{...}`?
- **D6: variadics.** Clojure's `&` in params and calls, or `(... T)`?
- **D7: pragmas and build constraints.** `go/build` reads `//go:build` from
  the file header, so the header format is fixed early.
- **D8: file extension** (`.lgo`, `.gol`, `.golisp`, `.edn`?) and the
  integration scope (compiler only vs `go build`).
- **D9: Clojure vocabulary.** Should there be aliases such as `defn`, `fn`,
  `let`, `when`, `cond` or `doseq`? Each would have to be pure syntax sugar
  with an exact Go AST equivalent, because of principle 2.

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

(nothing yet)
