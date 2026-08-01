import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { SqlClient } from "@effect/sql"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { makeRouter } from "./router.ts"

const temporaryDirectories: Array<string> = []
const disposers: Array<() => Promise<void>> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-users-api-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  await Effect.runPromise(
    seedDefaultAdmin.pipe(Effect.provide(runtimeLayer(databasePath)))
  )
  const web = HttpApp.toWebHandlerLayer(
    makeRouter("test", true),
    runtimeLayer(databasePath)
  )
  disposers.push(web.dispose)
  const login = await web.handler(new Request(
    "http://localhost/api/v1/auth/login",
    {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ username: "admin", password: "admin" })
    }
  ))
  return {
    databasePath,
    cookie: login.headers.get("set-cookie")?.split(";")[0] ?? "",
    handler: web.handler
  }
}

const request = (
  handler: (request: Request) => Promise<Response>,
  cookie: string,
  method: string,
  path: string,
  body?: unknown
) => handler(new Request(`http://localhost${path}`, {
  method,
  headers: {
    cookie,
    ...(body === undefined ? {} : { "content-type": "application/json" })
  },
  ...(body === undefined ? {} : { body: JSON.stringify(body) })
}))

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("users API", () => {
  test("creates, gets, updates, lists, audits, and deactivates users", async () => {
    const { databasePath, cookie, handler } = await setup()
    const createdResponse = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/users",
      {
        username: "newuser",
        full_name: " New User ",
        role: "viewer",
        password: "password123"
      }
    )
    const createdBody = await createdResponse.json() as {
      readonly data: {
        readonly id: number
        readonly username: string
        readonly full_name: string
        readonly must_change_password: boolean
        readonly password?: string
      }
    }
    const id = createdBody.data.id
    const getResponse = await request(
      handler,
      cookie,
      "GET",
      `/api/v1/users/${id}`
    )
    const updateResponse = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/users/${id}`,
      {
        full_name: "Updated User",
        role: "bookkeeper",
        is_active: true,
        password: "replacement"
      }
    )
    const listResponse = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/users?per_page=1&page=2"
    )
    const deleteResponse = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/users/${id}`
    )
    const state = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const users = yield* sql<{
          readonly is_active: number
          readonly must_change_password: number
        }>`
          SELECT is_active, must_change_password
          FROM users
          WHERE id = ${id}
        `
        const audits = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE target_id = ${id}
          ORDER BY id
        `
        return { users, audits }
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(createdResponse.status).toBe(201)
    expect(createdBody.data).toMatchObject({
      username: "newuser",
      full_name: "New User",
      must_change_password: true
    })
    expect(createdBody.data.password).toBeUndefined()
    expect(getResponse.status).toBe(200)
    expect(updateResponse.status).toBe(200)
    expect(listResponse.status).toBe(200)
    expect(deleteResponse.status).toBe(204)
    expect(state.users[0]).toEqual({
      is_active: 0,
      must_change_password: 1
    })
    expect(state.audits.map((row) => row.action)).toEqual([
      "user.create",
      "user.update",
      "user.delete"
    ])
  })

  test("matches validation, duplicate, invalid id, and self-protection errors", async () => {
    const { cookie, handler } = await setup()
    const invalid = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/users",
      {
        username: "",
        full_name: "",
        role: "missing",
        password: "short"
      }
    )
    const duplicate = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/users",
      {
        username: "admin",
        full_name: "Duplicate",
        role: "admin",
        password: "password123"
      }
    )
    const invalidId = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/users/not-a-number"
    )
    const selfDelete = await request(
      handler,
      cookie,
      "DELETE",
      "/api/v1/users/1"
    )
    const fields = (await invalid.json() as {
      readonly fields: Readonly<Record<string, string>>
    }).fields

    expect(invalid.status).toBe(422)
    expect(fields).toEqual({
      username: "required",
      full_name: "required",
      password: "minimum 8 characters",
      role: "invalid role"
    })
    expect(duplicate.status).toBe(409)
    expect(invalidId.status).toBe(400)
    expect(selfDelete.status).toBe(409)
  })

  test("rejects users without users.manage", async () => {
    const { databasePath, handler } = await setup()
    const plaintext = "lat_no-user-cap"
    const hash = new Bun.CryptoHasher("sha256")
      .update(plaintext)
      .digest("hex")
    await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        yield* sql`
          INSERT INTO users (
            username, password, full_name, role
          )
          SELECT 'viewer-users-api', password, 'Viewer', 'viewer'
          FROM users
          WHERE username = 'admin'
        `
        yield* sql`
          INSERT INTO api_tokens (
            user_id, name, token_prefix, token_hash, scopes
          )
          SELECT
            id, 'no-user-cap', 'lat_no-u', ${hash}, '[]'
          FROM users
          WHERE username = 'viewer-users-api'
        `
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const response = await handler(new Request(
      "http://localhost/api/v1/users",
      { headers: { authorization: `Bearer ${plaintext}` } }
    ))
    expect(response.status).toBe(403)
  })
})
