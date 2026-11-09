# Deferred items (phase 03)

## From 03-04 (found, not caused by it)

- RESOLVED in 03-05: `internal/web` `TestWSPingReaps` was flaky (`stalled client read = failed to get reader: failed to read
  frame header: EOF, want a going-away close`). Cause: a test race, not a bug in `ws.go`. The test jumped the manual clock
  while a write was still in flight, and coder/websocket closes the TCP connection hard when the context of an in-flight
  write expires (`setupWriteTimeout` -> `c.close()`), so the server's going-away close frame was never sent. Fixed in the test
  by never advancing time during a write; `-race -count=100` passes. See the 03-05 SUMMARY.
