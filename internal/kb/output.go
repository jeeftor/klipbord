package kb

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

type diagnosticStyles struct {
	debug, stage, value, url, success, warning, failure, accent lipgloss.Style
}

// newDiagnosticStyles scopes terminal detection to the stream receiving diagnostics.
func newDiagnosticStyles(output io.Writer) diagnosticStyles {
	renderer := lipgloss.NewRenderer(output)
	file, terminal := output.(interface{ Fd() uintptr })
	if !terminal || !term.IsTerminal(int(file.Fd())) || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		renderer.SetColorProfile(termenv.Ascii)
	} else {
		renderer.SetColorProfile(termenv.ANSI256)
	}
	return diagnosticStyles{
		debug:   renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("213")),
		stage:   renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("141")),
		value:   renderer.NewStyle().Foreground(lipgloss.Color("252")),
		url:     renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("81")),
		success: renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("82")),
		warning: renderer.NewStyle().Foreground(lipgloss.Color("214")),
		failure: renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("203")),
		accent:  renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("213")),
	}
}

var diagnosticTokens = regexp.MustCompile(`https?://[^\s)]+|"[^"]*"|\b[1-5][0-9]{2} [A-Za-z]+(?: [A-Za-z]+)*|\b(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|true|false)\b|\b[0-9]+(?:\.[0-9]+)?(?:ms|s)\b|\b[A-Z][A-Z0-9_]{2,}\b|--[a-z-]+`)

// highlight colors useful values without changing the underlying diagnostic text.
func (styles diagnosticStyles) highlight(text string, base lipgloss.Style) string {
	var result strings.Builder
	end := 0
	for _, match := range diagnosticTokens.FindAllStringIndex(text, -1) {
		result.WriteString(base.Render(text[end:match[0]]))
		token := text[match[0]:match[1]]
		style := styles.accent
		status := len(token) > 3 && token[3] == ' '
		switch {
		case strings.HasPrefix(token, "http://") || strings.HasPrefix(token, "https://"):
			style = styles.url
		case token == "true" || status && token[0] == '2':
			style = styles.success
		case status && (token[0] == '4' || token[0] == '5'):
			style = styles.failure
		case token == "false" || token[0] >= '0' && token[0] <= '9':
			style = styles.warning
		}
		result.WriteString(style.Render(token))
		end = match[1]
	}
	result.WriteString(base.Render(text[end:]))
	return result.String()
}

// PrintError writes a terminal-aware error and visually separates recovery advice.
func PrintError(output io.Writer, err error) {
	styles := newDiagnosticStyles(output)
	for index, line := range strings.Split(err.Error(), "\n") {
		if index == 0 {
			_, _ = fmt.Fprintln(output, styles.failure.Reverse(true).Render(" Error ")+" "+styles.highlight(line, styles.failure))
			continue
		}
		style := styles.warning
		if strings.HasPrefix(line, "Your profile remains saved") {
			style = styles.stage
		}
		_, _ = fmt.Fprintln(output, "  "+styles.highlight(line, style))
	}
}
