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
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-dashboard-"))
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
        "dashboard-password",
        "dashboard-password"
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

describe("server-rendered dashboard", () => {
  test("redirects the bare dashboard path to its canonical slash", async () => {
    const { handler } = await setup()
    const response = await handler(new Request("http://localhost/dashboard"))
    expect(response.status).toBe(301)
    expect(response.headers.get("location")).toBe("/dashboard/")
  })

  test("renders dashboard data and safely embedded chart JSON", async () => {
    const { handler, cookie } = await setup()
    const response = await handler(new Request(
      "http://localhost/dashboard/?granularity=quarterly",
      { headers: { cookie } }
    ))
    const body = await response.text()

    expect(response.status).toBe(200)
    expect(body).toContain("<title>Dashboard — Latasya ERP</title>")
    expect(body).toContain(
      "<h1 class=\"text-2xl font-bold mb-6\">Dashboard</h1>"
    )
    expect(body).toContain("Cash Balance")
    expect(body).toContain("Rp 0")
    expect(body).toContain("last 6 quarters")
    expect(body).toContain("const trends = [")
    expect(body).toContain('"start_date":"')
    expect(body).not.toContain('"StartDate":"')
    expect(body).toContain('const basePath = "/dashboard";')
  })

  test("matches the legacy invalid-granularity response", async () => {
    const { handler, cookie } = await setup()
    const response = await handler(new Request(
      "http://localhost/dashboard/?granularity=weekly",
      { headers: { cookie } }
    ))

    expect(response.status).toBe(400)
    expect(await response.text()).toBe(
      "Invalid granularity parameter: use monthly or quarterly\n"
    )
  })
})
