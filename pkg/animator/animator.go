package animator

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"
)

// Animation control constants
const (
	// DefaultCharDelay is the default delay between characters for the animation effect
	DefaultCharDelay = 5 * time.Millisecond
	// DefaultBatchSize is the default number of characters to process in each animation cycle
	DefaultBatchSize = 5
	// DefaultBufferCapacity is the initial capacity of the character buffer
	DefaultBufferCapacity = 1024
	// DefaultPollInterval is the interval to check for new content when the buffer is empty
	DefaultPollInterval = 50 * time.Millisecond
	// DefaultNewlineDelay is the delay multiplier applied to newline characters
	DefaultNewlineDelay = 0.5
)

// TypewriterWriter implements an io.Writer that adds a typewriter animation effect
// to text output by introducing a delay between characters.
type TypewriterWriter struct {
	outWriter        io.Writer
	delay            time.Duration
	ctx              context.Context
	cancel           context.CancelFunc
	charBuffer       []byte
	bufferMutex      sync.Mutex
	stopAnimation    bool
	processingDone   chan struct{}
	batchSize        int           // Number of characters to process in each batch
	contentAvailable chan struct{} // Signal that new content is available
	pollInterval     time.Duration // Interval to check for new content when buffer is empty
	newlineDelay     float64       // Multiplier for delay when encountering newlines
}

// TypewriterOption represents configuration options for the TypewriterWriter
type TypewriterOption func(*TypewriterWriter)

// WithDelay sets the delay between characters for the typewriter effect
func WithDelay(delay time.Duration) TypewriterOption {
	return func(tw *TypewriterWriter) {
		tw.delay = delay
	}
}

// WithBatchSize sets the number of characters to process in each animation cycle
func WithBatchSize(size int) TypewriterOption {
	return func(tw *TypewriterWriter) {
		if size > 0 {
			tw.batchSize = size
		}
	}
}

// WithPollInterval sets the interval to check for new content when the buffer is empty
func WithPollInterval(interval time.Duration) TypewriterOption {
	return func(tw *TypewriterWriter) {
		if interval > 0 {
			tw.pollInterval = interval
		}
	}
}

// WithNewlineDelay sets the delay multiplier for newline characters
func WithNewlineDelay(multiplier float64) TypewriterOption {
	return func(tw *TypewriterWriter) {
		if multiplier >= 0 {
			tw.newlineDelay = multiplier
		}
	}
}

// NewTypewriterWriter creates a new TypewriterWriter with the given output writer and options
func NewTypewriterWriter(outWriter io.Writer, options ...TypewriterOption) *TypewriterWriter {
	ctx, cancel := context.WithCancel(context.Background())
	tw := &TypewriterWriter{
		outWriter:        outWriter,
		delay:            DefaultCharDelay,
		ctx:              ctx,
		cancel:           cancel,
		charBuffer:       make([]byte, 0, DefaultBufferCapacity),
		processingDone:   make(chan struct{}),
		batchSize:        DefaultBatchSize,
		contentAvailable: make(chan struct{}, 1),
		pollInterval:     DefaultPollInterval,
		newlineDelay:     DefaultNewlineDelay,
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
	wasEmpty := len(tw.charBuffer) == 0
	tw.charBuffer = append(tw.charBuffer, p...)
	tw.bufferMutex.Unlock()

	// Signal content availability only if buffer was empty before
	// This prevents excessive signals when buffer already has content
	if wasEmpty {
		select {
		case tw.contentAvailable <- struct{}{}:
			// Signal sent
		default:
			// Channel already has a signal, no need to send another
		}
	}

	// Return the number of bytes processed
	return len(p), nil
}

// animateOutput processes characters from the buffer at a constant rate
// to create a typewriter effect
func (tw *TypewriterWriter) animateOutput() {
	defer close(tw.processingDone)

	for {
		// Try to process a batch of characters
		if !tw.processCharBatch() {
			// Buffer is empty, wait for new content or cancellation
			select {
			case <-tw.ctx.Done():
				// Context cancelled, exit the goroutine
				tw.flushBuffer()
				return
			case <-tw.contentAvailable:
				// New content available, continue immediately
				continue
			case <-time.After(tw.pollInterval):
				// Check occasionally in case we missed a signal
			}
		}
	}
}

// processCharBatch outputs a batch of characters from the buffer
// Returns true if characters were processed, false if buffer was empty
func (tw *TypewriterWriter) processCharBatch() bool {
	tw.bufferMutex.Lock()

	// If buffer is empty, there's nothing to do
	if len(tw.charBuffer) == 0 {
		tw.bufferMutex.Unlock()
		return false
	}

	// Calculate batch size (smaller of buffer length or max batch size)
	batchSize := tw.batchSize
	if batchSize > len(tw.charBuffer) {
		batchSize = len(tw.charBuffer)
	}

	// Special handling for newline characters
	// If the batch contains a newline, we'll process up to and including the newline
	nlIndex := bytes.IndexByte(tw.charBuffer[:batchSize], '\n')
	if nlIndex >= 0 {
		// Include the newline in the current batch
		batchSize = nlIndex + 1
	}

	// Extract the batch
	batch := tw.charBuffer[:batchSize]
	// Create a copy to avoid buffer reuse issues
	batchCopy := make([]byte, batchSize)
	copy(batchCopy, batch)
	tw.charBuffer = tw.charBuffer[batchSize:]

	// Check if there's more content in the buffer
	hasMoreContent := len(tw.charBuffer) > 0
	tw.bufferMutex.Unlock()

	// Write the batch to output
	tw.outWriter.Write(batchCopy)

	// Calculate appropriate sleep time
	var sleepTime time.Duration
	if nlIndex >= 0 && nlIndex > 0 {
		// Apply newline delay factor for text that includes a newline
		charCount := float64(nlIndex)
		sleepTime = time.Duration(charCount * float64(tw.delay))
	} else {
		sleepTime = tw.delay * time.Duration(batchSize)
	}

	// Sleep after writing to create the animation effect
	if sleepTime > 0 {
		time.Sleep(sleepTime)
	}

	// If there's more content and no signal is pending, signal content availability
	if hasMoreContent {
		select {
		case tw.contentAvailable <- struct{}{}:
			// Signal sent
		default:
			// Channel already has a signal, no need to send another
		}
	}

	return true
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
