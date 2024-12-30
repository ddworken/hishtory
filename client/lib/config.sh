# This script should be sourced inside of .bashrc to integrate bash with hishtory

# Include guard. This file is sourced in multiple places, but we want it to only execute once. 
# This trick is from https://stackoverflow.com/questions/7518584/is-there-any-mechanism-in-shell-script-alike-include-guard-in-c
if [ -n "$__hishtory_bash_config_sourced" ]; then return; fi
__hishtory_bash_config_sourced=true

# For detecting color rendering support for this terminal, see #134
hishtory getColorSupport
export _hishtory_tui_color=$?

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
    # This code with $LAST_PRESAVED_COMMAND and $LAST_SAVED_COMMAND is necessary to work around a quirk of
    # bash. With bash, if you run the command `foo` and then press `Control+R`, it will trigger the DEBUG
    # signal. And `history 1` will still return `foo` so this will lead to duplicate pre-saves causing entries
    # to show up twice. This works around this issue by skipping presaving if the last commadn we presaved
    # was identical. 
    # 
    # This does lead to a potential correctness bug since it means if someone runs the same non-terminating
    # command twice in a row, we won't pre-save the second entry. But this seems reasonably unlikely
    # such that it is worth accepting this issue to mitigate the duplicate entries observed in
    # https://github.com/ddworken/hishtory/issues/215.
    if [[ "$CMD" != "$LAST_PRESAVED_COMMAND" ]] &&  [[ "$CMD" != "$LAST_SAVED_COMMAND" ]]; then 
      (hishtory presaveHistoryEntry bash "$CMD" $HISHTORY_START_TIME &) 2>&1 >/dev/null # Background Run
      # hishtory presaveHistoryEntry bash "$CMD" $HISHTORY_START_TIME 2>&1 >/dev/null # Foreground Run
    fi 
  fi
  LAST_PRESAVED_COMMAND=$CMD
}
trap "__hishtory_precommand" DEBUG

HISHTORY_FIRST_PROMPT=1
function __hishtory_postcommand() {
  EXIT_CODE=$?
  HISHTORY_AT_PROMPT=1

  if [ -n "${HISHTORY_FIRST_PROMPT:-}" ]; then
    unset HISHTORY_FIRST_PROMPT
    return $EXIT_CODE
  fi

  # Run after every prompt
  CMD=`history 1`
  (hishtory saveHistoryEntry bash $EXIT_CODE "$CMD" $HISHTORY_START_TIME &) 2>&1 >/dev/null # Background Run
  # hishtory saveHistoryEntry bash $EXIT_CODE "$CMD" $HISHTORY_START_TIME 2>&1 >/dev/null # Foreground Run

  LAST_SAVED_COMMAND=$CMD

  (hishtory updateLocalDbFromRemote &) 2>&1 >/dev/null

  return $EXIT_CODE
}
PROMPT_COMMAND="__hishtory_postcommand; $PROMPT_COMMAND"
export HISTTIMEFORMAT=$HISTTIMEFORMAT

__history_control_r() {
	READLINE_LINE=$(HISHTORY_TERM_INTEGRATION=1 HISHTORY_SHELL_NAME=bash hishtory tquery "$READLINE_LINE")
	READLINE_POINT=0x7FFFFFFF
}

__hishtory_bind_control_r() {
  bind -x '"\C-r": __history_control_r'
}

[ "$(hishtory config-get enable-control-r)" = true ] && __hishtory_bind_control_r
