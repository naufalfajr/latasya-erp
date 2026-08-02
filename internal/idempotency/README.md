# Idempotency module

`idempotency` owns request hashing and the SQLite-backed 24-hour replay cache
used by JSON mutation endpoints. The API middleware owns request/response
capture and per-process locking; this module owns durable lookup, save, and
expiry cleanup semantics.

The deployed ERP runs one application process. Replay records survive process
restarts, but overlapping application processes are not supported: at-most-once
execution is enforced by the in-process keyed lock before the durable response
is written.
