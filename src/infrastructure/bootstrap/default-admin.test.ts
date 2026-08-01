import { afterEach, describe, expect, test } from "bun:test"
import { SqlClient } from "@effect/sql"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect, Layer } from "effect"
import { sqliteDatabaseLayer } from "../../adapters/sqlite/database.ts"
import {
  PasswordHasher,
  PasswordHasherLive
} from "../../domain/auth/password.ts"
import { migrateDatabase } from "../migrations/migrate.ts"
import { seedDefaultAdmin } from "./default-admin.ts"

const temporaryDirectories: Array<string> = []

const temporaryDatabasePath = () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-bootstrap-"))
  temporaryDirectories.push(directory)
  return join(directory, "latasya.db")
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("seedDefaultAdmin", () => {
  test("creates the same required default account only once", async () => {
    const databasePath = temporaryDatabasePath()
    await Effect.runPromise(migrateDatabase(databasePath))
    const dependencies = Layer.merge(
      sqliteDatabaseLayer(databasePath),
      PasswordHasherLive
    )
    const inspect = Effect.gen(function*() {
      const first = yield* seedDefaultAdmin
      const second = yield* seedDefaultAdmin
      const sql = yield* SqlClient.SqlClient
      const users = yield* sql<{
        readonly username: string
        readonly password: string
        readonly full_name: string
        readonly role: string
        readonly must_change_password: number
      }>`
        SELECT username, password, full_name, role, must_change_password
        FROM users
        WHERE username = 'admin'
      `
      const passwords = yield* PasswordHasher
      const passwordMatches = yield* passwords.verify(
        "admin",
        users[0]?.password ?? ""
      )

      return { first, second, users, passwordMatches }
    })

    const result = await Effect.runPromise(
      inspect.pipe(Effect.provide(dependencies))
    )

    expect(result.first).toBe(true)
    expect(result.second).toBe(false)
    expect(result.users).toHaveLength(1)
    expect(result.users[0]).toMatchObject({
      username: "admin",
      full_name: "Administrator",
      role: "admin",
      must_change_password: 1
    })
    expect(result.passwordMatches).toBe(true)
  })
})
