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
  const directory = mkdtempSync(join(tmpdir(), "latasya-tokens-api-"))
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
  body?: unknown,
  idempotencyKey?: string
) => handler(new Request(`http://localhost${path}`, {
  method,
  headers: {
    ...(authentication.startsWith("Bearer ")
      ? { authorization: authentication }
      : { cookie: authentication }),
    ...(body === undefined ? {} : { "content-type": "application/json" }),
    ...(idempotencyKey === undefined
      ? {}
      : { "idempotency-key": idempotencyKey })
  },
  ...(body === undefined ? {} : { body: JSON.stringify(body) })
}))

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("api tokens API", () => {
  test("creates once, lists safely, revokes, and audits", async () => {
    const { databasePath, cookie, handler } = await setup()
    const input = {
      name: "Telegram Bot",
      scopes: ["reports.view"],
      expires_at: "2030-01-01T00:00:00Z"
    }
    const created = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/api-tokens",
      input,
      "token-create"
    )
    const createdText = await created.text()
    const replay = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/api-tokens",
      input,
      "token-create"
    )
    const replayText = await replay.text()
    const body = JSON.parse(createdText) as {
      readonly data: {
        readonly id: number
        readonly name: string
        readonly prefix: string
        readonly plaintext: string
        readonly scopes: ReadonlyArray<string>
      }
    }
    const listed = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/api-tokens"
    )
    const listedText = await listed.text()
    const listedBody = JSON.parse(listedText) as {
      readonly data: ReadonlyArray<{
        readonly id: number
        readonly revoked_at: string | null
      }>
    }
    const revoked = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/api-tokens/${body.data.id}`
    )
    const database = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const tokens = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count FROM api_tokens
        `
        const audits = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE target_type = 'api_token'
          ORDER BY id
        `
        return {
          tokenCount: tokens[0]?.count ?? 0,
          audits: audits.map((row) => row.action)
        }
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(created.status).toBe(201)
    expect(replay.status).toBe(201)
    expect(replayText).toBe(createdText)
    expect(body.data).toMatchObject({
      name: "Telegram Bot",
      scopes: ["reports.view"]
    })
    expect(body.data.plaintext).toMatch(/^lat_[0-9A-Za-z]{32}$/)
    expect(body.data.prefix).toBe(body.data.plaintext.slice(0, 8))
    expect(listed.status).toBe(200)
    expect(listedText).not.toContain("plaintext")
    expect(listedText).not.toContain("token_hash")
    expect(listedBody.data).toHaveLength(1)
    expect(revoked.status).toBe(204)
    expect(database).toEqual({
      tokenCount: 1,
      audits: ["api_token.create", "api_token.revoke"]
    })
  })

  test("allows bearer listing but blocks bearer token mutations", async () => {
    const { cookie, handler } = await setup()
    const created = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/api-tokens",
      { name: "Bearer", scopes: ["reports.view"] }
    )
    const token = (await created.json() as {
      readonly data: {
        readonly id: number
        readonly plaintext: string
      }
    }).data
    const bearer = `Bearer ${token.plaintext}`
    const listed = await request(
      handler,
      bearer,
      "GET",
      "/api/v1/api-tokens"
    )
    const create = await request(
      handler,
      bearer,
      "POST",
      "/api/v1/api-tokens",
      { name: "Spawn", scopes: [] }
    )
    const revoke = await request(
      handler,
      bearer,
      "DELETE",
      `/api/v1/api-tokens/${token.id}`
    )

    expect(listed.status).toBe(200)
    expect(create.status).toBe(403)
    expect(await create.json()).toMatchObject({
      code: "forbidden",
      error: "api token cannot create or revoke api tokens"
    })
    expect(revoke.status).toBe(403)
  })

  test("matches strict body and validation behavior", async () => {
    const { cookie, handler } = await setup()
    const missingScopes = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/api-tokens",
      { name: "No scopes" }
    )
    const past = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/api-tokens",
      {
        name: "Past",
        scopes: [],
        expires_at: "2020-01-01T00:00:00Z"
      }
    )
    const malformedDate = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/api-tokens",
      {
        name: "Bad date",
        scopes: [],
        expires_at: "tomorrow"
      }
    )
    const unknown = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/api-tokens",
      { name: "Unknown", scopes: [], extra: true }
    )
    const invalidId = await request(
      handler,
      cookie,
      "DELETE",
      "/api/v1/api-tokens/nope"
    )

    expect(missingScopes.status).toBe(422)
    expect(await missingScopes.json()).toMatchObject({
      fields: { scopes: "required" }
    })
    expect(past.status).toBe(422)
    expect(await past.json()).toMatchObject({
      fields: { expires_at: "must be in the future" }
    })
    expect(malformedDate.status).toBe(400)
    expect(unknown.status).toBe(400)
    expect(invalidId.status).toBe(400)
  })
})
