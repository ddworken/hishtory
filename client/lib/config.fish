# For detecting color rendering support for this terminal, see #134
hishtory getColorSupport
export _hishtory_tui_color=$status

function _hishtory_post_exec --on-event fish_preexec 
    # Runs after <ENTER>, but before the command is executed
    set --global _hishtory_command $argv
    set --global _hishtory_start_time (hishtory getTimestamp)
    hishtory presaveHistoryEntry fish "$_hishtory_command" $_hishtory_start_time &> /dev/null &
    builtin disown
end

function __hishtory_on_prompt --on-event fish_postexec
    set _hishtory_exit_code $status
    hishtory saveHistoryEntry fish $_hishtory_exit_code \"$_hishtory_command\" $_hishtory_start_time &> /dev/null &
    builtin disown
    hishtory updateLocalDbFromRemote &> /dev/null &
    builtin disown
    set --global -e _hishtory_command  # Unset _hishtory_command so we don't double-save entries when fish_prompt is invoked but fish_postexec isn't
end

function __hishtory_on_control_r
    set -l tmp (mktemp -t fish.XXXXXX)
    set -x init_query (commandline -b)
    HISHTORY_TERM_INTEGRATION=1 HISHTORY_SHELL_NAME=fish hishtory tquery $init_query > $tmp
    set -l res $status
    commandline -f repaint
    if [ -s $tmp ]
        commandline -r -- (cat $tmp)
    end
    rm -f $tmp
end

[ (hishtory config-get enable-control-r) = true ] && bind \cr __hishtory_on_control_r
