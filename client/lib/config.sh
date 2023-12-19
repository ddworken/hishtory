# This script should be sourced inside of .bashrc to integrate bash with hishtory

# Include guard. This file is sourced in multiple places, but we want it to only execute once. 
# This trick is from https://stackoverflow.com/questions/7518584/is-there-any-mechanism-in-shell-script-alike-include-guard-in-c
if [ -n "$__hishtory_bash_config_sourced" ]; then return; fi
__hishtory_bash_config_sourced=`date`

# Implementation of running before/after every command based on https://jichu4n.com/posts/debug-trap-and-prompt_command-in-bash/
function __hishtory_precommand() {
  if [ -z "${HISHTORY_AT_PROMPT:-}" ]; then
    return
  fi
  unset HISHTORY_AT_PROMPT

  # Run before every command
  HISHTORY_START_TIME=`hishtory getTimestamp`
  CMD=`history 1`
  if ! [ -z "CMD " ] ; then
    (hishtory presaveHistoryEntry bash "$CMD" $HISHTORY_START_TIME &) # Background Run
    # hishtory presaveHistoryEntry bash "$CMD" $HISHTORY_START_TIME  # Foreground Run
  fi
}
trap "__hishtory_precommand" DEBUG

HISHTORY_FIRST_PROMPT=1
function __hishtory_postcommand() {
  EXIT_CODE=$?
  HISHTORY_AT_PROMPT=1

  if [ -n "${HISHTORY_FIRST_PROMPT:-}" ]; then
    unset HISHTORY_FIRST_PROMPT
    return
  fi

  # Run after every prompt
  (hishtory saveHistoryEntry bash $EXIT_CODE "`history 1`" $HISHTORY_START_TIME &) # Background Run
  # hishtory saveHistoryEntry bash $EXIT_CODE "`history 1`" $HISHTORY_START_TIME  # Foreground Run

  (hishtory updateLocalDbFromRemote &)
}
PROMPT_COMMAND="__hishtory_postcommand; $PROMPT_COMMAND"
export HISTTIMEFORMAT=$HISTTIMEFORMAT

__history_control_r() {
	READLINE_LINE=$(HISHTORY_TERM_INTEGRATION=1 hishtory tquery "$READLINE_LINE")
	READLINE_POINT=0x7FFFFFFF
}

__hishtory_bind_control_r() {
  bind -x '"\C-r": __history_control_r'
}

[ "$(hishtory config-get enable-control-r)" = true ] && __hishtory_bind_control_r

source <(hishtory completion bash)