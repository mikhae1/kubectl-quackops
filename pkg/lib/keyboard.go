package lib

import (
	"context"
	"os"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// StartEscWatcher starts a raw-input watcher that cancels the context on a standalone ESC key.
// It ignores ANSI escape sequences (e.g., arrow keys: ESC [ A) and Alt-modified keys (ESC + key).
// Ctrl-C/Z/\ trigger appropriate signals and immediate exit via CleanupAndExit.
// Returns a stop function that restores terminal state and stops the watcher.
func StartEscWatcher(ctx context.Context, cancel context.CancelFunc, spinnerManager *SpinnerManager, cfg *config.Config) func() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return func() {}
	}
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return func() {}
	}
	stopCh := make(chan struct{})
	restored := false

	go func() {
		defer func() {
			if !restored {
				_ = term.Restore(fd, oldState)
			}
		}()

		for {
			select {
			case <-stopCh:
				return
			case <-ctx.Done():
				return
			default:
			}

			var readfds unix.FdSet
			readfds.Set(fd)
			// Poll ~200ms like previous behavior to keep CPU low
			tv := unix.Timeval{Sec: 0, Usec: 200000}
			_, selErr := unix.Select(fd+1, &readfds, nil, nil, &tv)
			if selErr != nil {
				continue
			}
			if !readfds.IsSet(fd) {
				continue
			}

			var b [1]byte
			n, _ := os.Stdin.Read(b[:])
			if n == 0 {
				continue
			}

			switch b[0] {
			case 27: // ESC or start of escape/Alt sequence
				// Peek for a follow-up byte quickly to distinguish sequences (ESC+[ or ESC+O) and Alt+key
				var peekfds unix.FdSet
				peekfds.Set(fd)
				peekTV := unix.Timeval{Sec: 0, Usec: 25000} // ~25ms window
				_, _ = unix.Select(fd+1, &peekfds, nil, nil, &peekTV)
				if peekfds.IsSet(fd) {
					// There is a following byte: treat as sequence or Alt key; consume the rest quickly and ignore
					var seq [1]byte
					_, _ = os.Stdin.Read(seq[:])
					// If ANSI sequence (ESC [ or ESC O), drain remaining bytes briefly
					if seq[0] == '[' || seq[0] == 'O' {
						deadline := time.Now().Add(5 * time.Millisecond)
						for time.Now().Before(deadline) {
							var drainfds unix.FdSet
							drainfds.Set(fd)
							drainTV := unix.Timeval{Sec: 0, Usec: 1000}
							_, _ = unix.Select(fd+1, &drainfds, nil, nil, &drainTV)
							if !drainfds.IsSet(fd) {
								break
							}
							var junk [1]byte
							_, _ = os.Stdin.Read(junk[:])
						}
					}
					// Do not cancel on sequences/Alt
					continue
				}
				// Bare ESC pressed -> cancel
				if spinnerManager != nil {
					spinnerManager.Update("Cancelling...")
				}
				cancel()
				return

			case 3: // Ctrl+C -> SIGINT
				_ = term.Restore(fd, oldState)
				restored = true
				CleanupAndExit(cfg, CleanupOptions{ExitCode: -1})
				_ = unix.Kill(os.Getpid(), unix.SIGINT)
				return
			case 26: // Ctrl+Z -> SIGTSTP
				_ = term.Restore(fd, oldState)
				restored = true
				CleanupAndExit(cfg, CleanupOptions{ExitCode: -1})
				_ = unix.Kill(os.Getpid(), unix.SIGTSTP)
				return
			case 28: // Ctrl+\ -> SIGQUIT
				_ = term.Restore(fd, oldState)
				restored = true
				CleanupAndExit(cfg, CleanupOptions{ExitCode: -1})
				_ = unix.Kill(os.Getpid(), unix.SIGQUIT)
				return
			}
		}
	}()

	return func() {
		close(stopCh)
		if !restored {
			_ = term.Restore(fd, oldState)
		}
	}
}
