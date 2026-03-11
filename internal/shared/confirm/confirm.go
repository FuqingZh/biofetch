package confirm

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func RequireLiteral(reader io.Reader, writer io.Writer, riskText string, literal string) error {
	if _, err := fmt.Fprintf(writer, "%s\n", riskText); err != nil {
		return fmt.Errorf("write confirmation prompt: %w", err)
	}
	if _, err := fmt.Fprintf(writer, "Type %q to continue.\n", literal); err != nil {
		return fmt.Errorf("write confirmation prompt: %w", err)
	}
	if _, err := io.WriteString(writer, "> "); err != nil {
		return fmt.Errorf("write confirmation prompt: %w", err)
	}

	textInput, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && err != io.EOF {
		return fmt.Errorf("read confirmation input: %w", err)
	}
	if strings.TrimSpace(textInput) != literal {
		return fmt.Errorf("confirmation failed; expected %q", literal)
	}
	return nil
}
