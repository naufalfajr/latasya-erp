# Audit Module

Audit records business mutations and security events with actor, token, request, client, target, result, and metadata fields.

## Delivery contract

Audit delivery is best-effort and is not part of the business transaction. An audit write failure is logged but never changes the HTTP response or rolls back a successful mutation.

The implementation writes synchronously. `BenchmarkLogFileSQLite` measures a representative event against a file-backed SQLite database. On the 2026-08-02 architecture baseline it averaged 45–46 microseconds per event on an Apple M4, so an asynchronous queue was rejected as unnecessary complexity.

If a queue is introduced, it must be bounded, non-blocking for callers, observable when records are dropped, and drained with a timeout during graceful shutdown. These semantics permit record loss during saturation or process failure.
