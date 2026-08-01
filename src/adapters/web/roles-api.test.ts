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
  const directory = mkdtempSync(join(tmpdir(), "latasya-roles-api-"))
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
  const cookie = login.headers.get("set-cookie")?.split(";")[0] ?? ""
  return { databasePath, cookie, handler: web.handler }
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

describe("roles API", () => {
  test("lists capabilities and paginated system roles", async () => {
    const { cookie, handler } = await setup()
    const capabilities = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/roles/capabilities"
    )
    const list = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/roles?per_page=2"
    )
    const capabilitiesBody = await capabilities.json() as {
      readonly data: ReadonlyArray<string>
    }
    const listBody = await list.json() as {
      readonly data: ReadonlyArray<{ readonly name: string }>
      readonly meta: {
        readonly total: number
        readonly total_pages: number
      }
    }

    expect(capabilities.status).toBe(200)
    expect(capabilitiesBody.data).toHaveLength(11)
    expect(list.status).toBe(200)
    expect(listBody.data.map((role) => role.name)).toEqual([
      "admin",
      "bookkeeper"
    ])
    expect(listBody.meta).toMatchObject({ total: 3, total_pages: 2 })
  })

  test("creates, gets, updates, audits, and deletes a custom role", async () => {
    const { databasePath, cookie, handler } = await setup()
    const created = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/roles",
      {
        name: "auditor",
        description: " Audit access ",
        capabilities: ["reports.view"]
      }
    )
    const fetched = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/roles/auditor"
    )
    const updated = await request(
      handler,
      cookie,
      "PUT",
      "/api/v1/roles/auditor",
      {
        description: "Audit logs",
        capabilities: ["reports.view", "audit.view"]
      }
    )
    const removed = await request(
      handler,
      cookie,
      "DELETE",
      "/api/v1/roles/auditor"
    )
    const auditActions = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE target_label = 'auditor'
          ORDER BY id
        `
        return rows.map((row) => row.action)
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(created.status).toBe(201)
    expect((await created.json() as {
      readonly data: { readonly description: string }
    }).data.description).toBe("Audit access")
    expect(fetched.status).toBe(200)
    expect(updated.status).toBe(200)
    expect(removed.status).toBe(204)
    expect(await removed.text()).toBe("")
    expect(auditActions).toEqual([
      "role.create",
      "role.update",
      "role.delete"
    ])
  })

  test("enforces role capability and admin protections", async () => {
    const { databasePath, cookie, handler } = await setup()
    const plaintext = "lat_no-role-cap"
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
          SELECT 'viewer-role-api', password, 'Viewer', 'viewer'
          FROM users
          WHERE username = 'admin'
        `
        yield* sql`
          INSERT INTO api_tokens (
            user_id, name, token_prefix, token_hash, scopes
          )
          SELECT
            id, 'no-role-cap', 'lat_no-r', ${hash}, '[]'
          FROM users
          WHERE username = 'viewer-role-api'
        `
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const forbidden = await handler(new Request(
      "http://localhost/api/v1/roles",
      { headers: { authorization: `Bearer ${plaintext}` } }
    ))
    const editAdmin = await request(
      handler,
      cookie,
      "PUT",
      "/api/v1/roles/admin",
      { description: "Hacked", capabilities: [] }
    )
    const deleteAdmin = await request(
      handler,
      cookie,
      "DELETE",
      "/api/v1/roles/admin"
    )

    expect(forbidden.status).toBe(403)
    expect(editAdmin.status).toBe(409)
    expect(deleteAdmin.status).toBe(409)
  })
})
