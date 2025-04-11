package animator

import (
	"bytes"
	"testing"
	"time"
)

func TestTypewriterWriter_Basic(t *testing.T) {
	var output bytes.Buffer
	writer := NewTypewriterWriter(&output, WithDelay(1*time.Millisecond))

	// Write content
	testStr := "Hello, World!"
	n, err := writer.Write([]byte(testStr))

	if err != nil {
		t.Errorf("TypewriterWriter.Write() error = %v", err)
	}

	if n != len(testStr) {
		t.Errorf("TypewriterWriter.Write() wrote %d bytes, want %d", n, len(testStr))
	}

	// Allow some time for animation to process characters
	time.Sleep(50 * time.Millisecond)

	// Close to ensure all content is flushed
	if err := writer.Close(); err != nil {
		t.Errorf("TypewriterWriter.Close() error = %v", err)
	}

	// Check output
	got := output.String()
	if got != testStr {
		t.Errorf("TypewriterWriter output = %q, want %q", got, testStr)
	}
}

func TestTypewriterWriter_DisableAnimation(t *testing.T) {
	var output bytes.Buffer
	writer := NewTypewriterWriter(&output, WithDelay(10*time.Millisecond))

	// Disable animation immediately
	writer.DisableAnimation()

	// Write content - should be written directly without delay
	testStr := "Direct output without animation"
	_, err := writer.Write([]byte(testStr))

	if err != nil {
		t.Errorf("TypewriterWriter.Write() error = %v", err)
	}

	// No need to wait since animation is disabled

	// Close the writer
	if err := writer.Close(); err != nil {
		t.Errorf("TypewriterWriter.Close() error = %v", err)
	}

	// Check output
	got := output.String()
	if got != testStr {
		t.Errorf("TypewriterWriter output = %q, want %q", got, testStr)
	}
}

func TestTypewriterWriter_MultipleWrites(t *testing.T) {
	var output bytes.Buffer
	writer := NewTypewriterWriter(&output, WithDelay(1*time.Millisecond))

	// Write multiple chunks
	chunks := []string{"First chunk. ", "Second chunk. ", "Third chunk."}
	expectedOutput := "First chunk. Second chunk. Third chunk."

	for _, chunk := range chunks {
		_, err := writer.Write([]byte(chunk))
		if err != nil {
			t.Errorf("TypewriterWriter.Write() error = %v", err)
		}
	}

	// Allow time for all characters to be processed
	time.Sleep(100 * time.Millisecond)

	// Close and flush
	if err := writer.Close(); err != nil {
		t.Errorf("TypewriterWriter.Close() error = %v", err)
	}

	// Check output
	got := output.String()
	if got != expectedOutput {
		t.Errorf("TypewriterWriter output = %q, want %q", got, expectedOutput)
	}
}
