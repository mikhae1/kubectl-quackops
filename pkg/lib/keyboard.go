package lib

import (
	"os"
	"sync/atomic"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// StartEscWatcher starts a raw-input watcher that cancels the context on a standalone ESC key.
// It ignores ANSI escape sequences (e.g., arrow keys: ESC [ A) and Alt-modified keys (ESC + key).
// Ctrl-C/Z/\ trigger appropriate signals and immediate exit via CleanupAndExit.
// Returns a stop function that restores terminal state and stops the watcher.
// The stop function blocks until the watcher goroutine has fully exited.
func StartEscWatcher(cancel func(), spinnerManager *SpinnerManager, cfg *config.Config) func() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return func() {}
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return func() {}
	}

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	var stopped atomic.Bool
	var restored atomic.Bool

	restore := func() {
		if restored.CompareAndSwap(false, true) {
			_ = term.Restore(fd, oldState)
		}
	}

	go func() {
		defer close(doneCh)
		defer restore()

		// Use shorter polling interval (50ms) for more responsive ESC detection
		const pollInterval = 50 * time.Millisecond
		// Window to wait for escape sequence bytes after ESC
		const escSeqWindow = 30 * time.Millisecond

		pollTV := unix.NsecToTimeval(pollInterval.Nanoseconds())

		for {
			select {
			case <-stopCh:
				return
			default:
			}

			var readfds unix.FdSet
			readfds.Set(fd)
			tv := pollTV
			n, selErr := unix.Select(fd+1, &readfds, nil, nil, &tv)
			if selErr != nil || n <= 0 || !readfds.IsSet(fd) {
				continue
			}

			var b [1]byte
			nr, _ := os.Stdin.Read(b[:])
			if nr == 0 {
				continue
			}

			switch b[0] {
			case 27: // ESC
				if handleEscKey(fd, stopCh, escSeqWindow, cancel, spinnerManager) {
					return
				}
			case 3: // Ctrl+C
				restore()
				CleanupAndExit(cfg, CleanupOptions{ExitCode: -1})
				_ = unix.Kill(os.Getpid(), unix.SIGINT)
				return
			case 26: // Ctrl+Z
				restore()
				CleanupAndExit(cfg, CleanupOptions{ExitCode: -1})
				_ = unix.Kill(os.Getpid(), unix.SIGTSTP)
				return
			case 28: // Ctrl+\
				restore()
				CleanupAndExit(cfg, CleanupOptions{ExitCode: -1})
				_ = unix.Kill(os.Getpid(), unix.SIGQUIT)
				return
			}
		}
	}()

	return func() {
		if stopped.CompareAndSwap(false, true) {
			close(stopCh)
			restore()
			// Wait for goroutine to exit so next watcher can start immediately
			<-doneCh
		}
	}
}

// handleEscKey processes ESC byte and determines if it's a standalone ESC or part of a sequence.
// Returns true if the watcher should exit (ESC pressed or stopped).
func handleEscKey(fd int, stopCh chan struct{}, escSeqWindow time.Duration, cancel func(), spinnerManager *SpinnerManager) bool {
	// Check if stopped while processing
	select {
	case <-stopCh:
		return true
	default:
	}

	// Wait briefly to see if more bytes follow (ANSI escape sequence)
	var peekfds unix.FdSet
	peekfds.Set(fd)
	peekTV := unix.NsecToTimeval(escSeqWindow.Nanoseconds())
	n, _ := unix.Select(fd+1, &peekfds, nil, nil, &peekTV)

	if n <= 0 || !peekfds.IsSet(fd) {
		// No follow-up byte: standalone ESC pressed
		if spinnerManager != nil {
			spinnerManager.Update("Cancelling...")
		}
		cancel()
		return true
	}

	// Read the next byte to determine sequence type
	var seq [1]byte
	nr, _ := os.Stdin.Read(seq[:])
	if nr == 0 {
		// Read failed, treat as standalone ESC
		if spinnerManager != nil {
			spinnerManager.Update("Cancelling...")
		}
		cancel()
		return true
	}

	// Another ESC byte means user is pressing ESC rapidly - cancel immediately
	if seq[0] == 27 {
		if spinnerManager != nil {
			spinnerManager.Update("Cancelling...")
		}
		cancel()
		return true
	}

	// ANSI sequence (ESC [ ... or ESC O ...): drain remaining sequence bytes
	if seq[0] == '[' || seq[0] == 'O' {
		drainEscapeSequence(fd)
	}
	// Otherwise it's Alt+key (ESC + printable char): already consumed, ignore

	return false
}

// drainEscapeSequence consumes remaining bytes of an ANSI escape sequence.
func drainEscapeSequence(fd int) {
	deadline := time.Now().Add(10 * time.Millisecond)
	for time.Now().Before(deadline) {
		var drainfds unix.FdSet
		drainfds.Set(fd)
		drainTV := unix.Timeval{Sec: 0, Usec: 2000} // 2ms
		n, _ := unix.Select(fd+1, &drainfds, nil, nil, &drainTV)
		if n <= 0 || !drainfds.IsSet(fd) {
			break
		}
		var junk [1]byte
		nr, _ := os.Stdin.Read(junk[:])
		if nr == 0 {
			break
		}
		// Stop draining when we hit a terminal character (letter or ~)
		if (junk[0] >= 'A' && junk[0] <= 'Z') || (junk[0] >= 'a' && junk[0] <= 'z') || junk[0] == '~' {
			break
		}
	}
}
