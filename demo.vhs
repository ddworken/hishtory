# Demo file used https://github.com/charmbracelet/vhs
Output backend/web/landing/www/img/demo.gif
Set FontSize 22
Set Width 2300
Set Height 1050

# Set up 
Hide 
Type "zsh" 
Enter 
Type "setopt interactivecomments"
Enter
Type "clear"
Enter
Set TypingSpeed 0.1
Show 

Type "find . -iname '*.go' | xargs -I {} -- gofmt -w {}"
Enter 
Sleep 4000ms

Type "ssh server"
Enter
Sleep 400ms
Type "# Then press control + r to search your history"
Enter 
Sleep 3800ms

Ctrl+R
Sleep 6000ms
Type "g"
Sleep 400ms
Type "of
Sleep 400ms
Type "mt"
Sleep 1000ms
Type " cwd:~/code/hishtory/"
Sleep 7000ms
Enter
Sleep 6000ms
