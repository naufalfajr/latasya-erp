import { Database } from "bun:sqlite"
import { Data, Effect } from "effect"
import { migrationSources } from "./sources.ts"

export interface MigrationSummary {
  readonly applied: number
  readonly total: number
}

export class DatabaseMigrationError extends Data.TaggedError("DatabaseMigrationError")<{
  readonly databasePath: string
  readonly operation: string
  readonly cause: unknown
}> {}

const migrationFailure = (
  databasePath: string,
  operation: string,
  cause: unknown
) => new DatabaseMigrationError({ databasePath, operation, cause })

const openDatabase = (databasePath: string) =>
  Effect.try({
    try: () => new Database(databasePath, { create: true, readwrite: true }),
    catch: (cause) => migrationFailure(databasePath, "open database", cause)
  })

const closeDatabase = (database: Database) =>
  Effect.sync(() => database.close())

const applyMigrations = (databasePath: string, database: Database) =>
  Effect.try({
    try: (): MigrationSummary => {
      database.run("PRAGMA journal_mode = WAL")
      database.run("PRAGMA foreign_keys = OFF")
      database.run("PRAGMA busy_timeout = 5000")
      database.run("PRAGMA synchronous = NORMAL")
      database.run(`
        CREATE TABLE IF NOT EXISTS schema_migrations (
          filename TEXT PRIMARY KEY,
          applied_at TEXT NOT NULL DEFAULT (datetime('now'))
        )
      `)

      const hasMigration = database.query<
        { readonly count: number },
        [filename: string]
      >("SELECT COUNT(*) AS count FROM schema_migrations WHERE filename = ?")
      const recordMigration = database.query<
        never,
        [filename: string]
      >("INSERT INTO schema_migrations (filename) VALUES (?)")
      let applied = 0

      for (const migration of migrationSources) {
        if ((hasMigration.get(migration.filename)?.count ?? 0) > 0) {
          continue
        }

        const apply = database.transaction(() => {
          database.run(migration.sql)
          recordMigration.run(migration.filename)
        })
        apply()
        applied += 1
      }

      const violations = database.query("PRAGMA foreign_key_check").all()
      if (violations.length > 0) {
        throw new Error(`foreign_key_check returned ${violations.length} violation(s)`)
      }
      database.run("PRAGMA foreign_keys = ON")

      return { applied, total: migrationSources.length }
    },
    catch: (cause) => migrationFailure(databasePath, "apply migrations", cause)
  })

export const migrateDatabase = (
  databasePath: string
): Effect.Effect<MigrationSummary, DatabaseMigrationError> =>
  Effect.acquireUseRelease(
    openDatabase(databasePath),
    (database) => applyMigrations(databasePath, database),
    closeDatabase
  )
