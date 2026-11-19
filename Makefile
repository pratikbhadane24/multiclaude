.PHONY: test vet build

test:
	go test ./... -cover

vet:
	go vet ./...

build:
	go build -o bin/ccm ./cmd/ccm
	go build -o bin/ccm-shim ./cmd/ccm-shim
