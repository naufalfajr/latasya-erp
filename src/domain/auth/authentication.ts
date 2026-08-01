import { SqlClient } from "@effect/sql"
import { Clock, Context, Data, Effect, Layer } from "effect"
import {
  allCapabilities,
  intersectCapabilities,
  parseCapabilities,
  type Capability
} from "./capability.ts"
import {
  PasswordHasher,
  type PasswordHashError
} from "./password.ts"

const sessionIdleMilliseconds = 6 * 60 * 60 * 1000
const sessionAbsoluteMilliseconds = 48 * 60 * 60 * 1000
const sessionRefreshThresholdMilliseconds = 3 * 60 * 60 * 1000

type UserRow = {
  readonly id: number
  readonly username: string
  readonly password: string
  readonly full_name: string
  readonly role: string
  readonly is_active: number
  readonly must_change_password: number
  readonly created_at: string
  readonly updated_at: string
}

type SessionRow = {
  readonly user_id: number
  readonly csrf_token: string
  readonly expires_at: string
}

type TokenRow = {
  readonly id: number
  readonly user_id: number
  readonly scopes: string
}

export type AuthUser = {
  readonly id: number
  readonly username: string
  readonly passwordHash: string
  readonly fullName: string
  readonly role: string
  readonly isActive: boolean
  readonly mustChangePassword: boolean
  readonly createdAt: string
  readonly updatedAt: string
  readonly capabilities: ReadonlyArray<Capability>
}

export type CookieAuthentication = {
  readonly method: "cookie"
  readonly user: AuthUser
  readonly sessionId: string
  readonly csrfToken: string
  readonly effectiveCapabilities: ReadonlyArray<Capability>
}

export type BearerAuthentication = {
  readonly method: "bearer"
  readonly user: AuthUser
  readonly tokenId: number
  readonly effectiveCapabilities: ReadonlyArray<Capability>
}

export type Authenticated = CookieAuthentication | BearerAuthentication

export type LoginSession = CookieAuthentication

export class InvalidCredentials extends Data.TaggedError("InvalidCredentials")<{
  readonly reason: "unknown_user" | "bad_password" | "inactive"
  readonly userId?: number
}> {}

export class InvalidSession extends Data.TaggedError("InvalidSession")<{
  readonly reason: "invalid_or_expired" | "user_not_found_or_inactive"
}> {}

export class InvalidToken extends Data.TaggedError("InvalidToken")<{
  readonly reason: "invalid_or_expired" | "user_not_found_or_inactive"
}> {}

export class PasswordValidationFailed extends Data.TaggedError(
  "PasswordValidationFailed"
)<{
  readonly fields: Readonly<Record<string, string>>
}> {}

export class AuthenticationStoreError extends Data.TaggedError(
  "AuthenticationStoreError"
)<{
  readonly cause: unknown
}> {}

type AuthenticationError =
  | AuthenticationStoreError
  | InvalidCredentials
  | InvalidSession
  | InvalidToken
  | PasswordHashError
  | PasswordValidationFailed

export interface Authentication {
  readonly login: (
    username: string,
    password: string
  ) => Effect.Effect<LoginSession, AuthenticationError>
  readonly authenticateSession: (
    sessionId: string
  ) => Effect.Effect<CookieAuthentication, AuthenticationError>
  readonly authenticateBearer: (
    plaintext: string
  ) => Effect.Effect<BearerAuthentication, AuthenticationError>
  readonly logoutSession: (
    sessionId: string
  ) => Effect.Effect<void, AuthenticationStoreError>
  readonly changePassword: (
    user: AuthUser,
    currentPassword: string,
    newPassword: string,
    confirmPassword: string
  ) => Effect.Effect<void, AuthenticationError>
  readonly cleanExpiredSessions: Effect.Effect<void, AuthenticationStoreError>
}

export const Authentication = Context.GenericTag<Authentication>(
  "latasya/Authentication"
)

const randomHex = (bytes: number) =>
  Effect.try({
    try: () => {
      const value = crypto.getRandomValues(new Uint8Array(bytes))
      return Array.from(value, (byte) => byte.toString(16).padStart(2, "0")).join("")
    },
    catch: (cause) => new AuthenticationStoreError({ cause })
  })

const sqliteDateTime = (milliseconds: number) =>
  new Date(milliseconds).toISOString().slice(0, 19).replace("T", " ")

const parseSqliteDateTime = (value: string) =>
  Date.parse(`${value.replace(" ", "T")}Z`)

const hashToken = (plaintext: string) =>
  new Bun.CryptoHasher("sha256").update(plaintext).digest("hex")

const byteLength = (value: string) => new TextEncoder().encode(value).byteLength

const storeError = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new AuthenticationStoreError({ cause }))
  )

const fromUserRow = (
  row: UserRow,
  capabilities: ReadonlyArray<Capability>
): AuthUser => ({
  id: row.id,
  username: row.username,
  passwordHash: row.password,
  fullName: row.full_name,
  role: row.role,
  isActive: row.is_active !== 0,
  mustChangePassword: row.must_change_password !== 0,
  createdAt: row.created_at,
  updatedAt: row.updated_at,
  capabilities
})

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient
  const passwords = yield* PasswordHasher

  const roleCapabilities = (role: string) =>
    Effect.gen(function*() {
      if (role === "admin") {
        return allCapabilities
      }
      const rows = yield* storeError(sql<{ readonly capabilities: string }>`
        SELECT capabilities
        FROM roles
        WHERE name = ${role}
      `)
      const encoded = rows[0]?.capabilities
      if (encoded === undefined) {
        return []
      }
      return yield* Effect.try({
        try: () => parseCapabilities(encoded),
        catch: (cause) => new AuthenticationStoreError({ cause })
      })
    })

  const userById = (id: number) =>
    Effect.gen(function*() {
      const rows = yield* storeError(sql<UserRow>`
        SELECT
          id, username, password, full_name, role, is_active,
          must_change_password, created_at, updated_at
        FROM users
        WHERE id = ${id}
      `)
      const row = rows[0]
      if (row === undefined) {
        return undefined
      }
      const capabilities = yield* roleCapabilities(row.role)
      return fromUserRow(row, capabilities)
    })

  const createSession = (user: AuthUser) =>
    Effect.gen(function*() {
      const sessionId = yield* randomHex(32)
      const csrfToken = yield* randomHex(32)
      const now = yield* Clock.currentTimeMillis
      yield* storeError(sql`
        INSERT INTO sessions (
          id, user_id, expires_at, absolute_expires_at, csrf_token
        )
        VALUES (
          ${sessionId},
          ${user.id},
          ${sqliteDateTime(now + sessionIdleMilliseconds)},
          ${sqliteDateTime(now + sessionAbsoluteMilliseconds)},
          ${csrfToken}
        )
      `)
      return {
        method: "cookie",
        user,
        sessionId,
        csrfToken,
        effectiveCapabilities: user.capabilities
      } satisfies CookieAuthentication
    })

  const login: Authentication["login"] = (username, password) =>
    Effect.gen(function*() {
      const rows = yield* storeError(sql<UserRow>`
        SELECT
          id, username, password, full_name, role, is_active,
          must_change_password, created_at, updated_at
        FROM users
        WHERE username = ${username}
      `)
      const row = rows[0]
      if (row === undefined) {
        return yield* new InvalidCredentials({ reason: "unknown_user" })
      }
      if (!(yield* passwords.verify(password, row.password))) {
        return yield* new InvalidCredentials({
          reason: "bad_password",
          userId: row.id
        })
      }
      if (row.is_active === 0) {
        return yield* new InvalidCredentials({
          reason: "inactive",
          userId: row.id
        })
      }

      let currentRow = row
      if (
        username === "admin" &&
        password === "admin" &&
        row.must_change_password === 0
      ) {
        yield* storeError(sql`
          UPDATE users
          SET must_change_password = 1, updated_at = datetime('now')
          WHERE id = ${row.id}
        `)
        currentRow = { ...row, must_change_password: 1 }
      }

      const capabilities = yield* roleCapabilities(currentRow.role)
      const user = fromUserRow(currentRow, capabilities)
      yield* storeError(sql`DELETE FROM sessions WHERE user_id = ${user.id}`).pipe(
        Effect.ignore
      )
      return yield* createSession(user)
    })

  const authenticateSession: Authentication["authenticateSession"] =
    (sessionId) =>
      Effect.gen(function*() {
        const rows = yield* storeError(sql<SessionRow>`
          SELECT user_id, csrf_token, expires_at
          FROM sessions
          WHERE id = ${sessionId}
            AND expires_at > datetime('now')
            AND absolute_expires_at > datetime('now')
        `)
        const session = rows[0]
        if (session === undefined) {
          return yield* new InvalidSession({ reason: "invalid_or_expired" })
        }
        const user = yield* userById(session.user_id)
        if (user === undefined || !user.isActive) {
          return yield* new InvalidSession({
            reason: "user_not_found_or_inactive"
          })
        }

        const now = yield* Clock.currentTimeMillis
        const expiresAt = parseSqliteDateTime(session.expires_at)
        if (expiresAt - now < sessionRefreshThresholdMilliseconds) {
          yield* storeError(sql`
            UPDATE sessions
            SET expires_at = MIN(
              ${sqliteDateTime(now + sessionIdleMilliseconds)},
              absolute_expires_at
            )
            WHERE id = ${sessionId}
          `).pipe(Effect.ignore)
        }

        return {
          method: "cookie",
          user,
          sessionId,
          csrfToken: session.csrf_token,
          effectiveCapabilities: user.capabilities
        }
      })

  const authenticateBearer: Authentication["authenticateBearer"] =
    (plaintext) =>
      Effect.gen(function*() {
        if (plaintext === "") {
          return yield* new InvalidToken({ reason: "invalid_or_expired" })
        }
        const rows = yield* storeError(sql<TokenRow>`
          SELECT id, user_id, scopes
          FROM api_tokens
          WHERE token_hash = ${hashToken(plaintext)}
            AND revoked_at IS NULL
            AND (expires_at IS NULL OR datetime(expires_at) > datetime('now'))
        `)
        const token = rows[0]
        if (token === undefined) {
          return yield* new InvalidToken({ reason: "invalid_or_expired" })
        }
        const user = yield* userById(token.user_id)
        if (user === undefined || !user.isActive) {
          return yield* new InvalidToken({
            reason: "user_not_found_or_inactive"
          })
        }
        const scopes = yield* Effect.try({
          try: () => parseCapabilities(token.scopes),
          catch: (cause) => new AuthenticationStoreError({ cause })
        })
        const effectiveCapabilities = intersectCapabilities(
          scopes,
          user.capabilities,
          user.role === "admin"
        )

        yield* storeError(sql`
          UPDATE api_tokens
          SET last_used_at = datetime('now')
          WHERE id = ${token.id}
        `).pipe(Effect.forkDaemon)

        return {
          method: "bearer",
          user,
          tokenId: token.id,
          effectiveCapabilities
        }
      })

  const logoutSession: Authentication["logoutSession"] = (sessionId) =>
    storeError(sql`DELETE FROM sessions WHERE id = ${sessionId}`).pipe(
      Effect.asVoid
    )

  const changePassword: Authentication["changePassword"] = (
    user,
    currentPassword,
    newPassword,
    confirmPassword
  ) =>
    Effect.gen(function*() {
      const fields: Record<string, string> = {}
      if (currentPassword === "") {
        fields.current_password = "required"
      } else if (!(yield* passwords.verify(currentPassword, user.passwordHash))) {
        fields.current_password = "incorrect"
      }
      if (byteLength(newPassword) < 8) {
        fields.new_password = "must be at least 8 characters"
      }
      if (newPassword !== confirmPassword) {
        fields.confirm_password = "does not match new_password"
      }
      if (newPassword !== "" && newPassword === currentPassword) {
        fields.new_password = "must be different from current_password"
      }
      if (Object.keys(fields).length > 0) {
        return yield* new PasswordValidationFailed({ fields })
      }

      const passwordHash = yield* passwords.hash(newPassword)
      yield* storeError(sql`
        UPDATE users
        SET password = ${passwordHash}, updated_at = datetime('now')
        WHERE id = ${user.id}
      `)
      yield* storeError(sql`
        UPDATE users
        SET must_change_password = 0, updated_at = datetime('now')
        WHERE id = ${user.id}
      `)
    })

  const cleanExpiredSessions = storeError(sql`
    DELETE FROM sessions
    WHERE expires_at < datetime('now')
       OR absolute_expires_at < datetime('now')
  `).pipe(Effect.asVoid)

  return Authentication.of({
    login,
    authenticateSession,
    authenticateBearer,
    logoutSession,
    changePassword,
    cleanExpiredSessions
  })
})

export const AuthenticationLive = Layer.effect(Authentication, make)
