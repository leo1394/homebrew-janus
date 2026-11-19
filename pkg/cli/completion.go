package cli

// Self-contained Bash 3.2+ completion. The command contract and all candidates
// come from Cobra's read-only __complete protocol; no bash-completion dependency.
const bashCompletion = `# Bash 3.2+ completion for janus
_janus_complete() {
    local candidate directive=0 value
    COMPREPLY=()
    while IFS= read -r candidate; do
        case "$candidate" in
            :[0-9]*) directive=${candidate#:} ;;
            *)
                value=${candidate%%$'\t'*}
                [ -z "$value" ] || COMPREPLY[${#COMPREPLY[@]}]="$value"
                ;;
        esac
    done < <("${COMP_WORDS[0]}" __complete "${COMP_WORDS[@]:1:$COMP_CWORD}" 2>/dev/null)
    # This CLI has no path arguments; never fall back to local file suggestions.
    if declare -F compopt >/dev/null 2>&1 || type compopt >/dev/null 2>&1; then
        if (( directive & 2 )); then compopt -o nospace; else compopt +o nospace; fi
    else
        if (( directive & 2 )); then
            complete -o nospace -F _janus_complete janus
        else
            complete -F _janus_complete janus
        fi
    fi
}
complete -F _janus_complete janus
`
