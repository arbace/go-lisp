" Vim compiler file
" Compiler: go build, for go-lisp (and Go) packages

if exists("current_compiler")
  finish
endif
let current_compiler = "golisp"

if exists(":CompilerSet") != 2
  command -nargs=* CompilerSet setlocal <args>
endif

let s:cpo_save = &cpo
set cpo&vim

" :make builds the package in the current directory; compiler errors
" point into .lgo files (file:line:col: message).
CompilerSet makeprg=go\ build\ .
CompilerSet errorformat=
      \%-G#%.%#,
      \%f:%l:%c:\ %m,
      \%f:%l:\ %m,
      \%-G%.%#

let &cpo = s:cpo_save
unlet s:cpo_save
