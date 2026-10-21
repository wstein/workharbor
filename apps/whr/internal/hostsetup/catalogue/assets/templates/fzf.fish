# WorkHarbor fzf key bindings and completions for interactive fish sessions.
if status is-interactive; and type -q fzf
    fzf --fish | source
end
