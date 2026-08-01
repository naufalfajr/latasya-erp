import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { Authentication } from "../../domain/auth/authentication.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { makeRouter } from "./router.ts"

const temporaryDirectories: Array<string> = []
const disposers: Array<() => Promise<void>> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-accounts-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const layer = runtimeLayer(databasePath)
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(layer)))
  const session = await Effect.runPromise(
    Effect.gen(function*() {
      const authentication = yield* Authentication
      const loggedIn = yield* authentication.login("admin", "admin")
      yield* authentication.changePassword(
        loggedIn.user,
        "admin",
        "accounts-password",
        "accounts-password"
      )
      return loggedIn
    }).pipe(Effect.provide(layer))
  )
  const web = HttpApp.toWebHandlerLayer(makeRouter("test", true), layer)
  disposers.push(web.dispose)
  return {
    handler: web.handler,
    cookie: `session_id=${session.sessionId}`,
    csrf: session.csrfToken
  }
}

const postForm = (
  handler: (request: Request) => Promise<Response>,
  path: string,
  cookie: string,
  values: Readonly<Record<string, string>>
) => handler(new Request(`http://localhost${path}`, {
  method: "POST",
  headers: {
    cookie,
    "content-type": "application/x-www-form-urlencoded"
  },
  body: new URLSearchParams(values)
}))

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("server-rendered chart of accounts", () => {
  test("lists seeded accounts and renders legacy validation messages", async () => {
    const { handler, cookie, csrf } = await setup()
    const list = await handler(new Request(
      "http://localhost/dashboard/accounts?type=asset&search=Cash",
      { headers: { cookie } }
    ))
    const listBody = await list.text()
    expect(list.status).toBe(200)
    expect(listBody).toContain("<title>Chart of Accounts — Latasya ERP</title>")
    expect(listBody).toContain("Cash")
    expect(listBody).toContain("tab-active")

    const invalid = await postForm(
      handler,
      "/dashboard/accounts",
      cookie,
      { csrf_token: csrf, is_cash: "on" }
    )
    const invalidBody = await invalid.text()
    expect(invalid.status).toBe(200)
    expect(invalidBody).toContain("Code is required")
    expect(invalidBody).toContain("Name is required")
    expect(invalidBody).toContain("Account type is required")
    expect(invalidBody).toContain("Normal balance is required")
    expect(invalidBody).toContain("cash accounts must be debit-normal assets")
  })

  test("creates an account and returns the legacy flash redirect", async () => {
    const { handler, cookie, csrf } = await setup()
    const created = await postForm(
      handler,
      "/dashboard/accounts",
      cookie,
      {
        csrf_token: csrf,
        code: "9-9001",
        name: "UI Test Account",
        account_type: "asset",
        normal_balance: "debit",
        description: "Created through HTML form",
        is_active: "on"
      }
    )

    expect(created.status).toBe(303)
    expect(created.headers.get("location")).toBe("/dashboard/accounts")
    expect(created.headers.get("set-cookie")).toContain(
      "Account created successfully"
    )

    const list = await handler(new Request(
      "http://localhost/dashboard/accounts?search=UI%20Test",
      { headers: { cookie } }
    ))
    const body = await list.text()
    expect(body).toContain("9-9001")
    expect(body).toContain("UI Test Account")
    expect(body).toContain("Created through HTML form")
  })
})
