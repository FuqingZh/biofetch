package logx

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

var styleError = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("9"))

func Logf(prefix string, format string, args ...interface{}) {
	logger := log.NewWithOptions(os.Stderr, log.Options{
		Prefix:          prefix,
		ReportTimestamp: false,
		ReportCaller:    false,
	})
	logger.Info(fmt.Sprintf(format, args...))
}

func RenderErrorLabel() string {
	return styleError.Render("ERROR")
}
