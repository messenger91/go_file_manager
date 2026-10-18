BINARY  := go_file_manager
BIN_DIR := bin
GOFLAGS := -trimpath
LDFLAGS := -s -w

.PHONY: all build build-linux build-linux-amd64 build-linux-arm64 run test fmt vet tidy clean

all: build

## build: compile for the host platform
build:
	@mkdir -p $(BIN_DIR)
	go build $(GOFLAGS) -o $(BIN_DIR)/$(BINARY) .

## build-linux: cross-compile static Linux binaries (amd64 + arm64)
build-linux: build-linux-amd64 build-linux-arm64

build-linux-amd64:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-linux-amd64 .

build-linux-arm64:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-linux-arm64 .

## run: start the server using .env
run:
	go run .

## test: run all tests
test:
	go test ./...

## fmt: format all Go source
fmt:
	go fmt ./...

## vet: run static analysis
vet:
	go vet ./...

## tidy: sync go.mod/go.sum
tidy:
	go mod tidy

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR)
