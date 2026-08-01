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
  const directory = mkdtempSync(join(tmpdir(), "latasya-audit-api-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  await Effect.runPromise(
    seedDefaultAdmin.pipe(Effect.provide(runtimeLayer(databasePath)))
  )
  await Effect.runPromise(
    Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      yield* sql`
        INSERT INTO audit_log (
          occurred_at,
          request_id,
          actor_id,
          actor_username,
          action,
          target_type,
          target_id,
          target_label,
          result,
          ip,
          metadata
        )
        VALUES
          (
            '2026-01-10T10:00:00.000Z',
            'request-1',
            1,
            'alpha',
            'invoice.create',
            'invoice',
            10,
            'INV-10',
            'ok',
            '127.0.0.1',
            '{"amount":100}'
          ),
          (
            '2026-02-10T10:00:00.000Z',
            'request-2',
            1,
            'alpha',
            'invoice.update',
            'invoice',
            10,
            'INV-10',
            'ok',
            '127.0.0.1',
            '{"amount":200}'
          ),
          (
            '2026-02-11T10:00:00.000Z',
            NULL,
            NULL,
            'beta',
            'contact.create',
            NULL,
            NULL,
            NULL,
            'fail',
            NULL,
            NULL
          )
      `
      yield* sql`
        INSERT INTO users (
          username,
          password,
          full_name,
          role,
          is_active,
          must_change_password
        )
        VALUES (
          'audit-viewer',
          'unused',
          'Audit Viewer',
          'viewer',
          1,
          0
        )
      `
      const users = yield* sql<{ readonly id: number }>`
        SELECT last_insert_rowid() AS id
      `
      yield* sql`
        INSERT INTO sessions (
          id,
          user_id,
          expires_at,
          absolute_expires_at,
          csrf_token
        )
        VALUES (
          'audit-viewer-session',
          ${users[0]?.id ?? 0},
          '2030-01-01 00:00:00',
          '2030-01-02 00:00:00',
          'csrf'
        )
      `
    }).pipe(Effect.provide(runtimeLayer(databasePath)))
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
    cookie: login.headers.get("set-cookie")?.split(";")[0] ?? "",
    viewerCookie: "session_id=audit-viewer-session",
    handler: web.handler
  }
}

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("audit API", () => {
  test("filters by actor, action prefix, date range, and paginates", async () => {
    const { cookie, handler } = await setup()
    const response = await handler(new Request(
      "http://localhost/api/v1/audit" +
        "?actor=%20alpha%20&action=invoice." +
        "&from=2026-01-01&to=2026-12-31&per_page=1",
      { headers: { cookie } }
    ))
    const body = await response.json() as {
      readonly data: ReadonlyArray<{
        readonly ID: number
        readonly OccurredAt: string
        readonly ActorID: {
          readonly Int64: number
          readonly Valid: boolean
        }
        readonly Action: string
        readonly TargetID: {
          readonly Int64: number
          readonly Valid: boolean
        }
        readonly Metadata: string
      }>
      readonly meta: {
        readonly page: number
        readonly per_page: number
        readonly total: number
        readonly total_pages: number
      }
    }

    expect(response.status).toBe(200)
    expect(body.meta).toEqual({
      page: 1,
      per_page: 1,
      total: 2,
      total_pages: 2
    })
    expect(body.data).toHaveLength(1)
    expect(body.data[0]).toMatchObject({
      OccurredAt: "2026-02-10T10:00:00.000Z",
      ActorID: { Int64: 1, Valid: true },
      Action: "invoice.update",
      TargetID: { Int64: 10, Valid: true },
      Metadata: "{\"amount\":200}"
    })
  })

  test("silently ignores invalid dates and enforces audit.view", async () => {
    const { cookie, viewerCookie, handler } = await setup()
    const invalidDate = await handler(new Request(
      "http://localhost/api/v1/audit" +
        "?actor=alpha&from=not-a-date&to=2026-02-31",
      { headers: { cookie } }
    ))
    const invalidBody = await invalidDate.json() as {
      readonly meta: { readonly total: number }
    }
    const forbidden = await handler(new Request(
      "http://localhost/api/v1/audit",
      { headers: { cookie: viewerCookie } }
    ))

    expect(invalidDate.status).toBe(200)
    expect(invalidBody.meta.total).toBe(2)
    expect(forbidden.status).toBe(403)
    expect(await forbidden.json()).toMatchObject({
      code: "forbidden",
      error: "insufficient permissions"
    })
  })
})
