autoload -U add-zsh-hook
add-zsh-hook zshaddhistory _hishtory_add
add-zsh-hook precmd _hishtory_precmd

_hishtory_first_prompt=1

# For detecting color rendering support for this terminal, see #134
hishtory getColorSupport
export _hishtory_tui_color=$?

function _hishtory_add() {
    # Runs after <ENTER>, but before the command is executed
    # $1 contains the command that was run 
    _hishtory_command=$1
    _hishtory_start_time=`hishtory getTimestamp`
    if ! [ -z "$_hishtory_command " ]; then
        (hishtory presaveHistoryEntry zsh "$_hishtory_command" $_hishtory_start_time &) 2>&1 >/dev/null # Background Run
        # hishtory presaveHistoryEntry zsh "$_hishtory_command" $_hishtory_start_time 2>&1 >/dev/null # Foreground Run
    fi
}

function _hishtory_precmd() {
    # Runs after the command is executed in order to render the prompt
    # $? contains the exit code 
    _hishtory_exit_code=$?
    if [ -n "${_hishtory_first_prompt:-}" ]; then
        unset _hishtory_first_prompt
        return
    fi
    (hishtory saveHistoryEntry zsh $_hishtory_exit_code "$_hishtory_command" $_hishtory_start_time &) 2>&1 >/dev/null  # Background Run
    # hishtory saveHistoryEntry zsh $_hishtory_exit_code "$_hishtory_command" $_hishtory_start_time 2>&1 >/dev/null # Foreground Run
    (hishtory updateLocalDbFromRemote &) 2>&1 >/dev/null
}

_hishtory_widget() {
    BUFFER=$(HISHTORY_TERM_INTEGRATION=1 HISHTORY_SHELL_NAME=zsh hishtory tquery $BUFFER)
    CURSOR=${#BUFFER}
    zle reset-prompt
}

_hishtory_bind_control_r() {
    zle     -N   _hishtory_widget
    bindkey '^R' _hishtory_widget
}

[ "$(hishtory config-get enable-control-r)" = true ] && _hishtory_bind_control_r

# If running in a test environment, force loading of compinit so that shell completions work.
# Otherwise, we respect the user's choice and only run compdef if the user has loaded compinit.
if [ -n "${HISHTORY_TEST:-}" ]; then
    autoload -Uz compinit
    compinit
fi

source <(hishtory completion zsh); which compdef >/dev/null 2>&1 && compdef _hishtory hishtory || true