MODULE  := github.com/pratikbhadane24/multiclaude
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(MODULE)/internal/cli.Version=$(VERSION)

.PHONY: build install test vet fmt-check check dist clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/ccm ./cmd/ccm
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/ccm-shim ./cmd/ccm-shim

# Build, then install the shim and link ccm into ~/.local/bin.
install: build
	./bin/ccm install

test:
	go test ./... -cover

vet:
	go vet ./...

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

# Everything CI runs.
check: fmt-check vet test

# Release archives for every supported platform, plus checksums, in dist/.
dist:
	VERSION=$(VERSION) ./scripts/dist.sh

clean:
	rm -rf bin dist coverage.out
