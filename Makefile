.PHONY: all build test lint fmt fmt-check clean

BINARY_NAME=mallow
MAIN_PATH=./mallow.go

all: fmt-check lint test build

build:
	go build -v -o $(BINARY_NAME) $(MAIN_PATH)

test:
	go test -v ./...

test-e2e:
	go test -v -tags e2e ./...

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

fmt-check:
	@if [ "$$(gofmt -l . | wc -l)" -gt 0 ]; then \
		echo "The following files are not formatted:"; \
		gofmt -l .; \
		exit 1; \
	fi

clean:
	go clean
	rm -f $(BINARY_NAME)
