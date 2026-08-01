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
  const directory = mkdtempSync(join(tmpdir(), "latasya-contacts-api-"))
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

describe("contacts API", () => {
  test("creates, filters, updates, audits, and deletes contacts", async () => {
    const { databasePath, cookie, handler } = await setup()
    const created = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/contacts",
      {
        name: "New Student",
        contact_type: "customer",
        phone: "0812",
        class: "3A",
        distance_km: 7.5,
        has_sibling_discount: true,
        is_return_only: true
      }
    )
    const contact = await created.json() as {
      readonly id: number
      readonly distance_km: number
      readonly has_sibling_discount: boolean
    }
    const listed = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/contacts?type=customer&search=Student"
    )
    const fetched = await request(
      handler,
      cookie,
      "GET",
      `/api/v1/contacts/${contact.id}`
    )
    const updated = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/contacts/${contact.id}`,
      {
        name: "Updated Student",
        contact_type: "both",
        distance_km: 11.4,
        has_sibling_discount: true
      }
    )
    const removed = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/contacts/${contact.id}`
    )
    const auditActions = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE target_type = 'contact' AND target_id = ${contact.id}
          ORDER BY id
        `
        return rows.map((row) => row.action)
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(created.status).toBe(201)
    expect(contact).toMatchObject({
      distance_km: 7.5,
      has_sibling_discount: true
    })
    expect((await listed.json() as {
      readonly meta: { readonly total: number }
    }).meta.total).toBe(1)
    expect(fetched.status).toBe(200)
    expect(updated.status).toBe(200)
    expect((await updated.json() as {
      readonly distance_km: number
    }).distance_km).toBe(11.4)
    expect(removed.status).toBe(204)
    expect(auditActions).toEqual([
      "contact.create",
      "contact.update",
      "contact.delete"
    ])
  })

  test("preserves validation and missing-contact semantics", async () => {
    const { cookie, handler } = await setup()
    const invalid = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/contacts",
      {
        contact_type: "invalid",
        class: "123456",
        distance_km: -1
      }
    )
    const missingGet = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/contacts/not-a-number"
    )
    const missingDelete = await request(
      handler,
      cookie,
      "DELETE",
      "/api/v1/contacts/999999"
    )
    const fields = (await invalid.json() as {
      readonly fields: Readonly<Record<string, string>>
    }).fields

    expect(invalid.status).toBe(422)
    expect(fields).toEqual({
      name: "required",
      contact_type: "must be customer, supplier, or both",
      class: "must be 5 characters or fewer",
      distance_km: "must be 0 or greater"
    })
    expect(missingGet.status).toBe(404)
    expect(missingDelete.status).toBe(204)
  })

  test("requires contacts.manage for mutations", async () => {
    const { databasePath, handler } = await setup()
    const plaintext = "lat_no-contact-cap"
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
          SELECT 'viewer-contacts-api', password, 'Viewer', 'viewer'
          FROM users
          WHERE username = 'admin'
        `
        yield* sql`
          INSERT INTO api_tokens (
            user_id, name, token_prefix, token_hash, scopes
          )
          SELECT
            id, 'no-contact-cap', 'lat_no-c', ${hash}, '[]'
          FROM users
          WHERE username = 'viewer-contacts-api'
        `
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const response = await handler(new Request(
      "http://localhost/api/v1/contacts",
      {
        method: "POST",
        headers: {
          authorization: `Bearer ${plaintext}`,
          "content-type": "application/json"
        },
        body: JSON.stringify({
          name: "Forbidden",
          contact_type: "customer"
        })
      }
    ))
    expect(response.status).toBe(403)
  })
})
