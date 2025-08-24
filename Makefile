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

clean-test:
	rm -f coverage.out coverage.html
	rm -f *.test

build-benchmark:
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -v -o kubectl-quackops-benchmark cmd/benchmark/main.go

benchmark-openrouter-free: build-benchmark
	./kubectl-quackops-benchmark --models=openai/z-ai/glm-4.5-air:free,openai/moonshotai/kimi-k2:free --complexity=simple --iterations=3

benchmark-scenarios:
	./kubectl-quackops-benchmark --list-scenarios

benchmark-help:
	./kubectl-quackops-benchmark --help

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

.PHONY: build build-benchmark run test test-unit test-chat test-interactive test-integration test-e2e test-mock test-streaming test-verbose test-coverage test-benchmark test-benchmark-unit test-benchmark-scenarios test-benchmark-metrics test-benchmark-reports test-benchmark-integration test-race test-short test-timeout clean-test install benchmark-install benchmark-simple benchmark-openai-models benchmark-comparison benchmark-comprehensive benchmark-dry-run benchmark-scenarios benchmark-help benchmark-version benchmark-export-config check-logs check-perf check-pods check-deployments check-ingress check-storage check-network check-cluster
