.PHONY: help build run fmt vet test check

help:
	@printf '%s\n' 'Available targets:'
	@printf '%s\n' '  build  Build the Klipbord binary'
	@printf '%s\n' '  run    Run Klipbord locally'
	@printf '%s\n' '  fmt    Format Go source files'
	@printf '%s\n' '  vet    Run Go static analysis'
	@printf '%s\n' '  test   Run the test suite'
	@printf '%s\n' '  check  Run formatting, static analysis, and race-enabled tests'

build:
	go build -o klipbord .

run:
	go run .

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

check:
	@unformatted="$$(gofmt -l $$(find . -type f -name '*.go' -not -path './.git/*' -not -path './.gocache/*'))"; \
	if [ -n "$$unformatted" ]; then \
		echo "Run 'make fmt' to format:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	go vet ./...
	go test -race ./...
