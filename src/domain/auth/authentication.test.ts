import { afterEach, describe, expect, test } from "bun:test"
import { SqlClient } from "@effect/sql"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect, Layer } from "effect"
import { sqliteDatabaseLayer } from "../../adapters/sqlite/database.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { PasswordHasherLive } from "./password.ts"
import {
  Authentication,
  AuthenticationLive,
  InvalidCredentials,
  InvalidSession,
  InvalidToken,
  PasswordValidationFailed
} from "./authentication.ts"

const temporaryDirectories: Array<string> = []

const temporaryDatabasePath = () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-auth-"))
  temporaryDirectories.push(directory)
  return join(directory, "latasya.db")
}

const testLayer = (databasePath: string) => {
  const base = Layer.merge(
    sqliteDatabaseLayer(databasePath),
    PasswordHasherLive
  )
  return Layer.merge(base, AuthenticationLive.pipe(Layer.provide(base)))
}

const setup = async () => {
  const databasePath = temporaryDatabasePath()
  await Effect.runPromise(migrateDatabase(databasePath))
  const layer = testLayer(databasePath)
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(layer)))
  return layer
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("Authentication", () => {
  test("logs in with a compatible sliding session and replaces old sessions", async () => {
    const layer = await setup()
    const inspect = Effect.gen(function*() {
      const auth = yield* Authentication
      const first = yield* auth.login("admin", "admin")
      const second = yield* auth.login("admin", "admin")
      const sql = yield* SqlClient.SqlClient
      const rows = yield* sql<{
        readonly id: string
        readonly csrf_token: string
        readonly idle_seconds: number
        readonly absolute_seconds: number
      }>`
        SELECT
          id,
          csrf_token,
          CAST(strftime('%s', expires_at) - strftime('%s', 'now') AS INTEGER)
            AS idle_seconds,
          CAST(
            strftime('%s', absolute_expires_at) - strftime('%s', 'now')
            AS INTEGER
          ) AS absolute_seconds
        FROM sessions
      `
      return { first, second, rows }
    })
    const result = await Effect.runPromise(inspect.pipe(Effect.provide(layer)))

    expect(result.first.sessionId).not.toBe(result.second.sessionId)
    expect(result.second.sessionId).toHaveLength(64)
    expect(result.second.csrfToken).toHaveLength(64)
    expect(result.second.user.mustChangePassword).toBe(true)
    expect(result.second.user.capabilities).toHaveLength(11)
    expect(result.rows).toHaveLength(1)
    expect(result.rows[0]?.id).toBe(result.second.sessionId)
    expect(result.rows[0]?.csrf_token).toBe(result.second.csrfToken)
    expect(result.rows[0]?.idle_seconds).toBeWithin(21_598, 21_601)
    expect(result.rows[0]?.absolute_seconds).toBeWithin(172_798, 172_801)
  })

  test("preserves credential failure reasons without leaking them publicly", async () => {
    const layer = await setup()
    const attempt = (username: string, password: string) =>
      Effect.gen(function*() {
        const auth = yield* Authentication
        return yield* auth.login(username, password)
      }).pipe(Effect.provide(layer), Effect.flip)

    const unknown = await Effect.runPromise(attempt("missing", "password"))
    const badPassword = await Effect.runPromise(attempt("admin", "wrong"))
    expect(unknown).toEqual(new InvalidCredentials({ reason: "unknown_user" }))
    expect(badPassword).toEqual(new InvalidCredentials({
      reason: "bad_password",
      userId: 1
    }))
  })

  test("resolves cookie sessions and rejects expired sessions", async () => {
    const layer = await setup()
    const inspect = Effect.gen(function*() {
      const auth = yield* Authentication
      const login = yield* auth.login("admin", "admin")
      const resolved = yield* auth.authenticateSession(login.sessionId)
      const sql = yield* SqlClient.SqlClient
      yield* sql`
        UPDATE sessions
        SET absolute_expires_at = datetime('now', '-1 second')
        WHERE id = ${login.sessionId}
      `
      const expired = yield* auth.authenticateSession(login.sessionId).pipe(
        Effect.flip
      )
      return { login, resolved, expired }
    })
    const result = await Effect.runPromise(inspect.pipe(Effect.provide(layer)))

    expect(result.resolved.user.id).toBe(result.login.user.id)
    expect(result.resolved.csrfToken).toBe(result.login.csrfToken)
    expect(result.expired).toEqual(new InvalidSession({
      reason: "invalid_or_expired"
    }))
  })

  test("intersects bearer scopes with current role capabilities", async () => {
    const layer = await setup()
    const inspect = Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      const auth = yield* Authentication
      const plaintext = "lat_test-token"
      const hash = new Bun.CryptoHasher("sha256")
        .update(plaintext)
        .digest("hex")
      yield* sql`
        INSERT INTO users (
          username, password, full_name, role, is_active, must_change_password
        )
        SELECT
          'viewer', password, 'Viewer', 'viewer', 1, 0
        FROM users
        WHERE username = 'admin'
      `
      yield* sql`
        INSERT INTO api_tokens (
          user_id, name, token_prefix, token_hash, scopes
        )
        SELECT
          id,
          'test',
          'lat_test',
          ${hash},
          '["reports.view","accounts.manage"]'
        FROM users
        WHERE username = 'viewer'
      `
      return yield* auth.authenticateBearer(plaintext)
    })
    const result = await Effect.runPromise(inspect.pipe(Effect.provide(layer)))

    expect(result.method).toBe("bearer")
    expect(result.effectiveCapabilities).toEqual(["reports.view"])
  })

  test("rejects revoked bearer tokens and inactive token users", async () => {
    const layer = await setup()
    const inspect = Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      const auth = yield* Authentication
      const plaintext = "lat_revoked"
      const hash = new Bun.CryptoHasher("sha256")
        .update(plaintext)
        .digest("hex")
      yield* sql`
        INSERT INTO api_tokens (
          user_id, name, token_prefix, token_hash, scopes, revoked_at
        )
        VALUES (1, 'revoked', 'lat_revo', ${hash}, '[]', datetime('now'))
      `
      return yield* auth.authenticateBearer(plaintext).pipe(Effect.flip)
    })
    const result = await Effect.runPromise(inspect.pipe(Effect.provide(layer)))
    expect(result).toEqual(new InvalidToken({ reason: "invalid_or_expired" }))
  })

  test("validates and persists password changes with Go byte-length rules", async () => {
    const layer = await setup()
    const inspect = Effect.gen(function*() {
      const auth = yield* Authentication
      const login = yield* auth.login("admin", "admin")
      const invalid = yield* auth.changePassword(
        login.user,
        "wrong",
        "short",
        "different"
      ).pipe(Effect.flip)
      yield* auth.changePassword(
        login.user,
        "admin",
        "new-password",
        "new-password"
      )
      yield* auth.logoutSession(login.sessionId)
      const relogin = yield* auth.login("admin", "new-password")
      return { invalid, relogin }
    })
    const result = await Effect.runPromise(inspect.pipe(Effect.provide(layer)))

    expect(result.invalid).toEqual(new PasswordValidationFailed({
      fields: {
        current_password: "incorrect",
        new_password: "must be at least 8 characters",
        confirm_password: "does not match new_password"
      }
    }))
    expect(result.relogin.user.mustChangePassword).toBe(false)
  })
})
