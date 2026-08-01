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

const setup = async (development = true) => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-auth-api-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  await Effect.runPromise(
    seedDefaultAdmin.pipe(Effect.provide(runtimeLayer(databasePath)))
  )
  const web = HttpApp.toWebHandlerLayer(
    makeRouter("test", development),
    runtimeLayer(databasePath)
  )
  disposers.push(web.dispose)
  return { databasePath, handler: web.handler }
}

const login = async (
  handler: (request: Request) => Promise<Response>,
  body: unknown = { username: "admin", password: "admin" }
) => handler(new Request("http://localhost/api/v1/auth/login", {
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify(body)
}))

const sessionCookie = (response: Response) =>
  response.headers.get("set-cookie")?.split(";")[0] ?? ""

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("auth API", () => {
  test("matches login envelope and development cookie semantics", async () => {
    const { handler } = await setup()
    const response = await login(handler)
    const body = await response.json() as {
      readonly data: {
        readonly user: {
          readonly username: string
          readonly capabilities: ReadonlyArray<string>
          readonly must_change_password: boolean
        }
        readonly csrf_token: string
      }
    }
    const cookie = response.headers.get("set-cookie") ?? ""

    expect(response.status).toBe(200)
    expect(response.headers.get("content-type")).toBe(
      "application/json; charset=utf-8"
    )
    expect(body.data.user).toMatchObject({
      username: "admin",
      must_change_password: true
    })
    expect(body.data.user.capabilities).toHaveLength(11)
    expect(body.data.csrf_token).toHaveLength(64)
    expect(cookie).toContain("Max-Age=172800")
    expect(cookie).toContain("HttpOnly")
    expect(cookie).toContain("SameSite=Lax")
    expect(cookie).not.toContain("Secure")
  })

  test("rejects unknown JSON fields and reports validation fields", async () => {
    const { handler } = await setup()
    const unknown = await login(handler, {
      username: "admin",
      password: "admin",
      extra: true
    })
    const missing = await login(handler, { username: "", password: "" })
    const unknownBody = await unknown.json() as {
      readonly code: string
      readonly request_id: string
    }
    const missingBody = await missing.json() as {
      readonly code: string
      readonly fields: Readonly<Record<string, string>>
    }

    expect(unknown.status).toBe(400)
    expect(unknownBody.code).toBe("invalid_request")
    expect(unknownBody.request_id).toHaveLength(32)
    expect(missing.status).toBe(422)
    expect(missingBody.code).toBe("validation_failed")
    expect(missingBody.fields).toEqual({
      username: "required",
      password: "required"
    })
  })

  test("rate-limits failed JSON logins with the current shared unknown key", async () => {
    const { handler } = await setup()
    for (let attempt = 0; attempt < 5; attempt += 1) {
      const response = await login(handler, {
        username: `missing-${attempt}`,
        password: "wrong"
      })
      expect(response.status).toBe(401)
    }
    const blocked = await login(handler, {
      username: "another-user",
      password: "wrong"
    })
    const body = await blocked.json() as { readonly code: string }

    expect(blocked.status).toBe(429)
    expect(blocked.headers.get("retry-after")).toBe("900")
    expect(body.code).toBe("rate_limited")
  })

  test("correlates login audit events with API error request ids", async () => {
    const { databasePath, handler } = await setup()
    const response = await login(handler, {
      username: "ghost",
      password: "wrong"
    })
    const body = await response.json() as { readonly request_id: string }
    const rows = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        return yield* sql<{
          readonly request_id: string
          readonly actor_username: string
          readonly action: string
          readonly result: string
          readonly metadata: string
        }>`
          SELECT
            request_id, actor_username, action, result, metadata
          FROM audit_log
        `
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(rows).toEqual([{
      request_id: body.request_id,
      actor_username: "ghost",
      action: "auth.login_failed",
      result: "fail",
      metadata: '{"reason":"unknown_user"}'
    }])
  })

  test("returns cookie identity, csrf token, and clears logout session", async () => {
    const { handler } = await setup()
    const loginResponse = await login(handler)
    const cookie = sessionCookie(loginResponse)
    const csrfResponse = await handler(new Request(
      "http://localhost/api/v1/auth/csrf",
      { headers: { cookie } }
    ))
    const meResponse = await handler(new Request(
      "http://localhost/api/v1/auth/me",
      { headers: { cookie } }
    ))
    const logoutResponse = await handler(new Request(
      "http://localhost/api/v1/auth/logout",
      { method: "POST", headers: { cookie } }
    ))
    const afterLogout = await handler(new Request(
      "http://localhost/api/v1/auth/me",
      { headers: { cookie } }
    ))
    const me = await meResponse.json() as {
      readonly data: {
        readonly auth_method: string
        readonly token_id: number | null
      }
    }

    expect(csrfResponse.status).toBe(200)
    expect(me.data).toMatchObject({
      auth_method: "cookie",
      token_id: null
    })
    expect(logoutResponse.status).toBe(200)
    expect(logoutResponse.headers.get("set-cookie")).toContain("Max-Age=0")
    expect(afterLogout.status).toBe(303)
    expect(afterLogout.headers.get("location")).toBe("/login")
  })

  test("prefers bearer auth and exposes the token id", async () => {
    const { databasePath, handler } = await setup()
    const plaintext = "lat_http-test"
    const hash = new Bun.CryptoHasher("sha256")
      .update(plaintext)
      .digest("hex")
    await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        yield* sql`
          INSERT INTO api_tokens (
            user_id, name, token_prefix, token_hash, scopes
          )
          VALUES (
            1, 'http-test', 'lat_http', ${hash}, '["reports.view"]'
          )
        `
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    const response = await handler(new Request(
      "http://localhost/api/v1/auth/me",
      {
        headers: {
          authorization: `Bearer ${plaintext}`,
          cookie: "session_id=invalid"
        }
      }
    ))
    const body = await response.json() as {
      readonly data: {
        readonly auth_method: string
        readonly token_id: number
      }
    }

    expect(response.status).toBe(200)
    expect(body.data.auth_method).toBe("bearer")
    expect(body.data.token_id).toBe(1)
  })

  test("changes a password through cookie authentication", async () => {
    const { handler } = await setup()
    const loginResponse = await login(handler)
    const cookie = sessionCookie(loginResponse)
    const change = await handler(new Request(
      "http://localhost/api/v1/auth/password/change",
      {
        method: "POST",
        headers: {
          "content-type": "application/json",
          cookie
        },
        body: JSON.stringify({
          current_password: "admin",
          new_password: "new-password",
          confirm_password: "new-password"
        })
      }
    ))
    const oldLogin = await login(handler)
    const newLogin = await login(handler, {
      username: "admin",
      password: "new-password"
    })

    expect(change.status).toBe(200)
    expect(oldLogin.status).toBe(401)
    expect(newLogin.status).toBe(200)
  })
})
