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
  const directory = mkdtempSync(join(tmpdir(), "latasya-accounts-api-"))
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

describe("accounts API", () => {
  test("creates, filters, updates, audits, and deletes an account", async () => {
    const { databasePath, cookie, handler } = await setup()
    const created = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/accounts",
      {
        code: "9-API-CASH",
        name: "API Cash",
        account_type: "asset",
        normal_balance: "debit",
        is_cash: true
      }
    )
    const body = await created.json() as {
      readonly data: { readonly id: number; readonly is_cash: boolean }
    }
    const id = body.data.id
    const listed = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/accounts?type=asset&search=9-API-CASH"
    )
    const updated = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/accounts/${id}`,
      {
        code: "9-API-CASH",
        name: "Updated Cash",
        account_type: "asset",
        normal_balance: "debit",
        description: "updated"
      }
    )
    const removed = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/accounts/${id}`
    )
    const auditActions = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE target_id = ${id}
          ORDER BY id
        `
        return rows.map((row) => row.action)
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(created.status).toBe(201)
    expect(body.data.is_cash).toBe(true)
    expect((await listed.json() as {
      readonly meta: { readonly total: number }
    }).meta.total).toBe(1)
    expect(updated.status).toBe(200)
    expect((await updated.json() as {
      readonly data: { readonly is_cash: boolean }
    }).data.is_cash).toBe(true)
    expect(removed.status).toBe(204)
    expect(auditActions).toEqual([
      "account.create",
      "account.update",
      "account.delete"
    ])
  })

  test("matches cash validation, duplicate, id, and system protections", async () => {
    const { databasePath, cookie, handler } = await setup()
    const invalidCash = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/accounts",
      {
        code: "BAD-CASH",
        name: "Bad Cash",
        account_type: "liability",
        normal_balance: "credit",
        is_cash: true
      }
    )
    const duplicate = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/accounts",
      {
        code: "1-1001",
        name: "Duplicate",
        account_type: "asset",
        normal_balance: "debit"
      }
    )
    const invalidId = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/accounts/not-a-number"
    )
    const systemId = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly id: number }>`
          SELECT id FROM accounts WHERE is_system = 1 LIMIT 1
        `
        return rows[0]?.id ?? 0
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const systemDelete = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/accounts/${systemId}`
    )

    expect(invalidCash.status).toBe(422)
    expect(duplicate.status).toBe(409)
    expect(invalidId.status).toBe(400)
    expect(systemDelete.status).toBe(409)
  })
})
