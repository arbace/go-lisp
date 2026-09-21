" Vim syntax file
" Language: go-lisp (Go written as s-expressions; golisp/SPEC.md)
" Filenames: *.lgo

if exists("b:current_syntax")
  finish
endif

let s:cpo_save = &cpo
set cpo&vim

" Symbols contain letters, digits, _ and the characters below, so that
" -name, read-all, x.-f, and := are single words.
syn iskeyword @,48-57,_,192-255,33,37,38,42,43,45-47,58,60-63,94,124,126

" Comments and directives.
syn keyword golispTodo contained TODO FIXME XXX BUG NOTE
syn match golispComment ";.*$" contains=golispTodo,@Spell
syn match golispDirective ";go:.*$"
syn match golispDirective "^;line .*$"
syn match golispDiscardMark "#_"

" Literals.
syn match golispEscape contained +\\\%([abfnrtv\\'"]\|x\x\{2}\|\o\{3}\|u\x\{4}\|U\x\{8}\)+
syn region golispString start=+"+ skip=+\\\\\|\\"+ end=+"+ contains=golispEscape,@Spell
syn region golispRawString start=+`+ end=+`+
syn match golispRune +'\%([^\\']\|\\\%([abfnrtv\\'"]\|x\x\{2}\|\o\{3}\|u\x\{4}\|U\x\{8}\)\)'+
syn match golispNumber "\%(^\|[[:space:]([{,]\)\@1<=[-+]\=\.\=\d[[:alnum:]_.]*\%([eEpP][-+]\d[[:alnum:]_]*\)\="

" Go keywords head special forms; they are never names (SPEC I3).
syn keyword golispKeyword package import func var const type if else for range switch case default select go defer return break continue goto fallthrough
syn keyword golispTypeKeyword struct interface map chan <-chan chan<- ...

" Keyword heads (:index, :lit, ...), :=, the :_ marker, and field keys.
syn match golispForm "\%(^\|[[:space:]([{,]\)\@1<=:[[:alnum:]_=-]\+"
syn match golispOmitted "\%(^\|[[:space:]([{,]\)\@1<=:_\ze\%([[:space:])\]}]\|$\)"

" Operators and assignments at the head of a list.
syn match golispOperator "(\@1<=\%(==\|!=\|<=\|>=\|<<=\|>>=\|&^=\|+=\|-=\|\*=\|/=\|%=\|&=\||=\|\^=\|&&\|||\|<<\|>>\|&^\|<-\|++\|--\|[-+*/%&|^!~<>=]\)\ze[[:space:])]"

" Predeclared names (the frozen list of SPEC F15).
syn keyword golispType any bool byte comparable complex128 complex64 error float32 float64 int int16 int32 int64 int8 rune string uint uint16 uint32 uint64 uint8 uintptr
syn keyword golispBuiltin append cap clear close complex copy delete imag len make max min new panic print println real recover
syn keyword golispConstant true false nil iota

" Declared names: (func name ...), (func [recv] name ...), (type name ...).
syn match golispFuncName "\%((func\s\+\%(\[[^]]*\]\s\+\)\=\)\@200<=[^[:space:]()[\]{}";]\+"
syn match golispTypeName "\%((type\s\+\)\@20<=[^[:space:]()[\]{}";]\+"

syn match golispDelimiter "[()[\]{}]"

hi def link golispComment Comment
hi def link golispTodo Todo
hi def link golispDirective PreProc
hi def link golispDiscardMark Comment
hi def link golispString String
hi def link golispRawString String
hi def link golispEscape SpecialChar
hi def link golispRune Character
hi def link golispNumber Number
hi def link golispKeyword Keyword
hi def link golispTypeKeyword Type
hi def link golispForm Special
hi def link golispOmitted Special
hi def link golispOperator Operator
hi def link golispType Type
hi def link golispBuiltin Function
hi def link golispConstant Constant
hi def link golispFuncName Function
hi def link golispTypeName Typedef
hi def link golispDelimiter Delimiter

let b:current_syntax = "golisp"

let &cpo = s:cpo_save
unlet s:cpo_save
