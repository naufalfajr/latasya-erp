import { SqlClient } from "@effect/sql"
import { Data, Effect } from "effect"
import { PasswordHasher } from "../../domain/auth/password.ts"

export class DefaultAdminSeedError extends Data.TaggedError("DefaultAdminSeedError")<{
  readonly cause: unknown
}> {}

export const seedDefaultAdmin: Effect.Effect<
  boolean,
  DefaultAdminSeedError,
  PasswordHasher | SqlClient.SqlClient
> = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient
  const rows = yield* sql<{ readonly count: number }>`
    SELECT COUNT(*) AS count
    FROM users
    WHERE username = 'admin'
  `
  if ((rows[0]?.count ?? 0) > 0) {
    return false
  }

  const passwords = yield* PasswordHasher
  const password = yield* passwords.hash("admin")

  yield* sql`
    INSERT INTO users (
      username,
      password,
      full_name,
      role,
      must_change_password
    )
    VALUES (
      'admin',
      ${password},
      'Administrator',
      'admin',
      1
    )
  `
  yield* Effect.logInfo(
    "seeding default admin user (password change required on first login)"
  )
  return true
}).pipe(
  Effect.mapError((cause) => new DefaultAdminSeedError({ cause }))
)
