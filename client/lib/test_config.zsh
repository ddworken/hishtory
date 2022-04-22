# This is the same as config.zsh, except it doesn't run the save process in the background. This is crucial to making tests reproducible. 

autoload -U add-zsh-hook
add-zsh-hook zshaddhistory _hishtory_add
add-zsh-hook precmd _hishtory_precmd

_hishtory_first_prompt=1

function _hishtory_add() {
    # Runs after <ENTER>, but before the command is executed
    # $1 contains the command that was run 
    _hishtory_command=$1
    _hishtory_start_time=`date +%s`
}

function _hishtory_precmd() {
    # Runs after the command is executed in order to render the prompt
    # $? contains the exit code (TODO: is this always true? Could other precmds break this?)
    _hishtory_exit_code=$?
    if [ -n "$_hishtory_first_prompt" ]; then
        unset _hishtory_first_prompt
        return
    fi
    hishtory saveHistoryEntry zsh $_hishtory_exit_code "$_hishtory_command" $_hishtory_start_time
}
