import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { Authentication } from "../../domain/auth/authentication.ts"
import type {
  GoogleCalendarConfig
} from "../../domain/school-calendar/school-calendar.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { makeRouter } from "./router.ts"

const directories: Array<string> = []
const disposers: Array<() => Promise<void>> = []
const setup = async (googleCalendar?: GoogleCalendarConfig) => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-settings-"))
  directories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const layer = runtimeLayer(databasePath, googleCalendar)
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(layer)))
  const session = await Effect.runPromise(Effect.gen(function*() {
    const authentication = yield* Authentication
    const loggedIn = yield* authentication.login("admin", "admin")
    yield* authentication.changePassword(
      loggedIn.user,
      "admin",
      "settings-password",
      "settings-password"
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
  values: Array<[string, string]>
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
  for (const directory of directories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("server-rendered settings", () => {
  test("updates company, calendar, tokens, and renders audit", async () => {
    const { handler, cookie, csrf } = await setup()
    const company = await submit(
      handler,
      "/dashboard/settings/company",
      cookie,
      [
        ["csrf_token", csrf],
        ["name", "Latasya Baru"],
        ["tagline", "Transport"],
        ["phone", "0812"]
      ]
    )
    expect(company.status).toBe(303)
    const companyPage = await handler(new Request(
      "http://localhost/dashboard/settings/company",
      { headers: { cookie } }
    ))
    expect(await companyPage.text()).toContain('value="Latasya Baru"')

    const invalidClosure = await submit(
      handler,
      "/dashboard/settings/school-calendar/closures?month=2026-07",
      cookie,
      [["csrf_token", csrf]]
    )
    const invalidBody = await invalidClosure.text()
    expect(invalidBody).toContain("Title is required")
    expect(invalidBody).toContain("Valid start date is required")

    const closure = await submit(
      handler,
      "/dashboard/settings/school-calendar/closures?month=2026-07",
      cookie,
      [
        ["csrf_token", csrf],
        ["title", "Libur semester"],
        ["start_date", "2026-07-01"],
        ["end_date", "2026-07-03"]
      ]
    )
    expect(closure.status).toBe(303)
    const calendar = await handler(new Request(
      "http://localhost/dashboard/settings/school-calendar?month=2026-07",
      { headers: { cookie } }
    ))
    const calendarBody = await calendar.text()
    expect(calendarBody).toContain("Libur semester")
    expect(calendarBody).toContain(
      "Google OAuth is not configured on the server."
    )

    const token = await submit(
      handler,
      "/dashboard/settings/api-tokens",
      cookie,
      [
        ["csrf_token", csrf],
        ["name", "Integration"],
        ["scopes", "reports.view"]
      ]
    )
    expect(token.status).toBe(303)
    const tokenCookie = token.headers.get("set-cookie") ?? ""
    expect(tokenCookie).toContain("lat_")
    const created = await handler(new Request(
      "http://localhost/dashboard/settings/api-tokens/created",
      { headers: { cookie: `${cookie}; ${tokenCookie.split(";")[0]}` } }
    ))
    const createdBody = await created.text()
    expect(created.status).toBe(200)
    expect(created.headers.get("cache-control")).toBe("no-store, private")
    expect(createdBody).toContain("ONLY time")
    expect(createdBody).toContain("lat_")
    expect(createdBody).not.toContain("alert alert-info")

    const tokens = await handler(new Request(
      "http://localhost/dashboard/settings/api-tokens",
      { headers: { cookie } }
    ))
    const tokensBody = await tokens.text()
    expect(tokensBody).toContain("Integration")
    expect(tokensBody).toContain("reports.view")

    const audit = await handler(new Request(
      "http://localhost/dashboard/audit?action=api_token.",
      { headers: { cookie } }
    ))
    const auditBody = await audit.text()
    expect(audit.status).toBe(200)
    expect(auditBody).toContain("api_token.create")
    expect(auditBody).toContain("admin")
  })

  test("preserves Google Calendar callback validation redirects", async () => {
    const disabled = await setup()
    const unavailable = await disabled.handler(new Request(
      "http://localhost/dashboard/integrations/google-calendar/callback" +
      "?code=abc&state=xyz&month=2026-06",
      { headers: { cookie: disabled.cookie } }
    ))
    expect(unavailable.status).toBe(303)
    expect(unavailable.headers.get("location")).toBe(
      "/dashboard/settings/school-calendar?month=2026-06"
    )
    expect(unavailable.headers.get("set-cookie")).toContain(
      "Google Calendar OAuth is not configured"
    )

    const enabled = await setup({
      clientId: "client",
      clientSecret: "secret",
      redirectUrl:
        "http://localhost/dashboard/integrations/google-calendar/callback",
      tokenUrl: "http://127.0.0.1:1/token"
    })
    const cancelled = await enabled.handler(new Request(
      "http://localhost/dashboard/integrations/google-calendar/callback" +
      "?error=access_denied&month=2026-06",
      { headers: { cookie: enabled.cookie } }
    ))
    expect(cancelled.status).toBe(303)
    expect(cancelled.headers.get("set-cookie")).toContain(
      "Google Calendar connection cancelled"
    )

    const missing = await enabled.handler(new Request(
      "http://localhost/dashboard/integrations/google-calendar/callback" +
      "?state=xyz&month=2026-06",
      { headers: { cookie: enabled.cookie } }
    ))
    expect(missing.status).toBe(303)
    expect(missing.headers.get("set-cookie")).toContain(
      "Google Calendar callback was missing required values"
    )

    const expired = await enabled.handler(new Request(
      "http://localhost/dashboard/integrations/google-calendar/callback" +
      "?code=abc&state=unknown&month=2026-06",
      { headers: { cookie: enabled.cookie } }
    ))
    expect(expired.status).toBe(303)
    expect(expired.headers.get("set-cookie")).toContain(
      "Google Calendar connection expired. Please try again."
    )
  })
})
