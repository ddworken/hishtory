# This script should be sourced inside of .bashrc to integrate bash with hishtory

# Implementation of PreCommand and PostCommand based on https://jichu4n.com/posts/debug-trap-and-prompt_command-in-bash/
function PreCommand() {
  if [ -z "$HISHTORY_AT_PROMPT" ]; then
    return
  fi
  unset HISHTORY_AT_PROMPT

  # Run before every command
  HISHTORY_START_TIME=`date +%s%N`
}
trap "PreCommand" DEBUG

HISHTORY_FIRST_PROMPT=1
function PostCommand() {
  EXIT_CODE=$?
  HISHTORY_AT_PROMPT=1

  if [ -n "$HISHTORY_FIRST_PROMPT" ]; then
    unset HISHTORY_FIRST_PROMPT
    return
  fi

  # Run after every prompt
  (hishtory saveHistoryEntry bash $EXIT_CODE "`history 1`" $HISHTORY_START_TIME &)
}
PROMPT_COMMAND="PostCommand"
