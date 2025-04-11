package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/formatter"
)

// simulateChunkedResponse simulates a typical streaming LLM response
func simulateChunkedResponse(mdExample string, chunkSize int, delayMs int) <-chan []byte {
	chunks := make(chan []byte)

	go func() {
		defer close(chunks)

		// Break the response into chunks
		runes := []rune(mdExample)
		for i := 0; i < len(runes); i += chunkSize {
			end := i + chunkSize
			if end > len(runes) {
				end = len(runes)
			}

			// Send this chunk
			chunks <- []byte(string(runes[i:end]))

			// Simulate delay between chunks
			if delayMs > 0 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
		}
	}()

	return chunks
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--no-color" {
		fmt.Println("Running without color formatting")
		runExample(true)
	} else if len(os.Args) > 1 && os.Args[1] == "--interactive" {
		runInteractive()
	} else {
		fmt.Println("Running with color formatting")
		runExample(false)
	}
}

func runExample(disableColor bool) {
	// Create sample Markdown content
	mdExample := `# Kubernetes Cluster Analysis

## Current State
The cluster is running **version 1.29.3** with the following components:
- Control plane: 3 nodes
- Worker nodes: 5 nodes
- System pods: *Healthy*

### Resource Utilization
Current resource utilization:
1. CPU: 45% average
2. Memory: 62% average
3. Storage: 38% utilized

> Note: There are several pods in the kube-system namespace with high CPU usage.

Here's a sample manifest to debug:
` + "```yaml" + `
apiVersion: v1
kind: Pod
metadata:
  name: debug-pod
  namespace: default
spec:
  containers:
  - name: debug
    image: busybox
    command: ["sleep", "3600"]
` + "```" + `

For more information, see [Kubernetes Documentation](https://kubernetes.io/docs/).
`

	// Set up a writer with color formatting
	var writerOptions []formatter.FormatterOption
	if disableColor {
		writerOptions = append(writerOptions, formatter.WithColorDisabled())
	}

	// Create a streaming writer to stdout
	writer := formatter.NewStreamingWriter(os.Stdout, writerOptions...)
	defer writer.Close()

	// Simulate chunks (40 characters per chunk with 100ms delay)
	chunks := simulateChunkedResponse(mdExample, 40, 100)

	fmt.Println("Streaming formatted Markdown:")
	fmt.Println("-----------------------------")

	// Process each chunk
	for chunk := range chunks {
		_, err := writer.Write(chunk)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing chunk: %v\n", err)
			return
		}
	}

	fmt.Println("\n-----------------------------")
	fmt.Println("Streaming complete")
}

func runInteractive() {
	fmt.Println("Interactive Markdown Formatter")
	fmt.Println("Type or paste Markdown text (Ctrl+D to end):")
	fmt.Println("-----------------------------")

	// Create a streaming writer to stdout
	writer := formatter.NewStreamingWriter(os.Stdout)
	defer writer.Close()

	// Read from stdin in chunks
	reader := bufio.NewReader(os.Stdin)
	buf := make([]byte, 64) // Read in 64-byte chunks

	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			return
		}

		// Process the chunk
		if n > 0 {
			_, err = writer.Write(buf[:n])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error writing chunk: %v\n", err)
				return
			}
		}
	}

	fmt.Println("\n-----------------------------")
	fmt.Println("Input processing complete")
}
