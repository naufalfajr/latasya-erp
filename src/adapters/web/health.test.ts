import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { makeRouter } from "./router.ts"

const temporaryDirectories: Array<string> = []
const disposers: Array<() => Promise<void>> = []

const temporaryDatabasePath = () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-health-"))
  temporaryDirectories.push(directory)
  return join(directory, "latasya.db")
}

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

const makeHandler = (databasePath: string) => {
  const web = HttpApp.toWebHandlerLayer(
    makeRouter("test-sha"),
    runtimeLayer(databasePath)
  )
  disposers.push(web.dispose)
  return web.handler
}

describe("GET /healthz", () => {
  test("matches the current success response", async () => {
    const databasePath = temporaryDatabasePath()
    await Effect.runPromise(migrateDatabase(databasePath))
    const response = await makeHandler(databasePath)(
      new Request("http://localhost/healthz")
    )

    expect(response.status).toBe(200)
    expect(response.headers.get("content-type")).toBe(
      "text/plain; charset=utf-8"
    )
    expect(await response.text()).toBe(
      "ok version=test-sha migrations=23\n"
    )
  })

  test("matches the current database failure response", async () => {
    const response = await makeHandler(temporaryDatabasePath())(
      new Request("http://localhost/healthz")
    )

    expect(response.status).toBe(503)
    expect(await response.text()).toBe("db unreachable\n")
  })
})
