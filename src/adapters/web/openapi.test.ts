import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { mkdtempSync, readFileSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
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

describe("OpenAPI", () => {
  test("serves the existing contract unchanged behind authentication", async () => {
    const directory = mkdtempSync(join(tmpdir(), "latasya-openapi-"))
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
    const unauthorized = await web.handler(new Request(
      "http://localhost/api/v1/openapi.yaml"
    ))
    const login = await web.handler(new Request(
      "http://localhost/api/v1/auth/login",
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ username: "admin", password: "admin" })
      }
    ))
    const cookie = login.headers.get("set-cookie")?.split(";")[0] ?? ""
    const response = await web.handler(new Request(
      "http://localhost/api/v1/openapi.yaml",
      { headers: { cookie } }
    ))
    const body = await response.text()
    const expected = readFileSync(
      join(import.meta.dir, "../../../api/openapi.yaml"),
      "utf8"
    )

    expect(unauthorized.status).toBe(401)
    expect(response.status).toBe(200)
    expect(response.headers.get("content-type"))
      .toBe("application/yaml; charset=utf-8")
    expect(body).toBe(expected)
  })
})
