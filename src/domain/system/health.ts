import { SqlClient } from "@effect/sql"
import { Data, Effect } from "effect"

export interface HealthStatus {
  readonly version: string
  readonly migrations: number
}

export class DatabaseUnavailable extends Data.TaggedError("DatabaseUnavailable")<{
  readonly cause: unknown
}> {}

export const readHealth = (
  version: string
): Effect.Effect<HealthStatus, DatabaseUnavailable, SqlClient.SqlClient> =>
  Effect.gen(function*() {
    const sql = yield* SqlClient.SqlClient
    const rows = yield* sql.unsafe<{ readonly migrations: number }>(
      "SELECT COUNT(*) AS migrations FROM schema_migrations"
    )

    return {
      version,
      migrations: rows[0]?.migrations ?? 0
    }
  }).pipe(
    Effect.timeout("2 seconds"),
    Effect.mapError((cause) => new DatabaseUnavailable({ cause }))
  )
