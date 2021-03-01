.PHONY: hey
hey:
	@go build
	@upx -9 $@

