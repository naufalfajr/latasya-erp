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
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-reports-"))
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
        "reports-password",
        "reports-password"
      )
      return loggedIn
    }).pipe(Effect.provide(layer))
  )
  const web = HttpApp.toWebHandlerLayer(makeRouter("test", true), layer)
  disposers.push(web.dispose)
  return {
    handler: web.handler,
    cookie: `session_id=${session.sessionId}`
  }
}

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("server-rendered reports", () => {
  test("renders every report with legacy titles and date inputs", async () => {
    const { handler, cookie } = await setup()
    const cases = [
      ["/dashboard/reports/trial-balance", "Trial Balance"],
      ["/dashboard/reports/profit-loss", "Profit & Loss"],
      ["/dashboard/reports/balance-sheet", "Balance Sheet"],
      ["/dashboard/reports/cash-flow", "Cash Flow"],
      ["/dashboard/reports/general-ledger", "General Ledger"]
    ] as const

    for (const [path, title] of cases) {
      const response = await handler(new Request(`http://localhost${path}`, {
        headers: { cookie }
      }))
      const body = await response.text()
      expect(response.status).toBe(200)
      expect(body).toContain(
        `<title>${title.replace("&", "&amp;")} — Latasya ERP</title>`
      )
      expect(body).toContain('type="date"')
    }
  })

  test("preserves supplied report date filters", async () => {
    const { handler, cookie } = await setup()
    const response = await handler(new Request(
      "http://localhost/dashboard/reports/profit-loss" +
        "?from=2026-01-02&to=2026-03-04",
      { headers: { cookie } }
    ))
    const body = await response.text()

    expect(body).toContain('value="2026-01-02"')
    expect(body).toContain('value="2026-03-04"')
  })
})
