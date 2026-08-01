import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
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
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-auth-"))
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
  return web.handler
}

const postForm = (
  handler: (request: Request) => Promise<Response>,
  path: string,
  form: Readonly<Record<string, string>>,
  cookie = ""
) =>
  handler(new Request(`http://localhost${path}`, {
    method: "POST",
    headers: {
      "content-type": "application/x-www-form-urlencoded",
      ...(cookie === "" ? {} : { cookie })
    },
    body: new URLSearchParams(form)
  }))

const sessionCookie = (response: Response) =>
  response.headers.get("set-cookie")?.split(";")[0] ?? ""

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("server-rendered authentication", () => {
  test("renders the unchanged login page and validation state", async () => {
    const handler = await setup()
    const page = await handler(
      new Request("http://localhost/dashboard/login")
    )
    const invalid = await postForm(handler, "/dashboard/login", {
      username: `admin"><script>`,
      password: ""
    })
    const pageBody = await page.text()
    const invalidBody = await invalid.text()

    expect(page.status).toBe(200)
    expect(page.headers.get("content-type")).toBe("text/html; charset=utf-8")
    expect(pageBody).toContain("<title>Login — Latasya ERP</title>")
    expect(pageBody).toContain('action="/dashboard/login"')
    expect(invalid.status).toBe(200)
    expect(invalidBody).toContain("Username and password are required")
    expect(invalidBody).toContain("admin&#34;&gt;&lt;script&gt;")
  })

  test("logs in, enforces password change, validates CSRF, and logs out", async () => {
    const handler = await setup()
    const login = await postForm(handler, "/dashboard/login", {
      username: "admin",
      password: "admin"
    })
    const cookie = sessionCookie(login)

    expect(login.status).toBe(303)
    expect(login.headers.get("location")).toBe(
      "/dashboard/password/change"
    )
    expect(login.headers.get("set-cookie")).not.toContain("Secure")

    const page = await handler(new Request(
      "http://localhost/dashboard/password/change",
      { headers: { cookie } }
    ))
    const pageBody = await page.text()
    const csrf = /name="csrf_token" value="([^"]+)"/.exec(pageBody)?.[1] ?? ""
    expect(page.status).toBe(200)
    expect(pageBody).toContain(
      "You must change your password before continuing."
    )
    expect(csrf).toHaveLength(64)

    const missingCsrf = await postForm(
      handler,
      "/dashboard/password/change",
      {
        current_password: "admin",
        new_password: "new-password",
        confirm_password: "new-password"
      },
      cookie
    )
    expect(missingCsrf.status).toBe(403)
    expect(await missingCsrf.text()).toBe(
      "Forbidden: invalid CSRF token\n"
    )

    const changed = await postForm(
      handler,
      "/dashboard/password/change",
      {
        csrf_token: csrf,
        current_password: "admin",
        new_password: "new-password",
        confirm_password: "new-password"
      },
      cookie
    )
    expect(changed.status).toBe(303)
    expect(changed.headers.get("location")).toBe("/dashboard/")
    expect(changed.headers.get("set-cookie")).toContain(
      "Password updated successfully"
    )

    const passwordPage = await handler(new Request(
      "http://localhost/dashboard/password/change",
      { headers: { cookie } }
    ))
    const passwordBody = await passwordPage.text()
    const currentCsrf =
      /name="csrf_token" value="([^"]+)"/.exec(passwordBody)?.[1] ?? ""
    const logout = await postForm(
      handler,
      "/dashboard/logout",
      { csrf_token: currentCsrf },
      cookie
    )
    expect(logout.status).toBe(303)
    expect(logout.headers.get("location")).toBe("/dashboard/login")
    expect(logout.headers.get("set-cookie")).toContain("Max-Age=0")
  })
})
