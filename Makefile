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

test:
	go test -v ./...

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
