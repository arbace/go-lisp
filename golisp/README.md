# go-lisp

go-lisp is the Go toolchain with a second front end: it compiles Go
written as s-expressions, in `.lgo` files, next to ordinary `.go` files.
An `.lgo` file is parsed into the same syntax tree as the Go it stands
for, so everything after the parser (type checking, SSA, the linker, the
go command) treats both alike. The design is in [DESIGN.md](DESIGN.md)
and the grammar, form by form, in [SPEC.md](SPEC.md).

## Using it

Build this tree (`cd src && ./make.bash`), then use its `bin/go` as you
would use `go`:

```sh
go run main.lgo              # a single file
go build ./...               # packages may mix .go and .lgo files
go test ./...                # _test.lgo files too; -cover works
```

Build constraints and directives are `;go:` comments at column 1, as
`//go:` lines are in Go: `;go:build linux`, `;go:embed data.txt`,
`;go:noinline`.

Converting:

```sh
go tool golisp go2lisp x.go > x.lgo
go tool golisp lisp2go x.lgo > x.go    # gofmt-formatted
go tool golisp run x.lgo               # or build
```

Editors: Vim and Neovim support is in [vim/](vim/README.md).

## Names

Names are kebab-case and **exported by default**: a name maps to Go by
joining its segments in camelCase and capitalizing the first letter.

| go-lisp | Go |
|---|---|
| `new-scanner`, `read-all` | `NewScanner`, `ReadAll` |
| `-helper`, `-read-all` | `helper`, `readAll` (a leading `-` means unexported) |
| `serve-HTTP`, `io.EOF` | `ServeHTTP`, `io.EOF` (capitals stay as written) |
| `Map`, `New` | `Map`, `New` (a capitalized name is taken as it is) |

Predeclared names (`int`, `len`, `nil`, `error`, ...), `main`, `init` and
import names stay as they are. So do locals written without a `-`: they
become exported-looking names in Go (`(:= x 1)` is `X := 1`), which means
the same thing. `go2lisp` writes every unexported name with its `-`
(`-counts`), and hand-written code usually leaves locals plain. Go
keywords (`map`, `type`, `range`, ...) are special forms, so an exported
name that is a keyword is written capitalized: `Map`.

## Cheat sheet

Go keywords and operators head their forms; forms with no Go keyword use
EDN keywords such as `:index`. Dotted names are selectors. Vectors `[...]`
are never expressions: they hold parameters, fields, headers and cases.

| Go | go-lisp |
|---|---|
| `package main` | `(package main)` |
| `import ("fmt"; str "strings")` | `(import "fmt" [str "strings"])` |
| `fmt.Println(x)` | `(fmt.println x)` |
| `func Area(r *Rect) float64 { ... }` | `(func area [r (* rect)] [float64] ...)` |
| `func divide(a, b int) (int, error)` | `(func -divide [a int, b int] [:_ int error] ...)` |
| `func (r *Rect) Area() float64 { return r.W * r.H }` | `(func [r (* rect)] area [] [float64] (return (* r.w r.h)))` |
| `func Map[T, U any](xs []T, f func(T) U) []U` | `(func Map [t any, u any] [xs (:slice-of t), f (func [t] [u])] [(:slice-of u)] ...)` |
| `func(x int) string { ... }` | `(func [x int] [string] ...)` |
| `x := 1`, `a, b := f()` | `(:= x 1)`, `(:= [a b] (f))` |
| `x = y`, `x += 2`, `i++` | `(= x y)`, `(+= x 2)`, `(++ i)` |
| `var s Shape = r` | `(var s shape = r)` |
| `const (A = iota; B)` | `(const (a = iota) (b))` |
| `type Rect struct { W, H float64; name string }` | `(type rect (struct [w h float64] [-name string]))` |
| `type Shape interface { Area() float64 }` | `(type shape (interface (area [] [float64])))` |
| `type Pair[K comparable, V any] struct {...}` | `(type pair [k comparable, v any] (struct ...))` |
| `a + b*c`, `-x`, `!ok` | `(+ a (* b c))`, `(- x)`, `(! ok)` |
| `a == b`, `a < b` | `(== a b)`, `(< a b)` (comparisons take two operands) |
| `xs[i]`, `xs[1:]`, `Pair[string, int]` | `(:index xs i)`, `(:slice xs 1 :_)`, `(:index pair string int)` |
| `&Rect{W: 3, H: 4}` | `(& (:lit rect (:kv :w 3) (:kv :h 4)))` |
| `[]int{1, 2, 3}`, `map[string]int{"a": 1}` | `(:lit (:slice-of int) 1 2 3)`, `(:lit (map string int) (:kv "a" 1))` |
| `*p`, `v.(T)` | `(* p)`, `(:assert v t)` |
| `f(xs...)` | `(f xs ...)` |
| `if err != nil { ... } else if c { ... } else { ... }` | `(if (!= err nil) ... (else if c ...) (else ...))` |
| `for i := 0; i < n; i++ { ... }` | `(for [(:= i 0) (< i n) (++ i)] ...)` |
| `for cond { ... }`, `for { ... }` | `(for [cond] ...)`, `(for [] ...)` |
| `for _, x := range xs { ... }` | `(for [_ x (range xs)] ...)` |
| `switch { case q > 3: ...; default: ... }` | `(switch (case [(> q 3)] ...) (default ...))` |
| `switch v := s.(type) { case *Rect: ... }` | `(:type-switch [v s] (case [(* rect)] ...))` |
| `select { case v := <-ch: ... }` | `(select (case (:= v (<- ch)) ...))` |
| `ch <- 42`, `<-ch`, `make(chan int, 1)` | `(<- ch 42)`, `(<- ch)`, `(make (chan int) 1)` |
| `go f()`, `defer f()`, `return a, b` | `(go (f))`, `(defer (f))`, `(return a b)` |
| `` `raw` ``, `'x'`, `0x1F` | `` `raw` ``, `'x'`, `0x1F` (Go's literals) |
| `//go:noinline` | `;go:noinline` |

Everything else in Go (labels, goto, embedding, unions in constraints,
...) has a form too: see [SPEC.md](SPEC.md).

## How well it works

Every valid Go file in `$GOROOT/src` and `$GOROOT/test` (11,588 files)
converts to go-lisp and back to the identical syntax tree, and 966 of Go's
test programs behave the same when compiled from `.lgo`. `go vet`, `go fmt`,
cgo and gopls read Go syntax only; see the known limits in
[DESIGN.md](DESIGN.md).
