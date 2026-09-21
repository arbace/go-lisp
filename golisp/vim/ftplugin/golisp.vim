" Vim filetype plugin
" Language: go-lisp (golisp/SPEC.md)

if exists("b:did_ftplugin")
  finish
endif
let b:did_ftplugin = 1

let s:cpo_save = &cpo
set cpo&vim

setlocal comments=:;;,:;
setlocal commentstring=;;\ %s
" Symbol characters (by code, since | and ^ are special here):
" ! % & * + - . / : < = > ? ^ | ~
setlocal iskeyword=@,48-57,_,192-255,33,37,38,42,43,45-47,58,60-63,94,124,126
setlocal shiftwidth=2 softtabstop=2 expandtab
setlocal formatoptions-=t formatoptions+=croql
setlocal suffixesadd=.lgo

compiler golisp

" :GolispRun [args]    run the main package in the current directory
" :GolispToGo          show the current file as Go in a new window
" :GolispFromGo {file} show a Go file as go-lisp in a new window
command! -buffer -nargs=* GolispRun execute '!go run . ' . <q-args>
command! -buffer GolispToGo call s:Show('go tool golisp lisp2go ' . shellescape(expand('%')), 'go')
command! -buffer -nargs=1 -complete=file GolispFromGo call s:Show('go tool golisp go2lisp ' . shellescape(<q-args>), 'golisp')

function! s:Show(cmd, ft) abort
  let out = systemlist(a:cmd)
  if v:shell_error
    echohl ErrorMsg | echo join(out, "\n") | echohl None
    return
  endif
  new
  setlocal buftype=nofile bufhidden=wipe noswapfile
  call setline(1, out)
  execute 'setfiletype ' . a:ft
  setlocal nomodified
endfunction

let b:undo_ftplugin = "setlocal comments< commentstring< iskeyword< shiftwidth< softtabstop< expandtab< formatoptions< suffixesadd<"
      \ . " | delcommand GolispRun | delcommand GolispToGo | delcommand GolispFromGo"

let &cpo = s:cpo_save
unlet s:cpo_save
