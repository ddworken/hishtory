# This script should be sourced inside of .bashrc to integrate bash with hishtory

# Implementation of running before/after every command based on https://jichu4n.com/posts/debug-trap-and-prompt_command-in-bash/
function __hishtory_precommand() {
  if [ -z "$HISHTORY_AT_PROMPT" ]; then
    return
  fi
  unset HISHTORY_AT_PROMPT

  # Run before every command
  HISHTORY_START_TIME=`date +%s`
}
trap "__hishtory_precommand" DEBUG

HISHTORY_FIRST_PROMPT=1
function __hishtory_postcommand() {
  EXIT_CODE=$?
  HISHTORY_AT_PROMPT=1

  if [ -n "$HISHTORY_FIRST_PROMPT" ]; then
    unset HISHTORY_FIRST_PROMPT
    return
  fi

  # Run after every prompt
  (hishtory saveHistoryEntry bash $EXIT_CODE "`history 1`" $HISHTORY_START_TIME &) # Background Run
  # hishtory saveHistoryEntry bash $EXIT_CODE "`history 1`" $HISHTORY_START_TIME  # Foreground Run
}
PROMPT_COMMAND="__hishtory_postcommand; $PROMPT_COMMAND"
export HISTTIMEFORMAT=$HISTTIMEFORMAT

__history_control_r() {
	READLINE_LINE=$(hishtory tquery "$READLINE_LINE" | tr -d '\n')
	READLINE_POINT=0x7FFFFFFF
}

__hishtory_bind_control_r() {
  bind -x '"\C-r": __history_control_r'
}

[ "$(hishtory config-get enable-control-r)" = true ] && __hishtory_bind_control_r
