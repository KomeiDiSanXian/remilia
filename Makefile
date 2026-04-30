.PHONY: all build test lint clean tidy fmt vet docker-build help

APP_NAME    ?= remilia
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -X github.com/KomeiDiSanXian/remilia.Version=$(VERSION) -X main.commit=$(GIT_COMMIT) -X main.date=$(BUILD_TIME)

GO          ?= go
GO_BUILD    := $(GO) build -ldflags="$(LDFLAGS)"

help:
	@echo "Usage:"
	@echo "  make build     - Build the project"
	@echo "  make test      - Run all tests with race detection"
	@echo "  make lint      - Run golangci-lint"
	@echo "  make clean     - Remove build artifacts"
	@echo "  make tidy      - Run go mod tidy"
	@echo "  make fmt       - Format all Go source files"
	@echo "  make vet       - Run go vet"

all: tidy fmt vet test build

build:
	$(GO_BUILD) -o bin/$(APP_NAME) .

test:
	$(GO) test -count=1 -race -shuffle=on -timeout 180s ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
	rm -rf dist/

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

docker-build:
	docker build -t $(APP_NAME):$(VERSION) .
