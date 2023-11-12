package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/ddworken/hishtory/shared/ai"
)

func main() {
	resp, err := ai.GetAiSuggestions("Find all CSV files in the current directory or subdirectories and select the first column, then prepend `foo` to each line", 3)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(strings.Join(resp, "\n"))
}
