# reddit-archiver

[![CI](https://github.com/SuperOoge/reddit-archiver/actions/workflows/ci.yml/badge.svg)](https://github.com/SuperOoge/reddit-archiver/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/SuperOoge/reddit-archiver)](https://goreportcard.com/report/github.com/SuperOoge/reddit-archiver)
[![Go Reference](https://pkg.go.dev/badge/github.com/SuperOoge/reddit-archiver.svg)](https://pkg.go.dev/github.com/SuperOoge/reddit-archiver)
[![Go version](https://img.shields.io/github/go-mod/go-version/SuperOoge/reddit-archiver)](go.mod)
[![License: MIT](https://img.shields.io/github/license/SuperOoge/reddit-archiver)](LICENSE)

A command-line tool for scraping media links from Reddit, storing them in a local
SQLite database, downloading the files, and browsing what you've collected from a
terminal UI.

## Status

Early scaffold. The Reddit scraper, database layer, and TUI are functional for a
single subreddit's "new" listing; downloading, pagination past one run, and richer
TUI screens (scrape history, search, filtering) are open for contribution — see
[CONTRIBUTING.md](CONTRIBUTING.md) and the open issues.

## Responsible use

This tool talks to Reddit's public JSON endpoints (`/r/<subreddit>/new.json`).
Respect Reddit's [API terms](https://www.redditinc.com/policies/data-api-terms) and
rate limits — the default `Client` honors the `-subreddit`/`-pages` scope you give
it and doesn't attempt to evade rate limiting or authentication requirements.
Scraped content stays subject to whatever license/rights its original poster has
over it; this tool is for personal archiving, not redistribution.

## Requirements

- Go 1.26+
- A C compiler (the SQLite driver uses cgo) — gcc or clang, whatever your platform
  ships

## Getting started

```sh
cp config.example.json config.json   # edit subreddits, download_path, etc.
cp .env.example .env                 # optional: REDDIT_COOKIE, REDDIT_USER_AGENT

go build -o bin/scraper ./cmd/scraper
go build -o bin/tui ./cmd/tui

./bin/scraper -subreddit earthporn -pages 1
./bin/tui
```

Or with `make`:

```sh
make build
make run-scraper ARGS="-subreddit earthporn -pages 1"
make run-tui
```

## Project layout

- `cmd/scraper` — CLI entry point: fetches a subreddit's listing and stores it
- `cmd/tui` — terminal UI for browsing scraped posts
- `internal/reddit` — Reddit JSON listing client
- `internal/downloader` — content-addressed file downloader (SHA256-named, dedups
  identical content automatically)
- `internal/db` — SQLite connection and schema migration (GORM)
- `internal/models` — database schema (`Post`, `ScrapeRun`)
- `internal/config` — JSON config file + `.env` loader

## Development

```sh
make build   # build cmd/scraper and cmd/tui into ./bin
make test    # go test -race ./...
make lint    # go vet + golangci-lint
make fmt     # gofmt + goimports
```

Run `make lint` and `make test` before opening a pull request.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Participation is governed by the
[Code of Conduct](CODE_OF_CONDUCT.md). Found a security issue? See
[SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
