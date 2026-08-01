import { SqlClient } from "@effect/sql"
import * as SqliteClient from "@effect/sql-sqlite-bun/SqliteClient"
import { Context, Data, Effect, Layer } from "effect"

export class DatabaseInitializationError extends Data.TaggedError("DatabaseInitializationError")<{
  readonly message: string
}> {}

const initializeConnection = (
  context: Context.Context<SqlClient.SqlClient>
) => {
  const sql = Context.get(context, SqlClient.SqlClient)

  return Effect.gen(function*() {
    yield* sql.unsafe("PRAGMA busy_timeout = 5000")
    yield* sql.unsafe("PRAGMA synchronous = NORMAL")
    yield* sql.unsafe("PRAGMA foreign_keys = ON")

    const foreignKeys = yield* sql.unsafe<{ readonly foreign_keys: number }>(
      "PRAGMA foreign_keys"
    )
    if (foreignKeys[0]?.foreign_keys !== 1) {
      return yield* new DatabaseInitializationError({
        message: "SQLite foreign-key enforcement could not be enabled"
      })
    }
  })
}

export const sqliteDatabaseLayer = (databasePath: string) =>
  SqliteClient.layer({
    filename: databasePath
  }).pipe(
    Layer.tap(initializeConnection)
  )
