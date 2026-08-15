.PHONY: build test lint fmt tidy cover run-scraper run-tui vuln

build:
	go build -o bin/scraper ./cmd/scraper
	go build -o bin/tui ./cmd/tui

test:
	go test -race ./...

lint:
	go vet ./...
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w . 2>/dev/null || true

tidy:
	go mod tidy

cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

vuln:
	govulncheck ./...

run-scraper: build
	./bin/scraper $(ARGS)

run-tui: build
	./bin/tui $(ARGS)
