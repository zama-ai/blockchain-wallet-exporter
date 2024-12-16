TAG?=v0.1.0
DATE?=$(shell date +%Y-%m-%d-%H:%M:%S)
COMMIT?=$(shell git rev-parse HEAD)
PACKAGE?= github.com/zama-ai/blockchain-wallet-exporter/pkg/version
LDFLAGS=-ldflags "-s -w -X $(PACKAGE).Version=$(TAG) -X $(PACKAGE).Commit=$(COMMIT) -X $(PACKAGE).Date=$(DATE)"
REGISTRY?=ghcr.io/zama-ai/blockchain-wallet-exporter
BIN?=blockchain-wallet-exporter
ARCH?=amd64
OS?=linux
PLATFORM?=$(OS)/$(ARCH)
GO_TEST_BIN ?= go test
GO_TEST_COVERPROFILE?=export RUNNING_MOD="test"; $(GO_TEST_BIN) -v 2>&1 -coverprofile
GO_MOD_TIDY?=go mod tidy
GO_TOOL_COVER?=go tool cover
GO_LINT?=golangci-lint
GO_LINT_CONFIG?=.golangci.yml
ENV?=local


.DEFAULT_GOAL := help

.PHONY: build test docker-build help

build:
	GO_ENABLED=0 go build $(LDFLAGS) -o $(BIN) ./cmd/main.go

docker-build:
	docker build --build-arg ARCH=$(ARCH) --build-arg OS=$(OS) -t $(REGISTRY)/blockchain-wallet-exporter:latest .

test-lint:
	${GO_LINT} run -c ${GO_LINT_CONFIG}

test:
ifeq ($(ENV), local)
	${GO_MOD_TIDY}
	${GO_TEST_COVERPROFILE} coverage.txt  ./pkg/collector/... ./pkg/config/... ./pkg/currency/...
	${GO_TOOL_COVER} -func coverage.txt
else
	$(error ENV ${ENV} not found)	
endif

run:
	docker-compose build --build-arg ARCH=$(ARCH) --build-arg OS=$(OS)
	docker-compose up

run-macos:
	ARCH=arm64 OS=linux $(MAKE) run

destroy:
	docker-compose down

help:
	@echo "Targets:"
	@echo "  build: Build the application"
	@echo "  docker-build: Build the docker image"
	@echo "  test-lint: Run linter"
	@echo "  test: Run tests and generate a coverage report"
	@echo "  run: Run the application in docker-compose"
	@echo "  run-macos: Run the application in docker-compose for macOS"
	@echo "  destroy: Stop the application in docker-compose"
	@echo "  help: Show this help message"
