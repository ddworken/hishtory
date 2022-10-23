function _hishtory_post_exec --on-event fish_postexec 
    # Runs after <ENTER>, but before the command is executed
    set --global _hishtory_command $argv
    set --global _hishtory_start_time (date +%s)
end

set --global _hishtory_first_prompt 1

function __hishtory_on_prompt --on-event fish_prompt
    # Runs after the command is executed in order to render the prompt
    # $? contains the exit code 
    set _hishtory_exit_code $status
    if [ -n "$_hishtory_first_prompt" ]
        set --global -e _hishtory_first_prompt
    else if [ -n "$_hishtory_command" ]
        hishtory saveHistoryEntry fish $_hishtory_exit_code "$_hishtory_command" $_hishtory_start_time &
    end 
end

function __hishtory_on_control_r
	set -l tmp (mktemp -t fish.XXXXXX)
	set -x init_query (commandline -b)
	hishtory tquery $init_query > $tmp
	set -l res $status
	commandline -f repaint
	if [ -s $tmp ]
		commandline -r (cat $tmp)
	end
	rm -f $tmp
end

[ (hishtory config-get enable-control-r) = true ] && bind \cr __hishtory_on_control_r