go build -o /tmp/client clients/local/client.go
/tmp/client init
export PROMPT_COMMAND='/tmp/client upload $? "`history 1`"'
ls /a
ls /bar
ls /foo
echo foo
/tmp/client query