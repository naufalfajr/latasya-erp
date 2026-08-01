import { afterEach, describe, expect, test } from "bun:test"
import { SqlClient } from "@effect/sql"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect, Layer } from "effect"
import { sqliteDatabaseLayer } from "../../adapters/sqlite/database.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { Audit, AuditLive, auditDiff } from "./audit.ts"

const temporaryDirectories: Array<string> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-audit-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const database = sqliteDatabaseLayer(databasePath)
  return Layer.merge(database, AuditLive.pipe(Layer.provide(database)))
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("Audit", () => {
  test("stores actor, request, target, result, and canonical metadata", async () => {
    const layer = await setup()
    const inspect = Effect.gen(function*() {
      const audit = yield* Audit
      yield* audit.log(
        { requestId: "request-1", clientIp: "203.0.113.7" },
        {
          action: "invoice.create",
          actor: { id: 3, username: "alice", tokenId: 9 },
          targetType: "invoice",
          targetId: 42,
          targetLabel: "INV-2026-001",
          metadata: { total: 1_500_000, contact_id: 7 }
        }
      )
      const sql = yield* SqlClient.SqlClient
      return yield* sql<{
        readonly request_id: string
        readonly actor_id: number
        readonly actor_username: string
        readonly actor_token_id: number
        readonly action: string
        readonly target_type: string
        readonly target_id: number
        readonly target_label: string
        readonly result: string
        readonly ip: string
        readonly metadata: string
      }>`
        SELECT
          request_id, actor_id, actor_username, actor_token_id,
          action, target_type, target_id, target_label,
          result, ip, metadata
        FROM audit_log
      `
    })
    const rows = await Effect.runPromise(inspect.pipe(Effect.provide(layer)))

    expect(rows).toHaveLength(1)
    expect(rows[0]).toEqual({
      request_id: "request-1",
      actor_id: 3,
      actor_username: "alice",
      actor_token_id: 9,
      action: "invoice.create",
      target_type: "invoice",
      target_id: 42,
      target_label: "INV-2026-001",
      result: "ok",
      ip: "203.0.113.7",
      metadata: '{"contact_id":7,"total":1500000}'
    })
  })

  test("records failures and swallows insert errors", async () => {
    const layer = await setup()
    const inspect = Effect.gen(function*() {
      const audit = yield* Audit
      yield* audit.log({}, {
        action: "auth.login_failed",
        actor: { username: "bob" },
        error: new Error("bad password")
      })
      const sql = yield* SqlClient.SqlClient
      const rows = yield* sql<{
        readonly result: string
        readonly error_message: string
      }>`
        SELECT result, error_message
        FROM audit_log
      `
      yield* sql`DROP TABLE audit_log`
      yield* audit.log({}, { action: "test.nop" })
      return rows
    })
    const rows = await Effect.runPromise(inspect.pipe(Effect.provide(layer)))

    expect(rows[0]).toEqual({
      result: "fail",
      error_message: "bad password"
    })
  })
})

describe("auditDiff", () => {
  test("includes only allow-listed changed fields", () => {
    expect(auditDiff(
      { name: "Old", status: "draft", password_hash: "secret-a" },
      { name: "New", status: "draft", password_hash: "secret-b" },
      ["name", "status"]
    )).toEqual({
      before: { name: "Old" },
      after: { name: "New" }
    })
  })

  test("returns undefined when nothing changed", () => {
    expect(auditDiff(
      { name: "same", values: [1, 2] },
      { name: "same", values: [1, 2] },
      ["name", "values"]
    )).toBeUndefined()
  })
})
