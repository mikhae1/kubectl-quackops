package animator

import (
	"context"
	"io"
	"sync"
	"time"
)

// TypewriterWriter implements an io.Writer that adds a typewriter animation effect
// to text output by introducing a delay between characters.
type TypewriterWriter struct {
	outWriter      io.Writer
	delay          time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
	charBuffer     []byte
	bufferMutex    sync.Mutex
	stopAnimation  bool
	processingDone chan struct{}
}

// TypewriterOption represents configuration options for the TypewriterWriter
type TypewriterOption func(*TypewriterWriter)

// WithDelay sets the delay between characters for the typewriter effect
func WithDelay(delay time.Duration) TypewriterOption {
	return func(tw *TypewriterWriter) {
		tw.delay = delay
	}
}

// NewTypewriterWriter creates a new TypewriterWriter with the given output writer and options
func NewTypewriterWriter(outWriter io.Writer, options ...TypewriterOption) *TypewriterWriter {
	ctx, cancel := context.WithCancel(context.Background())
	tw := &TypewriterWriter{
		outWriter:      outWriter,
		delay:          5 * time.Millisecond, // Default delay between characters
		ctx:            ctx,
		cancel:         cancel,
		charBuffer:     make([]byte, 0),
		processingDone: make(chan struct{}),
	}

	// Apply options
	for _, opt := range options {
		opt(tw)
	}

	// Start the animation goroutine
	go tw.animateOutput()

	return tw
}

// Write implements the io.Writer interface
func (tw *TypewriterWriter) Write(p []byte) (n int, err error) {
	// If animation is disabled, write directly to output
	if tw.stopAnimation {
		return tw.outWriter.Write(p)
	}

	// Add the bytes to the buffer
	tw.bufferMutex.Lock()
	tw.charBuffer = append(tw.charBuffer, p...)
	tw.bufferMutex.Unlock()

	// Return the number of bytes processed
	return len(p), nil
}

// animateOutput processes characters from the buffer at a constant rate
// to create a typewriter effect
func (tw *TypewriterWriter) animateOutput() {
	defer close(tw.processingDone)

	ticker := time.NewTicker(tw.delay)
	defer ticker.Stop()

	for {
		select {
		case <-tw.ctx.Done():
			// Write any remaining characters in the buffer
			tw.flushBuffer()
			return
		case <-ticker.C:
			tw.processNextChar()
		}
	}
}

// processNextChar outputs a single character from the buffer
func (tw *TypewriterWriter) processNextChar() {
	tw.bufferMutex.Lock()
	defer tw.bufferMutex.Unlock()

	if len(tw.charBuffer) > 0 {
		// Process the next character
		char := tw.charBuffer[0]
		tw.charBuffer = tw.charBuffer[1:]

		// Write directly to the output
		tw.outWriter.Write([]byte{char})
	}
}

// flushBuffer writes all remaining characters in the buffer to output
func (tw *TypewriterWriter) flushBuffer() {
	tw.bufferMutex.Lock()
	defer tw.bufferMutex.Unlock()

	if len(tw.charBuffer) > 0 {
		tw.outWriter.Write(tw.charBuffer)
		tw.charBuffer = nil
	}
}

// Flush ensures all buffered content is written to the output
func (tw *TypewriterWriter) Flush() error {
	tw.flushBuffer()
	return nil
}

// Close stops the animation and flushes any pending content
func (tw *TypewriterWriter) Close() error {
	tw.cancel()
	<-tw.processingDone // Wait for the processing goroutine to finish
	return nil
}

// DisableAnimation stops the typewriter animation and writes directly
func (tw *TypewriterWriter) DisableAnimation() {
	tw.bufferMutex.Lock()
	defer tw.bufferMutex.Unlock()

	tw.stopAnimation = true
	// Flush any existing buffer
	if len(tw.charBuffer) > 0 {
		tw.outWriter.Write(tw.charBuffer)
		tw.charBuffer = nil
	}
}
