//go:build linux || darwin

package main

import (
	"fmt"
	str "strings"
	_ "embed"
)

type Shape interface {
	Area() float64
	fmt.Stringer
	Type() string
}

type Rect struct {
	W, H  float64
	Name  string `json:"name"`
	cache float64
	*base
}

type List[T any] struct{ head *node[T] }

type Number interface{ ~int | ~float64 }

const (
	Sunday Weekday = iota
	Monday
)

var x, y = 1, 2

//go:noinline
func (r *Rect) Area() float64 { return r.W * r.H }

func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func split(s string) (string, string) { return s[:1], s[1:] }

func asm(x int) int

func main() {
	r := Rect{W: 3, H: 4}
	ps := []Point{{1, 2}, {X: 3}}
	var n int = 0
	if v, ok := m["k"]; ok {
		fmt.Println(v)
	} else if n > 0 {
		return
	} else {
		panic("no")
	}
	for i := 0; i < 10; i++ {
		n += i * (i - 1) - -5
	}
	for {
		break
	}
	for range ch {
	}
	defer func() {}()
	go work(&r)
	switch n {
	case 1, 2:
		fmt.Println("small")
		fallthrough
	default:
		fmt.Println(str.ToUpper("big"), a[1:n], b[:], c[1:2:3], x.(int))
	}
	switch v := x.(type) {
	case int, string:
		use(v)
	case nil:
	}
	select {
	case out <- v:
	case y, ok := <-in:
		got(y, ok)
	default:
	}
	if n > 0 && n < 10 || !done {
		ch <- 1
	}
outer:
	for k, v := range m {
		if k == "" {
			continue outer
		}
		_ = v
	}
	fmt.Printf("%v %c\n", '\n', args...)
	p := (*T)(nil).f().g.h
	var ch2 <-chan chan<- int
	_ = [...]int{1, 2}
	_ = map[string]int{"a": 1}
	_ = struct{}{}
	len := 3
	_ = 0x1p-2 + 3i + 1_000
}
