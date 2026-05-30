.PHONY: all build test lint clean tidy fmt vet docker-build help

APP_NAME    ?= remilia
VERSION     ?= $(shell git describe --tags --always --dirty 2>NUL || echo "dev")
GIT_COMMIT  ?= $(shell git rev-parse --short HEAD 2>NUL || echo "unknown")
BUILD_TIME  ?= $(shell powershell -Command "Get-Date -Format 'yyyy-MM-ddTHH:mm:ssZ'")
LDFLAGS     := -X github.com/KomeiDiSanXian/remilia.Version=$(VERSION) -X main.commit=$(GIT_COMMIT) -X main.date=$(BUILD_TIME)

GO          ?= go
ifeq ($(OS),Windows_NT)
BIN_SUFFIX  := .exe
GO_BUILD    := set CGO_ENABLED=1 && $(GO) build -ldflags="$(LDFLAGS)"
else
BIN_SUFFIX  :=
GO_BUILD    := CGO_ENABLED=1 $(GO) build -ldflags="$(LDFLAGS)"
endif

help:
	@echo "Usage:"
	@echo "  make build       - Build cmd/bot binary"
	@echo "  make test        - Run all tests with race detection"
	@echo "  make lint        - Run golangci-lint"
	@echo "  make clean       - Remove build artifacts"
	@echo "  make tidy        - Run go mod tidy"
	@echo "  make fmt         - Format all Go source files"
	@echo "  make vet         - Run go vet"
	@echo "  make docker      - Build Docker image"
	@echo "  make build-wasm-test     - Build WASM test plugins (requires TinyGo + Go 1.23)"
	@echo "  make build-wasm-showcase - Build showcase WASM demo plugin"

all: tidy fmt vet test build

build:
	$(GO_BUILD) -o bin/$(APP_NAME)$(BIN_SUFFIX) ./cmd/bot

build-wasm-test:
ifeq ($(OS),Windows_NT)
	cd plugin/wasm/testdata && set GOOS=wasip1 && set GOARCH=wasm && $(GO) build -o testplugin.wasm .
	cd plugin/wasm/testdata/tinygoplugin && tinygo build -o tinygoplugin.wasm -target=wasi . && copy tinygoplugin.wasm ../
else
	cd plugin/wasm/testdata && GOOS=wasip1 GOARCH=wasm $(GO) build -o testplugin.wasm .
	cd plugin/wasm/testdata/tinygoplugin && tinygo build -o tinygoplugin.wasm -target=wasi . && cp tinygoplugin.wasm ../
endif

build-wasm-showcase:
	cd examples/showcase/wasm && tinygo build -o ../demo.wasm -target=wasi .

test:
	$(GO) test -count=1 -race -shuffle=on -timeout 180s ./...

lint:
	golangci-lint run ./...

clean:
ifeq ($(OS),Windows_NT)
	if exist bin\ rmdir /s /q bin
	if exist dist\ rmdir /s /q dist
else
	rm -rf bin/
	rm -rf dist/
endif

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

docker:
	docker build -t $(APP_NAME):$(VERSION) .
