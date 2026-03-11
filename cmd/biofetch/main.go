package main

import (
	"fmt"
	"os"

	rootcli "biofetch/internal/biofetch"
)

func main() {
	if err := rootcli.RunCLI(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "[biofetch][ERROR] %v\n", err)
		os.Exit(1)
	}
}
