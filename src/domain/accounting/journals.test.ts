import { afterEach, describe, expect, test } from "bun:test"
import { SqlClient } from "@effect/sql"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect, Layer } from "effect"
import { sqliteDatabaseLayer } from "../../adapters/sqlite/database.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { PasswordHasherLive } from "../auth/password.ts"
import {
  JournalConflict,
  Journals,
  JournalsLive,
  validateJournal
} from "./journals.ts"

const temporaryDirectories: Array<string> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-journals-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const base = Layer.merge(
    sqliteDatabaseLayer(databasePath),
    PasswordHasherLive
  )
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(base)))
  return Layer.merge(base, JournalsLive.pipe(Layer.provide(base)))
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("Journals", () => {
  test("atomically creates, reads, lists, updates, and deletes manual entries", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const accounts = yield* sql<{ readonly id: number }>`
          SELECT id FROM accounts ORDER BY id LIMIT 2
        `
        const first = accounts[0]?.id ?? 0
        const second = accounts[1]?.id ?? 0
        const journals = yield* Journals
        const created = yield* journals.create({
          entryDate: "2026-05-10",
          description: "Test entry",
          sourceType: "manual",
          isPosted: true,
          createdBy: 1,
          lines: [
            { accountId: first, debit: 100_000, credit: 0, memo: "Dr" },
            { accountId: second, debit: 0, credit: 100_000, memo: "Cr" }
          ]
        })
        const listed = yield* journals.list({
          dateFrom: "2026-05-01",
          dateTo: "2026-05-31",
          sourceType: "manual",
          search: "Test",
          limit: 50,
          offset: 0
        })
        const updated = yield* journals.updateManual(
          created.id,
          "2026-05-11",
          "Updated",
          [
            { accountId: first, debit: 200_000, credit: 0, memo: "" },
            { accountId: second, debit: 0, credit: 200_000, memo: "" }
          ]
        )
        const removed = yield* journals.removeManual(created.id)
        return { created, listed, updated, removed }
      }).pipe(Effect.provide(layer))
    )

    expect(result.created.reference).toMatch(/^JE-\d{6}-\d{4}$/)
    expect(result.created.total_debit).toBe("100000")
    expect(result.created.lines?.[0]?.debit).toBe("100000")
    expect(result.listed.total).toBe(1)
    expect(result.listed.entries[0]?.lines).toBeNull()
    expect(result.updated).toMatchObject({
      entry_date: "2026-05-11",
      total_debit: "200000"
    })
    expect(result.removed.id).toBe(result.created.id)
  })

  test("rolls back when any account is invalid", async () => {
    const layer = await setup()
    const count = await Effect.runPromise(
      Effect.gen(function*() {
        const journals = yield* Journals
        const sql = yield* SqlClient.SqlClient
        const accounts = yield* sql<{ readonly id: number }>`
          SELECT id FROM accounts ORDER BY id LIMIT 1
        `
        yield* journals.create({
          entryDate: "2026-05-10",
          description: "Invalid account",
          sourceType: "manual",
          isPosted: true,
          createdBy: 1,
          lines: [
            {
              accountId: accounts[0]?.id ?? 0,
              debit: 100,
              credit: 0,
              memo: ""
            },
            { accountId: 999_999, debit: 0, credit: 100, memo: "" }
          ]
        }).pipe(Effect.flip)
        const rows = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count FROM journal_entries
        `
        return rows[0]?.count ?? -1
      }).pipe(Effect.provide(layer))
    )
    expect(count).toBe(0)
  })

  test("protects auto-generated entries", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const accounts = yield* sql<{ readonly id: number }>`
          SELECT id FROM accounts ORDER BY id LIMIT 2
        `
        const journals = yield* Journals
        const created = yield* journals.create({
          entryDate: "2026-05-10",
          reference: "AUTO-1",
          description: "Income",
          sourceType: "income",
          isPosted: true,
          createdBy: 1,
          lines: [
            {
              accountId: accounts[0]?.id ?? 0,
              debit: 100,
              credit: 0,
              memo: ""
            },
            {
              accountId: accounts[1]?.id ?? 0,
              debit: 0,
              credit: 100,
              memo: ""
            }
          ]
        })
        return yield* journals.removeManual(created.id).pipe(Effect.flip)
      }).pipe(Effect.provide(layer))
    )
    expect(result).toEqual(new JournalConflict({
      message: "cannot delete auto-generated journal entry (source: income)"
    }))
  })
})

describe("validateJournal", () => {
  test("parses integer-IDR strings and rejects unbalanced lines", () => {
    expect(validateJournal({
      entryDate: "2026-05-10",
      description: "Unbalanced",
      lines: [
        { accountId: 1, debit: " 100 ", credit: "0", memo: "" },
        { accountId: 2, debit: "0", credit: "50", memo: "" }
      ]
    })).toEqual({
      fields: { lines: "debits must equal credits" }
    })
  })
})
