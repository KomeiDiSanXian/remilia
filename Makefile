.PHONY: all build test lint clean tidy fmt vet docker-build help

APP_NAME    ?= remilia
VERSION     ?= $(shell git describe --tags --always --dirty 2>NUL || echo "dev")
GIT_COMMIT  ?= $(shell git rev-parse --short HEAD 2>NUL || echo "unknown")
BUILD_TIME  ?= $(shell powershell -Command "Get-Date -Format 'yyyy-MM-ddTHH:mm:ssZ'")
LDFLAGS     := -X github.com/KomeiDiSanXian/remilia.Version=$(VERSION) -X main.commit=$(GIT_COMMIT) -X main.date=$(BUILD_TIME)

CGO_ENABLED ?= 0
GO          ?= go
ifeq ($(OS),Windows_NT)
BIN_SUFFIX  := .exe
GO_BUILD    := set CGO_ENABLED=$(CGO_ENABLED) && $(GO) build -ldflags="$(LDFLAGS)"
else
BIN_SUFFIX  :=
GO_BUILD    := CGO_ENABLED=$(CGO_ENABLED) $(GO) build -ldflags="$(LDFLAGS)"
endif

help:
	@echo "Usage:"
	@echo "  make build            - Build cmd/bot binary (with embedded dashboard)"
	@echo "  make test             - Run all tests with race detection"
	@echo "  make lint             - Run golangci-lint"
	@echo "  make clean            - Remove build artifacts"
	@echo "  make tidy             - Run go mod tidy"
	@echo "  make fmt              - Format all Go source files"
	@echo "  make vet              - Run go vet"
	@echo "  make docker           - Build Docker image"
	@echo "  make dashboard-build  - Build Web Dashboard SPA"
	@echo "  make desktop-front	 - Build Desktop frontend only"
	@echo "  make desktop-dev      - Start Tauri desktop in dev mode"
	@echo "  make desktop-build    - Build Tauri desktop release"
	@echo "  make build-wasm-test  - Build WASM test plugins (requires TinyGo + Go 1.23)"
	@echo "  make build-wasm-showcase - Build showcase WASM demo plugin"
	@echo ""
	@echo "Prerequisites for desktop:"
	@echo "  - Rust toolchain (rustup)"
	@echo "  - MSVC Build Tools (Windows) or GCC + system libs (Linux/macOS)"
	@echo "  - See desktop/README.md for details"

all: tidy fmt vet test build

build: dashboard-build
	$(GO_BUILD) -o bin/$(APP_NAME)$(BIN_SUFFIX) ./cmd/bot

# 构建 Web Dashboard 并复制到嵌入目录
dashboard-build:
ifeq ($(OS),Windows_NT)
	cd dashboard && npm ci && npm run build
	if exist cmd\bot\dashboarddist\ rmdir /s /q cmd\bot\dashboarddist
	mkdir cmd\bot\dashboarddist
	xcopy /E /I /Y dashboard\dist\* cmd\bot\dashboarddist
else
	cd dashboard && npm ci && npm run build
	cp -r dashboard/dist/* cmd/bot/dashboarddist/
endif

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

# -p 4 限制并行编译的 test binary 数量，避免端口/CPU 争用导致偶发超时
test:
	$(GO) test -count=1 -race -shuffle=on -p 4 -timeout 180s ./...

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

# --- Desktop (Tauri) ---

# 构建 Go sidecar 二进制并放入 Tauri 预期位置
# Tauri 要求二进制名为 <name>-<target-triple><ext>
ifeq ($(OS),Windows_NT)
TARGET_TRIPLE := x86_64-pc-windows-msvc
else
TARGET_TRIPLE := $(shell rustc -vV | grep host | awk '{print $$2}')
endif
SIDECAR_NAME := remilia-bot-$(TARGET_TRIPLE)$(BIN_SUFFIX)

desktop-sidecar:
	$(GO_BUILD) -o desktop/src-tauri/binaries/$(SIDECAR_NAME) ./cmd/bot

# 构建桌面端前端（无需 Rust 即可验证）
desktop-front:
	cd desktop && npm ci && npm run build

# 构建 sidecar + 前端（Tauri dev 前需执行一次）
desktop-prepare: desktop-sidecar desktop-front

# 开发模式启动桌面端（需要 MSVC/Linux 原生工具链）
desktop-dev: desktop-front
	cd desktop && npx tauri dev

# 构建桌面端 release（sidecar 会自动被 Tauri 打包）
desktop-build: desktop-sidecar desktop-front
	cd desktop && npx tauri build

docker:
	docker build -t $(APP_NAME):$(VERSION) .
