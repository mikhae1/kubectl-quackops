BIN_NAME := kubectl-quackops
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

CHECK_ARGS := -v -x 4000
export DEBUG := 1

all: build run

build:
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -v -o $(BIN_NAME) cmd/**.go

run: build
	./$(BIN_NAME)

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
	{ echo 'check storage'; cat; } | kubectl quackops $(CHECK_ARGS)

check-network: build
	{ echo 'check network'; cat; } | kubectl quackops $(CHECK_ARGS)
