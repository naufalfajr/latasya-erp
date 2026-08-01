import { afterEach, describe, expect, test } from "bun:test"
import { Database } from "bun:sqlite"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { migrateDatabase } from "./migrate.ts"

const temporaryDirectories: Array<string> = []

const temporaryDatabasePath = () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-migrations-"))
  temporaryDirectories.push(directory)
  return join(directory, "latasya.db")
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("migrateDatabase", () => {
  test("applies the existing Go migration ledger unchanged", async () => {
    const databasePath = temporaryDatabasePath()
    const summary = await Effect.runPromise(migrateDatabase(databasePath))

    expect(summary).toEqual({ applied: 21, total: 21 })

    using database = new Database(databasePath)
    const filenames = database.query<
      { readonly filename: string },
      []
    >("SELECT filename FROM schema_migrations ORDER BY filename").all()

    expect(filenames).toHaveLength(21)
    expect(filenames[0]?.filename).toBe("001_initial_schema.sql")
    expect(filenames.at(-1)?.filename).toBe("021_portal_code.sql")
  })

  test("is idempotent for an already migrated database", async () => {
    const databasePath = temporaryDatabasePath()

    await Effect.runPromise(migrateDatabase(databasePath))
    const second = await Effect.runPromise(migrateDatabase(databasePath))

    expect(second).toEqual({ applied: 0, total: 21 })
  })

  test("creates the final schema expected by the current application", async () => {
    const databasePath = temporaryDatabasePath()
    await Effect.runPromise(migrateDatabase(databasePath))

    using database = new Database(databasePath)
    const tables = database.query<
      { readonly name: string },
      []
    >(`
      SELECT name
      FROM sqlite_master
      WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
      ORDER BY name
    `).all().map(({ name }) => name)

    expect(tables).toEqual([
      "accounts",
      "api_tokens",
      "audit_log",
      "bill_lines",
      "bills",
      "company_profile",
      "contacts",
      "credit_note_lines",
      "credit_notes",
      "google_calendar_connections",
      "google_oauth_states",
      "idempotency_keys",
      "invoice_lines",
      "invoices",
      "journal_entries",
      "journal_lines",
      "payments",
      "roles",
      "routes",
      "schema_migrations",
      "school_closures",
      "sessions",
      "users",
      "vehicle_route_assignments",
      "vehicles"
    ])
  })
})
