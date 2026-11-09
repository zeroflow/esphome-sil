# Deferred items (phase 03)

## From 03-04 (found, not caused by it)

- `internal/web` `TestWSPingReaps` is flaky: with `go test -race -count=15 -run TestWSPingReaps ./internal/web/` it fails
  on one of the first runs with `ws_test.go:179: stalled client read = failed to get reader: failed to read frame header: EOF,
  want a going-away close`. It fails identically on commit 15d0f94 (before 03-04), and `make check` passes on most runs.
  Owner: 03-03 (ping reaping); the test expects a close frame but sometimes sees the connection closed first.
