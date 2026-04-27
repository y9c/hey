#
# Makefile
# Ye Chang, 2020-11-05 15:31
#

ifeq ($(OS),Windows_NT)     # is Windows_NT on XP, 2000, 7, Vista, 10...
    detected_OS := Windows
else
    detected_OS := $(shell uname)
endif

BUILD_FLAGS='-s -w'
GO_BUILD_ENV=CGO_ENABLED=0
GO_BUILD_TAGS=netgo,osusergo

all: build-go-binary
release: build-release-binary

.PHONY: linux
linux:
	@echo "building binary for linux 64bit..."
	@$(GO_BUILD_ENV) GOOS=linux GOARCH=amd64 go build -tags $(GO_BUILD_TAGS) -ldflags $(BUILD_FLAGS) -o ./hey && upx ./hey

.PHONY: build-go-binary
build-go-binary:
	@echo "building binary..."
	@$(GO_BUILD_ENV) go build -tags $(GO_BUILD_TAGS) -ldflags $(BUILD_FLAGS) -o ./hey

.PHONY: build-release-binary
build-release-binary:
	@echo "building release binary..."
	@$(GO_BUILD_ENV) go build -tags $(GO_BUILD_TAGS) -ldflags $(BUILD_FLAGS) -o ./hey && upx --ultra-brute --force-macos ./hey
