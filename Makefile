BIN_NAME := kubectl-quackops
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
INSTALL_DIR := /usr/local/bin

CHECK_ARGS := -v
export DEBUG := 1

all: build run

build:
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -v -o $(BIN_NAME) cmd/**.go

run: build
	./$(BIN_NAME)

# Test targets
test:
	go test -v ./...

test-unit:
	go test -v ./pkg/...

test-chat:
	go test -v ./pkg/llm/ -run "TestChat|TestMockLLM"

test-interactive:
	go test -v ./pkg/cmd/ -run "TestProcess"

test-integration:
	go test -v ./pkg/llm/ -run "TestProviderIntegration"

test-e2e:
	go test -v ./tests/

test-mock:
	go test -v ./pkg/llm/ -run "TestMock"

test-streaming:
	go test -v ./pkg/llm/ -run "TestStreamingBehavior|TestE2E_StreamingVsNonStreaming"

test-verbose:
	go test -v ./pkg/cmd/ -run "TestVerboseMode|TestProcessUserPrompt_VerboseMode"

test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-benchmark:
	go test -v -run=^$$ -bench=. ./...

test-race:
	go test -v -race ./...

test-short:
	go test -v -short ./...

test-timeout:
	go test -v -timeout=30s ./...

# Clean test artifacts
clean-test:
	rm -f coverage.out coverage.html
	rm -f *.test

install: build
	install -m 755 $(BIN_NAME) $(INSTALL_DIR)/$(BIN_NAME)
	@echo "Installed $(BIN_NAME) to $(INSTALL_DIR)"

check-logs: build
	{ echo 'analyze pods logs'; cat; } | kubectl quackops $(CHECK_ARGS)

check-perf: build
	{ echo 'analyze performance'; cat; } | kubectl quackops $(CHECK_ARGS)

check-pods: build
	{ echo 'find pod issues'; cat; } | kubectl quackops $(CHECK_ARGS)

check-deployments: build
	{ echo 'find deployments issues'; cat; } | kubectl quackops $(CHECK_ARGS)

check-ingress: build
	{ echo 'check ingress'; cat; } | kubectl quackops $(CHECK_ARGS)

check-storage: build
	{ echo 'check storage issues'; cat; } | kubectl quackops $(CHECK_ARGS)

check-network: build
	{ echo 'check network issues'; cat; } | kubectl quackops $(CHECK_ARGS)

check-cluster: build
	{ echo 'check cluster issues'; cat; } | kubectl quackops $(CHECK_ARGS)

# Help target
help:
	@echo "Available targets:"
	@echo ""
	@echo "Build & Run:"
	@echo "  build              Build the kubectl-quackops binary"
	@echo "  run                Build and run the application"
	@echo "  install            Install binary to system PATH"
	@echo ""
	@echo "Testing:"
	@echo "  test               Run all tests"
	@echo "  test-unit          Run unit tests for packages"
	@echo "  test-chat          Run chat functionality tests"
	@echo "  test-interactive   Run interactive mode tests"
	@echo "  test-integration   Run provider integration tests"
	@echo "  test-e2e           Run end-to-end scenario tests"
	@echo "  test-mock          Run mock infrastructure tests"
	@echo "  test-streaming     Run streaming behavior tests"
	@echo "  test-verbose       Run verbose mode tests"
	@echo "  test-coverage      Generate test coverage report"
	@echo "  test-benchmark     Run benchmark tests"
	@echo "  test-race          Run tests with race condition detection"
	@echo "  test-short         Run only short tests"
	@echo "  test-timeout       Run tests with 30s timeout"
	@echo "  clean-test         Clean test artifacts"
	@echo ""
	@echo "Cluster Checks (requires kubectl connection):"
	@echo "  check-logs         Analyze pod logs"
	@echo "  check-perf         Analyze performance"
	@echo "  check-pods         Find pod issues"
	@echo "  check-deployments  Find deployment issues"
	@echo "  check-ingress      Check ingress configuration"
	@echo "  check-storage      Check storage issues"
	@echo "  check-network      Check network issues"
	@echo "  check-cluster      Check general cluster issues"
	@echo ""
	@echo "Example usage:"
	@echo "  make test-chat     # Test only chat functionality"
	@echo "  make test-e2e      # Run end-to-end tests"
	@echo "  make test-coverage # Generate coverage report"

.PHONY: help build run test test-unit test-chat test-interactive test-integration test-e2e test-mock test-streaming test-verbose test-coverage test-benchmark test-race test-short test-timeout clean-test install check-logs check-perf check-pods check-deployments check-ingress check-storage check-network check-cluster
