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

build-release:
	go test -v ./...
	@last_tag=$$(git describe --tags --abbrev=0 2>/dev/null || true); \
	if [ -z "$$last_tag" ]; then \
		start_ref=$$(git rev-list --max-parents=0 --reverse HEAD | head -n1); \
		range="$$start_ref..HEAD"; \
		since="$$start_ref"; \
	else \
		range="$$last_tag..HEAD"; \
		since="$$last_tag"; \
	fi; \
	changes=$$(git log --no-merges --pretty=format:"- %s" $$range); \
	if [ -z "$$changes" ]; then \
		echo "No new commits since $$since; changelog unchanged."; \
		exit 0; \
	fi; \
	today=$$(date +%Y-%m-%d); \
	body=$$(awk 'BEGIN{skip=0} { if ($$0 ~ /^### Unreleased/) {skip=1; next} if (skip==1 && $$0 ~ /^### v[0-9]+\./) {skip=0} if (skip==0) print $$0 }' CHANGELOG.md); \
	tmp=$$(mktemp); \
	{ \
		echo "## Changelog"; \
		echo ""; \
		echo "### Unreleased — $$today"; \
		echo ""; \
		printf "%s\n\n" "$$changes"; \
	} > $$tmp; \
	printf "%s\n" "$$body" | tail -n +2 >> $$tmp; \
	mv $$tmp CHANGELOG.md; \
	git add CHANGELOG.md; \
	echo "Updated CHANGELOG.md with commits since $$since and staged changes."

build-publish:
	@read -p "Version (e.g., v1.9.0): " VERSION; \
	if [ -z "$$VERSION" ]; then echo "Version is required"; exit 1; fi; \
	if git rev-parse "$$VERSION" >/dev/null 2>&1; then echo "Tag $$VERSION already exists"; exit 1; fi; \
	if ! grep -q "^### Unreleased" CHANGELOG.md; then echo "No Unreleased section to publish. Run make build:release first."; exit 1; fi; \
	today=$$(date +%Y-%m-%d); \
	tmp=$$(mktemp); \
	awk -v ver="$$VERSION" -v today="$$today" 'BEGIN{done=0} { if(done==0 && $$0 ~ /^### Unreleased/) {print "### " ver " — " today; done=1; next} print }' CHANGELOG.md > $$tmp; \
	mv $$tmp CHANGELOG.md; \
	git add CHANGELOG.md; \
	git commit -m "Release $$VERSION"; \
	git tag -a "$$VERSION" -m "Release $$VERSION"; \
	echo "Created commit and tag $$VERSION (not pushed)."

build-push:
	git push
	git push --tags

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

.PHONY: build build-benchmark run test test-unit test-chat test-interactive test-integration test-e2e test-mock test-streaming test-verbose test-coverage test-benchmark test-benchmark-unit test-benchmark-scenarios test-benchmark-metrics test-benchmark-reports test-benchmark-integration test-race test-short test-timeout clean-test build-release build-publish build-push install benchmark-install benchmark-simple benchmark-openai-models benchmark-comparison benchmark-comprehensive benchmark-dry-run benchmark-scenarios benchmark-help benchmark-version benchmark-export-config check-logs check-perf check-pods check-deployments check-ingress check-storage check-network check-cluster
