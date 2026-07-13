.PHONY: build run test

build:
	go build -o klipbord .

run:
	go run .

test:
	go test ./...
