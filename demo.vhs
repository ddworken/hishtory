# Demo file used https://github.com/charmbracelet/vhs
Output backend/web/landing/www/img/demo.gif
Set FontSize 22
Set Width 2500
Set Height 1050

# Set up 
Hide 
Type "zsh" 
Enter 
Type "clear"
Enter
Show 

Type "find . -iname '*.go' | xargs -I {} -- gofmt -w {}"
Enter 
Sleep 2000ms

Type "ssh server"
Enter
Sleep 200ms
Type "# Then press control + r to search your history"
Enter 
Sleep 2800ms

Ctrl+R
Sleep 4000ms
Type "g"
Sleep 400ms
Type "of
Sleep 400ms
Type "mt"
Sleep 6000ms
Enter
Sleep 5000ms
