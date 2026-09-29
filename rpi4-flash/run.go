package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	stepStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	okStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	errStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
)

// runner executes external commands, sending their output to a log file so
// the terminal only shows step headers (and the log tail on failure).
type runner struct {
	log     *os.File
	verbose bool
}

func newRunner(logPath string, verbose bool) (*runner, error) {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(f, "\n===== rpi4-flash %s =====\n", time.Now().Format(time.RFC3339))
	return &runner{log: f, verbose: verbose}, nil
}

func (r *runner) step(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintln(os.Stderr, stepStyle.Render("==> "+msg))
	fmt.Fprintln(r.log, "==> "+msg)
}

func (r *runner) info(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintln(os.Stderr, "    "+msg)
	fmt.Fprintln(r.log, "    "+msg)
}

func (r *runner) warn(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintln(os.Stderr, warnStyle.Render("    WARNING: "+msg))
	fmt.Fprintln(r.log, "    WARNING: "+msg)
}

func (r *runner) output() io.Writer {
	if r.verbose {
		return io.MultiWriter(r.log, os.Stderr)
	}
	return r.log
}

// run executes a command; stdin may be empty.
func (r *runner) run(stdin string, name string, args ...string) error {
	fmt.Fprintf(r.log, "$ %s %s\n", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var tail bytes.Buffer
	w := io.MultiWriter(r.output(), &tail)
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, lastLines(tail.String(), 15))
	}
	return nil
}

// capture executes a command and returns its trimmed stdout.
func (r *runner) capture(name string, args ...string) (string, error) {
	fmt.Fprintf(r.log, "$ %s %s\n", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stderr = r.log
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
