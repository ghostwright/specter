package main

import (
	"fmt"
	"os"

	"github.com/ghostwright/specter/cmd/specter/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
