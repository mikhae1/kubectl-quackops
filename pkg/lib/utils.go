package lib

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strings"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// CosineSimilarity computes cosine similarity between two embedding vectors.
func CosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// CosineSimilarityMatrix computes row‑wise cosine similarity between two equal‑shaped matrices X and Y.
// Each of X and Y must be a slice of the same number of float32 slices, each of equal length.
// Returns element i: dot(X[i],Y[i]) / (‖X[i]‖ · ‖Y[i]‖).
func CosineSimilarityMatrix(X, Y [][]float32) ([]float32, error) {
	n := len(X)
	if n != len(Y) {
		return nil, fmt.Errorf("CosineSimilarity: row count mismatch X has %d rows, Y has %d", n, len(Y))
	}

	result := make([]float32, n)
	for i := 0; i < n; i++ {
		xi, yi := X[i], Y[i]
		if len(xi) != len(yi) {
			return nil, fmt.Errorf("CosineSimilarity: column count mismatch at row %d: len(X[%d])=%d, len(Y[%d])=%d",
				i, i, len(xi), i, len(yi))
		}

		var dot, normX, normY float64
		for j := range xi {
			a := float64(xi[j])
			b := float64(yi[j])
			dot += a * b
			normX += a * a
			normY += b * b
		}

		if normX == 0 || normY == 0 {
			// if either vector is zero‐length, define similarity as zero
			result[i] = 0
		} else {
			result[i] = float32(dot / (math.Sqrt(normX) * math.Sqrt(normY)))
		}
	}

	return result, nil
}

// FormatCompactNumber returns a compact human-readable string for integers
// using letter suffixes: k (thousands), M (millions), B (billions), T (trillions).
// Examples: 950 -> "950", 2910 -> "2.9k", 1200000 -> "1.2M".
func FormatCompactNumber(value int) string {
	if value == 0 {
		return "0"
	}
	sign := ""
	n := value
	if n < 0 {
		sign = "-"
		n = -n
	}

	var scaled float64
	var suffix string
	switch {
	case n >= 1_000_000_000_000:
		scaled = float64(n) / 1_000_000_000_000.0
		suffix = "T"
	case n >= 1_000_000_000:
		scaled = float64(n) / 1_000_000_000.0
		suffix = "B"
	case n >= 1_000_000:
		scaled = float64(n) / 1_000_000.0
		suffix = "M"
	case n >= 1_000:
		scaled = float64(n) / 1_000.0
		suffix = "k"
	default:
		return fmt.Sprintf("%s%d", sign, n)
	}

	// Use one decimal place, then trim trailing .0
	s := fmt.Sprintf("%.1f", scaled)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return sign + s + suffix
}

// ConfirmWithSingleKey prompts the user and returns true only for 'y'/'Y'.
// It accepts ESC as an immediate "no" without requiring Enter. Enter defaults to "no".
// Falls back to line mode if raw mode is unavailable.
func ConfirmWithSingleKey(prompt string) bool {
	fmt.Print(prompt)

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		if oldState, err := term.MakeRaw(fd); err == nil {
			defer term.Restore(fd, oldState)

			var b [1]byte
			if _, err := os.Stdin.Read(b[:]); err == nil {
				// Echo a newline to move past the prompt line
				fmt.Println()
				switch b[0] {
				case 'y', 'Y':
					return true
				case 27: // ESC
					return false
				case '\r', '\n':
					return false
				default:
					return false
				}
			}
		}
	}

	// Fallback: canonical line mode
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

// ReadSingleKey prints a prompt and returns the first key pressed (raw mode if possible).
// Returns lowercase letter for alphabetic keys. ESC is returned as byte 27.
func ReadSingleKey(prompt string) byte {
	fmt.Print(prompt)

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		if oldState, err := term.MakeRaw(fd); err == nil {
			defer term.Restore(fd, oldState)
			var b [1]byte
			if _, err := os.Stdin.Read(b[:]); err == nil {
				if prompt != "" {
					fmt.Println()
				}
				// Normalize to lowercase letters for convenience
				if b[0] >= 'A' && b[0] <= 'Z' {
					return b[0] + 32
				}
				return b[0]
			}
		}
	}

	// Fallback line mode: read a line and take its first rune
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return 'n'
	}
	ch := line[0]
	if ch >= 'A' && ch <= 'Z' {
		ch = ch + 32
	}
	return ch
}

// ReadKey reads a single key or ANSI escape sequence (e.g., arrow keys) in raw mode and
// returns a normalized code string: "up", "down", "left", "right", "enter", "esc",
// "space", or a lowercased single-letter string (e.g., "y", "n", "e", "j", "k", "a").
func ReadKey(prompt string) string {
	if prompt != "" {
		fmt.Print(prompt)
	}
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		if oldState, err := term.MakeRaw(fd); err == nil {
			defer term.Restore(fd, oldState)
			// Wait until input is ready
			for {
				var readfds unix.FdSet
				readfds.Set(fd)
				// ~1 second timeout to avoid hanging indefinitely
				tv := unix.Timeval{Sec: 1, Usec: 0}
				_, err := unix.Select(fd+1, &readfds, nil, nil, &tv)
				if err == nil && readfds.IsSet(fd) {
					break
				}
			}
			// Read up to 8 bytes to capture typical ANSI sequences
			buf := make([]byte, 8)
			n, _ := os.Stdin.Read(buf)
			if prompt != "" {
				fmt.Println()
			}
			if n == 0 {
				return ""
			}
			b := buf[:n]
			// Arrow keys: ESC [ A/B/C/D
			if n >= 3 && b[0] == 27 && b[1] == 91 {
				switch b[2] {
				case 'A':
					return "up"
				case 'B':
					return "down"
				case 'C':
					return "right"
				case 'D':
					return "left"
				}
			}
			// Single byte keys
			switch b[0] {
			case 27:
				return "esc"
			case '\r', '\n':
				return "enter"
			case ' ':
				return "space"
			}
			// Normalize to lower-case alpha if applicable
			ch := b[0]
			if ch >= 'A' && ch <= 'Z' {
				ch = ch + 32
			}
			return string([]byte{ch})
		}
	}
	// Fallback line mode
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	ch := line[0]
	if ch >= 'A' && ch <= 'Z' {
		ch = ch + 32
	}
	switch ch {
	case 27:
		return "esc"
	case '\r', '\n':
		return "enter"
	case ' ':
		return "space"
	}
	return string([]byte{ch})
}
