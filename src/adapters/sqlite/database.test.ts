import { afterEach, describe, expect, test } from "bun:test"
import { SqlClient } from "@effect/sql"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { sqliteDatabaseLayer } from "./database.ts"

const temporaryDirectories: Array<string> = []

const temporaryDatabasePath = () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-database-"))
  temporaryDirectories.push(directory)
  return join(directory, "latasya.db")
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("sqliteDatabaseLayer", () => {
  test("configures the runtime connection like the Go database", async () => {
    const databasePath = temporaryDatabasePath()
    const inspect = Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      const journalMode = yield* sql.unsafe<{ readonly journal_mode: string }>(
        "PRAGMA journal_mode"
      )
      const foreignKeys = yield* sql.unsafe<{ readonly foreign_keys: number }>(
        "PRAGMA foreign_keys"
      )
      const busyTimeout = yield* sql.unsafe<{ readonly timeout: number }>(
        "PRAGMA busy_timeout"
      )
      const synchronous = yield* sql.unsafe<{ readonly synchronous: number }>(
        "PRAGMA synchronous"
      )

      return {
        journalMode: journalMode[0]?.journal_mode,
        foreignKeys: foreignKeys[0]?.foreign_keys,
        busyTimeout: busyTimeout[0]?.timeout,
        synchronous: synchronous[0]?.synchronous
      }
    })

    await expect(
      Effect.runPromise(
        inspect.pipe(Effect.provide(sqliteDatabaseLayer(databasePath)))
      )
    ).resolves.toEqual({
      journalMode: "wal",
      foreignKeys: 1,
      busyTimeout: 5000,
      synchronous: 1
    })
  })
})
