GO ?= go
GOEXPERIMENT ?= jsonv2
VERSION ?= dev

.PHONY: test build

test:
	GOEXPERIMENT=$(GOEXPERIMENT) $(GO) test ./...

build:
	mkdir -p build_assets
	GOEXPERIMENT=$(GOEXPERIMENT) $(GO) build -v -o build_assets/v2node -trimpath -ldflags "-X 'github.com/keli-123456/kelinode/cmd.version=$(VERSION)' -s -w -buildid="
