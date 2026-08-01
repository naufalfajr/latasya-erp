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
  const directory = mkdtempSync(join(tmpdir(), "latasya-calendar-api-"))
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
  authentication: string,
  method: string,
  path: string,
  body?: unknown
) => handler(new Request(`http://localhost${path}`, {
  method,
  headers: {
    ...(authentication.startsWith("Bearer ")
      ? { authorization: authentication }
      : { cookie: authentication }),
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

describe("school calendar API", () => {
  test("creates, lists, calculates, deletes, and audits closures", async () => {
    const { databasePath, cookie, handler } = await setup()
    const created = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/school-calendar/closures",
      {
        title: "  Long break  ",
        start_date: " 2026-06-01 ",
        end_date: " 2026-06-15 "
      }
    )
    const createdBody = await created.json() as {
      readonly data: {
        readonly id: number
        readonly source: string
        readonly title: string
      }
    }
    const list = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/school-calendar/closures?month=%202026-06%20"
    )
    const listBody = await list.json() as {
      readonly data: ReadonlyArray<{ readonly id: number }>
    }
    const effective = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/school-calendar/effective-days?month=2026-06"
    )
    const effectiveBody = await effective.json() as {
      readonly data: {
        readonly month: string
        readonly effective_days: number
        readonly multiplier_percent: number
      }
    }
    const removed = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/school-calendar/closures/${createdBody.data.id}`
    )
    const audits = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE target_type = 'school_closure'
          ORDER BY id
        `
        return rows.map((row) => row.action)
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(created.status).toBe(201)
    expect(createdBody.data).toMatchObject({
      source: "manual",
      title: "Long break"
    })
    expect(list.status).toBe(200)
    expect(listBody.data).toEqual([{ ...createdBody.data }])
    expect(effective.status).toBe(200)
    expect(effectiveBody.data).toEqual({
      month: "2026-06",
      effective_days: 13,
      multiplier_percent: 75
    })
    expect(removed.status).toBe(200)
    expect(await removed.json()).toEqual({ data: { deleted: true } })
    expect(audits).toEqual([
      "school_closure.create",
      "school_closure.delete"
    ])
  })

  test("enforces strict validation, month formats, and capability", async () => {
    const { cookie, handler } = await setup()
    const tokenResponse = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/api-tokens",
      { name: "No calendar scope", scopes: [] }
    )
    const plaintext = (await tokenResponse.json() as {
      readonly data: { readonly plaintext: string }
    }).data.plaintext
    const noCapability = await request(
      handler,
      `Bearer ${plaintext}`,
      "GET",
      "/api/v1/school-calendar/closures"
    )
    const invalidMonth = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/school-calendar/closures?month=2026-6"
    )
    const invalidClosure = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/school-calendar/closures",
      {
        title: "",
        start_date: "2026-06-20",
        end_date: "2026-06-10"
      }
    )
    const unknownField = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/school-calendar/closures",
      {
        title: "Break",
        start_date: "2026-06-01",
        end_date: "2026-06-02",
        unknown: true
      }
    )
    const invalidId = await request(
      handler,
      cookie,
      "DELETE",
      "/api/v1/school-calendar/closures/nope"
    )

    expect(noCapability.status).toBe(403)
    expect(invalidMonth.status).toBe(400)
    expect(await invalidMonth.json()).toMatchObject({
      fields: { month: "must be YYYY-MM" }
    })
    expect(invalidClosure.status).toBe(422)
    expect(await invalidClosure.json()).toMatchObject({
      fields: {
        title: "required",
        end_date: "must be on or after start_date"
      }
    })
    expect(unknownField.status).toBe(400)
    expect(invalidId.status).toBe(404)
  })

  test("reaches the Google connection check for an in-scope admin", async () => {
    const { cookie, handler } = await setup()
    const response = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/integrations/google-calendar/sync"
    )

    expect(response.status).toBe(400)
    expect(await response.json()).toMatchObject({
      code: "invalid_request",
      error: "google calendar is not connected"
    })
  })
})
