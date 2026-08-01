import { afterEach, describe, expect, test } from "bun:test"
import { SqlClient } from "@effect/sql"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect, Layer } from "effect"
import { sqliteDatabaseLayer } from "../../adapters/sqlite/database.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import {
  RoleConflict,
  Roles,
  RolesLive,
  validateRole
} from "./roles.ts"

const temporaryDirectories: Array<string> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-roles-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const database = sqliteDatabaseLayer(databasePath)
  return Layer.merge(database, RolesLive.pipe(Layer.provide(database)))
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("Roles", () => {
  test("lists system roles in the current order", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const roles = yield* Roles
        return yield* roles.list
      }).pipe(Effect.provide(layer))
    )
    expect(result.map((role) => role.name)).toEqual([
      "admin",
      "bookkeeper",
      "viewer"
    ])
  })

  test("creates, updates, and removes a custom role", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const roles = yield* Roles
        const created = yield* roles.create({
          name: "auditor",
          description: " Audits ",
          capabilities: ["reports.view"]
        })
        const updated = yield* roles.update("auditor", {
          description: "Read audits",
          capabilities: ["reports.view", "audit.view"]
        })
        const removed = yield* roles.remove("auditor")
        return { created, updated, removed }
      }).pipe(Effect.provide(layer))
    )

    expect(result.created.description).toBe(" Audits ")
    expect(result.updated.capabilities).toEqual([
      "reports.view",
      "audit.view"
    ])
    expect(result.removed.name).toBe("auditor")
  })

  test("protects admin and roles assigned to users", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const roles = yield* Roles
        const sql = yield* SqlClient.SqlClient
        const admin = yield* roles.remove("admin").pipe(Effect.flip)
        yield* roles.create({
          name: "inuse",
          description: "",
          capabilities: []
        })
        yield* sql`
          INSERT INTO users (
            username, password, full_name, role
          )
          VALUES ('assigned', 'hash', 'Assigned', 'inuse')
        `
        const inUse = yield* roles.remove("inuse").pipe(Effect.flip)
        return { admin, inUse }
      }).pipe(Effect.provide(layer))
    )

    expect(result.admin).toEqual(new RoleConflict({ reason: "admin_delete" }))
    expect(result.inUse).toEqual(new RoleConflict({ reason: "in_use" }))
  })
})

describe("validateRole", () => {
  test("matches current name and capability validation", () => {
    expect(validateRole({
      name: "Invalid Name",
      capabilities: ["not.real"]
    }, false)).toEqual({
      name:
        "use lowercase letters, digits, hyphens or underscores (must start with a letter)",
      capabilities: "unknown capability: not.real"
    })
  })
})
