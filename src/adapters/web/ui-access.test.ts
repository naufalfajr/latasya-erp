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

const directories: Array<string> = []
const disposers: Array<() => Promise<void>> = []
const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-access-"))
  directories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const layer = runtimeLayer(databasePath)
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(layer)))
  const session = await Effect.runPromise(Effect.gen(function*() {
    const authentication = yield* Authentication
    const loggedIn = yield* authentication.login("admin", "admin")
    yield* authentication.changePassword(
      loggedIn.user,
      "admin",
      "access-password",
      "access-password"
    )
    return loggedIn
  }).pipe(Effect.provide(layer)))
  const web = HttpApp.toWebHandlerLayer(makeRouter("test", true), layer)
  disposers.push(web.dispose)
  return {
    handler: web.handler,
    cookie: `session_id=${session.sessionId}`,
    csrf: session.csrfToken
  }
}
const submit = (
  handler: (request: Request) => Promise<Response>,
  path: string,
  cookie: string,
  values: Array<[string, string]>,
  method = "POST"
) => handler(new Request(`http://localhost${path}`, {
  method,
  headers: {
    cookie,
    "content-type": "application/x-www-form-urlencoded"
  },
  body: new URLSearchParams(values)
}))
afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of directories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("server-rendered access administration", () => {
  test("creates roles and users and preserves protections", async () => {
    const { handler, cookie, csrf } = await setup()
    const role = await submit(handler, "/dashboard/roles", cookie, [
      ["csrf_token", csrf],
      ["name", "operator"],
      ["description", "Operations"],
      ["capabilities", "contacts.manage"],
      ["capabilities", "invoices.manage"]
    ])
    expect(role.status).toBe(303)
    const roles = await handler(new Request(
      "http://localhost/dashboard/roles",
      { headers: { cookie } }
    ))
    const rolesBody = await roles.text()
    expect(rolesBody).toContain("operator")
    expect(rolesBody).toContain("contacts.manage")

    const invalid = await submit(handler, "/dashboard/users", cookie, [
      ["csrf_token", csrf],
      ["username", ""],
      ["full_name", ""],
      ["role", "missing"],
      ["password", "x"]
    ])
    const invalidBody = await invalid.text()
    expect(invalidBody).toContain("Username is required")
    expect(invalidBody).toContain("Full name is required")
    expect(invalidBody).toContain("Invalid role")
    expect(invalidBody).toContain("Password must be at least 4 characters")

    const created = await submit(handler, "/dashboard/users", cookie, [
      ["csrf_token", csrf],
      ["username", "rina"],
      ["full_name", "Rina Operator"],
      ["role", "operator"],
      ["password", "pass1234"],
      ["is_active", "on"]
    ])
    expect(created.status).toBe(303)
    const users = await handler(new Request(
      "http://localhost/dashboard/users",
      { headers: { cookie } }
    ))
    const usersBody = await users.text()
    expect(usersBody).toContain("rina")
    expect(usersBody).toContain("Rina Operator")

    const self = await submit(
      handler,
      "/dashboard/users/1",
      cookie,
      [
        ["csrf_token", csrf],
        ["full_name", "Administrator"],
        ["role", "admin"]
      ]
    )
    expect(self.status).toBe(303)
    const forbidden = await handler(new Request(
      "http://localhost/dashboard/roles/admin/edit",
      { headers: { cookie } }
    ))
    expect(forbidden.status).toBe(403)
  })
})
