#
# Makefile
# Ye Chang, 2020-11-05 15:31
#

all: build-go-binary

.PHONY: build-go-binary
build-go-binary:
	@echo "building release binary..."
	@#go build -ldflags="-s -w" -o ./hey && upx --ultra-brute ./hey
	@go build -ldflags '-s -w -linkmode external -extldflags "-fno-PIC -static"' -o ./hey && upx ./hey
