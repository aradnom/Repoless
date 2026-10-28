BIN     := repoless
VERSION := $(shell grep -m1 'const version' main.go | cut -d'"' -f2)
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

.PHONY: build install release clean test

build:
	go build -trimpath -ldflags="-s -w" -o $(BIN) .

install:
	go install -trimpath -ldflags="-s -w" .

release:
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		out=dist/$(BIN)-$(VERSION)-$$os-$$arch; \
		echo "building $$out"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $$out . || exit 1; \
	done

test:
	go vet ./... && go test ./...

clean:
	rm -rf $(BIN) dist
