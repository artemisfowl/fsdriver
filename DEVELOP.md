## How Version injection works in this project

During `make` or `make all`, the Makefile automatically extracts three variables from the repository:

- VERSION: The latest Git tag (e.g., v1.0.0). If no tag exists, it falls back to the short commit hash or dev.
- COMMIT: The current short Git commit hash (e.g., ef60be1).
- DATE: The UTC timestamp at the time of compilation (ISO 8601 format).

These values are passed to Go using the -X flag:

```bash
go build -ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.Date=$(DATE)" ./cmd/fsdriver
```
