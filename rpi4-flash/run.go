package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	stepStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	okStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	errStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
)

// errInterrupted is returned when a signal (Ctrl+C, SIGTERM) stopped the run.
var errInterrupted = errors.New("interrupted")

// killGrace is how long an interrupted command gets to exit after SIGTERM
// before its whole process group is killed.
const killGrace = 15 * time.Second

// runner executes external commands, sending their output to a log file so
// the terminal only shows step headers (and the log tail on failure).
type runner struct {
	log     *os.File
	verbose bool
	// ctx is cancelled by SIGINT/SIGTERM; running commands are then stopped
	// so that the deferred unmounting can happen.
	ctx context.Context
}

func newRunner(ctx context.Context, logPath string, verbose bool) (*runner, error) {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(f, "\n===== rpi4-flash %s =====\n", time.Now().Format(time.RFC3339))
	return &runner{log: f, verbose: verbose, ctx: ctx}, nil
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

// command builds a command that stops when r.ctx is cancelled.
//
// It runs in its own process group: on cancellation the whole group gets
// SIGTERM (arch-chroot is a shell script; signalling only the shell would
// leave pacman/mkinitcpio running and the shell waiting for them), then
// SIGKILL after killGrace. arch-chroot's exit trap unmounts its API
// filesystems when it is terminated rather than killed.
func (r *runner) command(name string, args ...string) (*exec.Cmd, *atomic.Bool) {
	cmd := exec.CommandContext(r.ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	done := &atomic.Bool{}
	cmd.Cancel = func() error {
		pgid := cmd.Process.Pid
		time.AfterFunc(killGrace, func() {
			if !done.Load() {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
		})
		return syscall.Kill(-pgid, syscall.SIGTERM)
	}
	cmd.WaitDelay = killGrace + 5*time.Second
	return cmd, done
}

// run executes a command; stdin may be empty.
func (r *runner) run(stdin string, name string, args ...string) error {
	fmt.Fprintf(r.log, "$ %s %s\n", name, strings.Join(args, " "))
	cmd, done := r.command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var tail bytes.Buffer
	w := io.MultiWriter(r.output(), &tail)
	cmd.Stdout, cmd.Stderr = w, w
	err := cmd.Run()
	done.Store(true)
	if err != nil {
		if r.ctx.Err() != nil {
			return errInterrupted
		}
		return fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, lastLines(tail.String(), 15))
	}
	return nil
}

// capture executes a command and returns its trimmed stdout.
func (r *runner) capture(name string, args ...string) (string, error) {
	fmt.Fprintf(r.log, "$ %s %s\n", name, strings.Join(args, " "))
	cmd, done := r.command(name, args...)
	cmd.Stderr = r.log
	out, err := cmd.Output()
	done.Store(true)
	if err != nil {
		if r.ctx.Err() != nil {
			return "", errInterrupted
		}
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
