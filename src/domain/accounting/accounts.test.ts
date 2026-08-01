import { afterEach, describe, expect, test } from "bun:test"
import { SqlClient } from "@effect/sql"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect, Layer } from "effect"
import { sqliteDatabaseLayer } from "../../adapters/sqlite/database.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import {
  AccountConflict,
  Accounts,
  AccountsLive,
  validateAccount
} from "./accounts.ts"

const temporaryDirectories: Array<string> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-accounts-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const database = sqliteDatabaseLayer(databasePath)
  return Layer.merge(database, AccountsLive.pipe(Layer.provide(database)))
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("Accounts", () => {
  test("creates, filters, updates, and removes accounts", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const accounts = yield* Accounts
        const created = yield* accounts.create({
          code: "1-TEST",
          name: "Test Cash",
          accountType: "asset",
          normalBalance: "debit",
          isActive: true,
          isCash: true,
          description: ""
        })
        const listed = yield* accounts.list({
          type: "asset",
          search: "1-TEST"
        })
        const updated = yield* accounts.update(created.id, {
          code: "1-TEST",
          name: "Updated Cash",
          accountType: "asset",
          normalBalance: "debit",
          isActive: false,
          isCash: true,
          description: "updated"
        })
        const removed = yield* accounts.remove(created.id)
        return { created, listed, updated, removed }
      }).pipe(Effect.provide(layer))
    )
    expect(result.created.is_cash).toBe(true)
    expect(result.listed).toHaveLength(1)
    expect(result.updated).toMatchObject({
      name: "Updated Cash",
      is_active: false
    })
    expect(result.removed.code).toBe("1-TEST")
  })

  test("protects system and linked accounts", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const accounts = yield* Accounts
        const sql = yield* SqlClient.SqlClient
        yield* sql`
          INSERT INTO accounts (
            code, name, account_type, normal_balance, is_system
          )
          VALUES ('SYS', 'System', 'asset', 'debit', 1)
        `
        const rows = yield* sql<{ readonly id: number }>`
          SELECT id FROM accounts WHERE code = 'SYS'
        `
        return yield* accounts.remove(rows[0]?.id ?? 0).pipe(Effect.flip)
      }).pipe(Effect.provide(layer))
    )
    expect(result).toEqual(new AccountConflict({ reason: "system" }))
  })
})

describe("validateAccount", () => {
  test("enforces the cash-account invariant", () => {
    expect(validateAccount({
      code: "2-1000",
      name: "Cash Liability",
      accountType: "liability",
      normalBalance: "credit",
      isCash: true
    })).toEqual({
      is_cash: "cash accounts must be debit-normal assets"
    })
  })
})
