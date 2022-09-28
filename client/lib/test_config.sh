# This script should be sourced inside of .bashrc to integrate bash with hishtory
# This is the same as config.sh, except it doesn't run the save process in the background. This is crucial to making tests reproducible. 

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
  hishtory saveHistoryEntry bash $EXIT_CODE "`history 1`" $HISHTORY_START_TIME
}
PROMPT_COMMAND="__hishtory_postcommand; $PROMPT_COMMAND"
export HISTTIMEFORMAT=$HISTTIMEFORMAT
