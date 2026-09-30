.PHONY: build test vet fmt check

build:
	go build -o bin/whr ./cmd/whr

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

check: vet test
