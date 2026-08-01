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

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("HTMX line partials", () => {
  test("renders all four legacy row fragments from active accounts", async () => {
    const directory = mkdtempSync(join(tmpdir(), "latasya-ui-partials-"))
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
          "partials-password",
          "partials-password"
        )
        return loggedIn
      }).pipe(Effect.provide(layer))
    )
    const web = HttpApp.toWebHandlerLayer(makeRouter("test", true), layer)
    disposers.push(web.dispose)
    const cookie = `session_id=${session.sessionId}`

    const cases = [
      ["/dashboard/htmx/journal-line", "line_debit", "Select account"],
      ["/dashboard/htmx/invoice-line", "line_quantity", "Account"],
      ["/dashboard/htmx/bill-line", "line_unit_price", "Account"],
      ["/dashboard/htmx/credit-note-line", "line_description", "Account"]
    ] as const
    for (const [path, input, label] of cases) {
      const response = await web.handler(new Request(
        `http://localhost${path}`,
        { headers: { cookie } }
      ))
      const body = await response.text()
      expect(response.status).toBe(200)
      expect(body.trimStart()).toStartWith("<tr>")
      expect(body).toContain(`name="${input}"`)
      expect(body).toContain(label)
      expect(body).not.toContain("<!DOCTYPE html>")
    }
  })
})
