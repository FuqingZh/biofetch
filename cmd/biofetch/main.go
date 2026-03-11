package main

import (
	"os"

	rootcli "biofetch/internal/biofetch"
	"biofetch/internal/logx"
)

func main() {
	if err := rootcli.RunCLI(os.Args[1:]); err != nil {
		_, _ = os.Stderr.WriteString("[biofetch] " + logx.RenderErrorLabel() + " " + err.Error() + "\n")
		os.Exit(1)
	}
}
