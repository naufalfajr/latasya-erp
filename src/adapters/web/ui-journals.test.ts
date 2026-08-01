import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { Accounts } from "../../domain/accounting/accounts.ts"
import { Authentication } from "../../domain/auth/authentication.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { makeRouter } from "./router.ts"

const temporaryDirectories: Array<string> = []
const disposers: Array<() => Promise<void>> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-journals-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const layer = runtimeLayer(databasePath)
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(layer)))
  const result = await Effect.runPromise(
    Effect.gen(function*() {
      const authentication = yield* Authentication
      const loggedIn = yield* authentication.login("admin", "admin")
      yield* authentication.changePassword(
        loggedIn.user,
        "admin",
        "journals-password",
        "journals-password"
      )
      const accounts = yield* Accounts
      const values = yield* accounts.list({ type: "", search: "" })
      return { loggedIn, accounts: values }
    }).pipe(Effect.provide(layer))
  )
  const web = HttpApp.toWebHandlerLayer(makeRouter("test", true), layer)
  disposers.push(web.dispose)
  return {
    handler: web.handler,
    cookie: `session_id=${result.loggedIn.sessionId}`,
    csrf: result.loggedIn.csrfToken,
    accountIds: result.accounts.slice(0, 2).map((account) => account.id)
  }
}

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("server-rendered journal entries", () => {
  test("renders a two-line form and legacy balance validation", async () => {
    const { handler, cookie, csrf, accountIds } = await setup()
    const formPage = await handler(new Request(
      "http://localhost/dashboard/journals/new",
      { headers: { cookie } }
    ))
    const formBody = await formPage.text()
    expect(formPage.status).toBe(200)
    expect(formBody).toContain("<title>New Journal Entry — Latasya ERP</title>")
    expect(formBody.match(/name="line_account_id"/g)).toHaveLength(2)

    const body = new URLSearchParams({
      csrf_token: csrf,
      entry_date: "2026-07-26",
      description: "Unbalanced entry"
    })
    body.append("line_account_id", String(accountIds[0]))
    body.append("line_account_id", String(accountIds[1]))
    body.append("line_debit", "10.000")
    body.append("line_debit", "")
    body.append("line_credit", "")
    body.append("line_credit", "9.000")
    body.append("line_memo", "Debit")
    body.append("line_memo", "Credit")
    const invalid = await handler(new Request(
      "http://localhost/dashboard/journals",
      {
        method: "POST",
        headers: {
          cookie,
          "content-type": "application/x-www-form-urlencoded"
        },
        body
      }
    ))
    expect(invalid.status).toBe(200)
    expect(await invalid.text()).toContain(
      "Debits (10000) must equal credits (9000)"
    )
  })

  test("creates, lists, and views a balanced manual journal", async () => {
    const { handler, cookie, csrf, accountIds } = await setup()
    const body = new URLSearchParams({
      csrf_token: csrf,
      entry_date: "2026-07-26",
      description: "Browser journal"
    })
    body.append("line_account_id", String(accountIds[0]))
    body.append("line_account_id", String(accountIds[1]))
    body.append("line_debit", "125000")
    body.append("line_debit", "")
    body.append("line_credit", "")
    body.append("line_credit", "125000")
    body.append("line_memo", "Left")
    body.append("line_memo", "Right")
    const created = await handler(new Request(
      "http://localhost/dashboard/journals",
      {
        method: "POST",
        headers: {
          cookie,
          "content-type": "application/x-www-form-urlencoded"
        },
        body
      }
    ))
    const location = created.headers.get("location") ?? ""
    expect(created.status).toBe(303)
    expect(location).toMatch(/^\/dashboard\/journals\/\d+$/)
    expect(created.headers.get("set-cookie")).toContain(
      "Journal entry created successfully"
    )

    const view = await handler(new Request(`http://localhost${location}`, {
      headers: { cookie }
    }))
    const viewBody = await view.text()
    expect(view.status).toBe(200)
    expect(viewBody).toContain("Browser journal")
    expect(viewBody).toContain("Rp 125.000")
    expect(viewBody).toContain(">Edit</a>")

    const list = await handler(new Request(
      "http://localhost/dashboard/journals?search=Browser",
      { headers: { cookie } }
    ))
    expect(await list.text()).toContain("Browser journal")
  })
})
