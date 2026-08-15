# Contributing

Thanks for considering a contribution. This project is a young scaffold, so there's
a lot of room for meaningfully-sized pull requests.

## Setup

```sh
cp config.example.json config.json
cp .env.example .env
go build ./...
go test -race ./...
```

## Before opening a pull request

```sh
make fmt
make lint
make test
```

CI runs the same checks (build, vet, test with race detector, `gofmt -l`,
golangci-lint, and a `go mod tidy` diff check) against every pull request — a green
run is required before merge.

## Guidelines

- Keep pull requests focused: one feature or fix per PR is easier to review than a
  bundle of unrelated changes.
- Add tests for new behavior. Table-driven tests are preferred for anything with
  more than two or three cases — see `internal/reddit/reddit_test.go` for the
  pattern used throughout.
- Prefer `httptest.Server` over hitting real Reddit endpoints in tests, so the test
  suite runs offline and deterministically.
- Follow [Google's Go Style Guide](https://google.github.io/styleguide/go/guide):
  no `Get` prefix on accessors, no type-suffixed names, initialisms stay all-caps
  (`URL`, `ID`, `HTTP`).
- New shared code should live in a domain-specific package (`internal/<thing>`),
  not a general-purpose `utils` package.
- Every `cmd/*/main.go` should wrap `os.Exit(run())` — returning from `main`
  directly exits 0 and silently swallows errors.

## Ideas for a first contribution

- `internal/reddit`: pagination helper that drives `Listing` across multiple pages
  automatically, respecting Reddit's rate-limit headers
- `internal/downloader`: wire the downloader into `cmd/scraper` so posts are
  fetched to disk, not just recorded in the database
- `cmd/tui`: a scrape-history screen backed by the `ScrapeRun` table
- `cmd/tui`: search/filter posts by subreddit or title substring
- Perceptual-hash or byte-identical duplicate detection across downloaded files

If you're planning something larger, open an issue first so the approach can be
discussed before you invest the time.

## Reporting bugs / requesting features

Use the issue templates under `.github/ISSUE_TEMPLATE/`.
