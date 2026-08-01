import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"
import {
  isCapability,
  type Capability
} from "../auth/capability.ts"

type RoleRow = {
  readonly name: string
  readonly description: string
  readonly is_system: number
  readonly capabilities: string
  readonly created_at: string
  readonly updated_at: string
}

export type Role = {
  readonly name: string
  readonly description: string
  readonly is_system: boolean
  readonly capabilities: ReadonlyArray<Capability>
  readonly created_at: string
  readonly updated_at: string
}

export type RoleValues = {
  readonly name: string
  readonly description: string
  readonly capabilities: ReadonlyArray<Capability>
}

export class RoleNotFound extends Data.TaggedError("RoleNotFound") {}

export class RoleConflict extends Data.TaggedError("RoleConflict")<{
  readonly reason: "duplicate" | "admin_edit" | "admin_delete" | "in_use"
}> {}

export class RoleStoreError extends Data.TaggedError("RoleStoreError")<{
  readonly cause: unknown
}> {}

export interface Roles {
  readonly list: Effect.Effect<ReadonlyArray<Role>, RoleStoreError>
  readonly get: (name: string) => Effect.Effect<Role, RoleNotFound | RoleStoreError>
  readonly create: (
    values: RoleValues
  ) => Effect.Effect<Role, RoleConflict | RoleStoreError>
  readonly update: (
    name: string,
    values: Omit<RoleValues, "name">
  ) => Effect.Effect<Role, RoleConflict | RoleNotFound | RoleStoreError>
  readonly remove: (
    name: string
  ) => Effect.Effect<Role, RoleConflict | RoleNotFound | RoleStoreError>
}

export const Roles = Context.GenericTag<Roles>("latasya/Roles")

const parseRole = (row: RoleRow): Role => {
  const decoded: unknown = JSON.parse(row.capabilities)
  if (
    !Array.isArray(decoded) ||
    !decoded.every((value) => typeof value === "string" && isCapability(value))
  ) {
    throw new Error(`invalid capabilities for role ${row.name}`)
  }
  return {
    name: row.name,
    description: row.description,
    is_system: row.is_system !== 0,
    capabilities: decoded,
    created_at: row.created_at,
    updated_at: row.updated_at
  }
}

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new RoleStoreError({ cause }))
  )

const decode = (row: RoleRow) =>
  Effect.try({
    try: () => parseRole(row),
    catch: (cause) => new RoleStoreError({ cause })
  })

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient

  const get: Roles["get"] = (name) =>
    Effect.gen(function*() {
      const rows = yield* store(sql<RoleRow>`
        SELECT
          name, description, is_system, capabilities, created_at, updated_at
        FROM roles
        WHERE name = ${name}
      `)
      const row = rows[0]
      if (row === undefined) {
        return yield* new RoleNotFound()
      }
      return yield* decode(row)
    })

  const list = Effect.gen(function*() {
    const rows = yield* store(sql<RoleRow>`
      SELECT
        name, description, is_system, capabilities, created_at, updated_at
      FROM roles
      ORDER BY is_system DESC, name
    `)
    return yield* Effect.forEach(rows, decode)
  })

  const create: Roles["create"] = (values) =>
    Effect.gen(function*() {
      const existing = yield* store(sql<{ readonly found: number }>`
        SELECT 1 AS found
        FROM roles
        WHERE name = ${values.name}
      `)
      if (existing.length > 0) {
        return yield* new RoleConflict({ reason: "duplicate" })
      }
      yield* store(sql`
        INSERT INTO roles (
          name, description, is_system, capabilities
        )
        VALUES (
          ${values.name},
          ${values.description},
          0,
          ${JSON.stringify(values.capabilities)}
        )
      `)
      return yield* get(values.name).pipe(
        Effect.catchTag(
          "RoleNotFound",
          (cause) => new RoleStoreError({ cause })
        )
      )
    })

  const update: Roles["update"] = (name, values) =>
    Effect.gen(function*() {
      if (name === "admin") {
        return yield* new RoleConflict({ reason: "admin_edit" })
      }
      yield* get(name)
      yield* store(sql`
        UPDATE roles
        SET
          description = ${values.description},
          capabilities = ${JSON.stringify(values.capabilities)},
          updated_at = datetime('now')
        WHERE name = ${name}
      `)
      return yield* get(name)
    })

  const remove: Roles["remove"] = (name) =>
    Effect.gen(function*() {
      const role = yield* get(name)
      if (name === "admin") {
        return yield* new RoleConflict({ reason: "admin_delete" })
      }
      const counts = yield* store(sql<{ readonly count: number }>`
        SELECT COUNT(*) AS count
        FROM users
        WHERE role = ${name}
      `)
      if ((counts[0]?.count ?? 0) > 0) {
        return yield* new RoleConflict({ reason: "in_use" })
      }
      yield* store(sql`DELETE FROM roles WHERE name = ${name}`)
      return role
    })

  return Roles.of({ list, get, create, update, remove })
})

export const RolesLive = Layer.effect(Roles, make)

const roleNamePattern = /^[a-z][a-z0-9_-]*$/

export const validateRole = (
  input: {
    readonly name: string
    readonly capabilities: ReadonlyArray<string>
  },
  editing: boolean
): Readonly<Record<string, string>> => {
  const fields: Record<string, string> = {}
  if (!editing) {
    if (input.name.trim() === "") {
      fields.name = "required"
    } else if (!roleNamePattern.test(input.name)) {
      fields.name =
        "use lowercase letters, digits, hyphens or underscores (must start with a letter)"
    } else if (input.name === "admin") {
      fields.name = "reserved role name"
    }
  }
  const unknown = input.capabilities.find((value) => !isCapability(value))
  if (unknown !== undefined) {
    fields.capabilities = `unknown capability: ${unknown}`
  }
  return fields
}
