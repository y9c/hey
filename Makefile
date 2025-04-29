#
# Makefile
# Ye Chang, 2020-11-05 15:31
#

ifeq ($(OS),Windows_NT)     # is Windows_NT on XP, 2000, 7, Vista, 10...
    detected_OS := Windows
else
    detected_OS := $(shell uname)
endif

ifeq ($(detected_OS),Linux)        # Linux
	BUILD_FLAGS='-s -w -linkmode external -extldflags "-fno-PIC -static"' 
else
	BUILD_FLAGS='-s -w'
endif

all: build-go-binary
release: build-release-binary

.PHONY: linux
linux:
	@echo "building binary for linux 64bit..."
	@GOOS=linux GOARCH=amd64 go build -ldflags $(BUILD_FLAGS) -o ./hey && upx ./hey

.PHONY: build-go-binary
build-go-binary:
	@echo "building binary..."
	@go build -ldflags $(BUILD_FLAGS) -o ./hey

.PHONY: build-release-binary
build-release-binary:
	@echo "building release binary..."
	@go build -ldflags $(BUILD_FLAGS) -o ./hey && upx --ultra-brute --force-macos ./hey
