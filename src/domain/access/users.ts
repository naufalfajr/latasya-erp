import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"
import {
  PasswordHasher,
  type PasswordHashError
} from "../auth/password.ts"

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

export type User = {
  readonly id: number
  readonly username: string
  readonly full_name: string
  readonly role: string
  readonly is_active: boolean
  readonly must_change_password: boolean
  readonly created_at: string
  readonly updated_at: string
}

export type CreateUserValues = {
  readonly username: string
  readonly fullName: string
  readonly role: string
  readonly isActive: boolean
  readonly password: string
}

export type UpdateUserValues = {
  readonly fullName: string
  readonly role: string
  readonly isActive: boolean
  readonly password?: string
}

export class UserNotFound extends Data.TaggedError("UserNotFound") {}

export class UserConflict extends Data.TaggedError("UserConflict")<{
  readonly reason: "duplicate_username" | "self_deactivation"
}> {}

export class UserStoreError extends Data.TaggedError("UserStoreError")<{
  readonly cause: unknown
}> {}

export class UserPasswordError extends Data.TaggedError("UserPasswordError")<{
  readonly cause: PasswordHashError
}> {}

export interface Users {
  readonly list: Effect.Effect<ReadonlyArray<User>, UserStoreError>
  readonly get: (id: number) => Effect.Effect<User, UserNotFound | UserStoreError>
  readonly roleExists: (role: string) => Effect.Effect<boolean, UserStoreError>
  readonly create: (
    values: CreateUserValues
  ) => Effect.Effect<User, UserConflict | UserPasswordError | UserStoreError>
  readonly update: (
    actorId: number,
    id: number,
    values: UpdateUserValues
  ) => Effect.Effect<
    User,
    UserConflict | UserNotFound | UserPasswordError | UserStoreError
  >
  readonly deactivate: (
    actorId: number,
    id: number
  ) => Effect.Effect<User, UserConflict | UserNotFound | UserStoreError>
}

export const Users = Context.GenericTag<Users>("latasya/Users")

const fromRow = (row: UserRow): User => ({
  id: row.id,
  username: row.username,
  full_name: row.full_name,
  role: row.role,
  is_active: row.is_active !== 0,
  must_change_password: row.must_change_password !== 0,
  created_at: row.created_at,
  updated_at: row.updated_at
})

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new UserStoreError({ cause }))
  )

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient
  const passwords = yield* PasswordHasher

  const rowById = (id: number) => store(sql<UserRow>`
    SELECT
      id, username, password, full_name, role, is_active,
      must_change_password, created_at, updated_at
    FROM users
    WHERE id = ${id}
  `)

  const get: Users["get"] = (id) =>
    Effect.gen(function*() {
      const rows = yield* rowById(id)
      const row = rows[0]
      if (row === undefined) {
        return yield* new UserNotFound()
      }
      return fromRow(row)
    })

  const list = store(sql<UserRow>`
    SELECT
      id, username, '' AS password, full_name, role, is_active,
      must_change_password, created_at, updated_at
    FROM users
    ORDER BY id
  `).pipe(
    Effect.map((rows) => rows.map(fromRow))
  )

  const roleExists: Users["roleExists"] = (role) =>
    store(sql<{ readonly found: number }>`
      SELECT 1 AS found
      FROM roles
      WHERE name = ${role}
    `).pipe(
      Effect.map((rows) => rows.length > 0)
    )

  const create: Users["create"] = (values) =>
    Effect.gen(function*() {
      const duplicate = yield* store(sql<{ readonly found: number }>`
        SELECT 1 AS found
        FROM users
        WHERE username = ${values.username}
      `)
      if (duplicate.length > 0) {
        return yield* new UserConflict({ reason: "duplicate_username" })
      }
      const password = yield* passwords.hash(values.password).pipe(
        Effect.mapError((cause) => new UserPasswordError({ cause }))
      )
      yield* store(sql`
        INSERT INTO users (
          username,
          password,
          full_name,
          role,
          is_active,
          must_change_password
        )
        VALUES (
          ${values.username},
          ${password},
          ${values.fullName},
          ${values.role},
          ${values.isActive ? 1 : 0},
          1
        )
      `)
      const rows = yield* store(sql<UserRow>`
        SELECT
          id, username, password, full_name, role, is_active,
          must_change_password, created_at, updated_at
        FROM users
        WHERE username = ${values.username}
      `)
      const row = rows[0]
      if (row === undefined) {
        return yield* new UserStoreError({
          cause: new Error("created user could not be read")
        })
      }
      return fromRow(row)
    })

  const update: Users["update"] = (actorId, id, values) =>
    Effect.gen(function*() {
      const existingRows = yield* rowById(id)
      const existing = existingRows[0]
      if (existing === undefined) {
        return yield* new UserNotFound()
      }
      if (actorId === id && !values.isActive) {
        return yield* new UserConflict({ reason: "self_deactivation" })
      }
      yield* store(sql`
        UPDATE users
        SET
          full_name = ${values.fullName},
          role = ${values.role},
          is_active = ${values.isActive ? 1 : 0},
          updated_at = datetime('now')
        WHERE id = ${id}
      `)
      if (values.password !== undefined && values.password !== "") {
        const password = yield* passwords.hash(values.password).pipe(
          Effect.mapError((cause) => new UserPasswordError({ cause }))
        )
        yield* store(sql`
          UPDATE users
          SET password = ${password}, updated_at = datetime('now')
          WHERE id = ${id}
        `)
        if (actorId !== id) {
          yield* store(sql`
            UPDATE users
            SET must_change_password = 1, updated_at = datetime('now')
            WHERE id = ${id}
          `)
        }
      }
      return yield* get(id)
    })

  const deactivate: Users["deactivate"] = (actorId, id) =>
    Effect.gen(function*() {
      if (actorId === id) {
        return yield* new UserConflict({ reason: "self_deactivation" })
      }
      const existing = yield* get(id)
      yield* store(sql`
        UPDATE users
        SET is_active = 0, updated_at = datetime('now')
        WHERE id = ${id}
      `)
      return existing
    })

  return Users.of({
    list,
    get,
    roleExists,
    create,
    update,
    deactivate
  })
})

export const UsersLive = Layer.effect(Users, make)

const byteLength = (value: string) => new TextEncoder().encode(value).byteLength

export const validateUser = (
  input: {
    readonly username: string
    readonly fullName: string
    readonly role: string
    readonly password: string
  },
  editing: boolean,
  roleValid: boolean
): Readonly<Record<string, string>> => {
  const fields: Record<string, string> = {}
  if (!editing && input.username.trim() === "") {
    fields.username = "required"
  }
  if (input.fullName.trim() === "") {
    fields.full_name = "required"
  }
  if (!editing && input.password === "") {
    fields.password = "required"
  } else if (input.password !== "" && byteLength(input.password) < 8) {
    fields.password = "minimum 8 characters"
  }
  if (input.role === "") {
    fields.role = "required"
  } else if (!roleValid) {
    fields.role = "invalid role"
  }
  return fields
}
