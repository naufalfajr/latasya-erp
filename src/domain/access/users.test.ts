import { afterEach, describe, expect, test } from "bun:test"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect, Layer } from "effect"
import { sqliteDatabaseLayer } from "../../adapters/sqlite/database.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { PasswordHasherLive } from "../auth/password.ts"
import {
  UserConflict,
  Users,
  UsersLive,
  validateUser
} from "./users.ts"

const temporaryDirectories: Array<string> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-users-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const base = Layer.merge(
    sqliteDatabaseLayer(databasePath),
    PasswordHasherLive
  )
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(base)))
  return Layer.merge(base, UsersLive.pipe(Layer.provide(base)))
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("Users", () => {
  test("creates users with forced password rotation", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const users = yield* Users
        return yield* users.create({
          username: "newuser",
          fullName: "New User",
          role: "viewer",
          isActive: true,
          password: "password123"
        })
      }).pipe(Effect.provide(layer))
    )
    expect(result).toMatchObject({
      username: "newuser",
      role: "viewer",
      is_active: true,
      must_change_password: true
    })
  })

  test("updates profile and forces rotation for admin-set passwords", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const users = yield* Users
        const created = yield* users.create({
          username: "target",
          fullName: "Target",
          role: "viewer",
          isActive: true,
          password: "password123"
        })
        return yield* users.update(1, created.id, {
          fullName: "Updated",
          role: "bookkeeper",
          isActive: true,
          password: "replacement"
        })
      }).pipe(Effect.provide(layer))
    )
    expect(result).toMatchObject({
      full_name: "Updated",
      role: "bookkeeper",
      must_change_password: true
    })
  })

  test("prevents self-deactivation", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const users = yield* Users
        return yield* users.deactivate(1, 1).pipe(Effect.flip)
      }).pipe(Effect.provide(layer))
    )
    expect(result).toEqual(new UserConflict({ reason: "self_deactivation" }))
  })
})

describe("validateUser", () => {
  test("uses UTF-8 byte length like Go", () => {
    expect(validateUser({
      username: "",
      fullName: "",
      role: "missing",
      password: "éééé"
    }, false, false)).toEqual({
      username: "required",
      full_name: "required",
      role: "invalid role"
    })
  })
})
