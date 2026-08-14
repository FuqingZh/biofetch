package main

import (
	"os"

	rootcli "github.com/FuqingZh/biofetch/internal/biofetch"
	"github.com/FuqingZh/biofetch/internal/shared/logx"
)

func main() {
	if err := rootcli.RunCLI(os.Args[1:]); err != nil {
		_, _ = os.Stderr.WriteString("[biofetch] " + logx.RenderErrorLabel() + " " + err.Error() + "\n")
		os.Exit(1)
	}
}
