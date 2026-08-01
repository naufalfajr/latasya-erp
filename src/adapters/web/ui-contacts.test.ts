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
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-contacts-"))
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
        "contacts-password",
        "contacts-password"
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

describe("server-rendered contacts", () => {
  test("renders route capacity and legacy contact validation", async () => {
    const { handler, cookie, csrf } = await setup()
    const list = await handler(new Request(
      "http://localhost/dashboard/contacts?sort=route&order=desc",
      { headers: { cookie } }
    ))
    const listBody = await list.text()
    expect(list.status).toBe(200)
    expect(listBody).toContain("West route")
    expect(listBody).toContain("LA001")
    expect(listBody).toContain("0 / 13")
    expect(listBody).toContain("sort=route")

    const invalid = await postForm(
      handler,
      "/dashboard/contacts",
      cookie,
      {
        csrf_token: csrf,
        class: "123456",
        distance_km: "-1"
      }
    )
    const body = await invalid.text()
    expect(body).toContain("Name is required")
    expect(body).toContain("Contact type is required")
    expect(body).toContain("Class must be 5 characters or fewer")
    expect(body).toContain("Distance must be 0 or greater")
  })

  test("creates a routed contact and manages its public portal code", async () => {
    const { handler, cookie, csrf } = await setup()
    const created = await postForm(
      handler,
      "/dashboard/contacts",
      cookie,
      {
        csrf_token: csrf,
        name: "Andi Family",
        contact_type: "customer",
        phone: "08123456789",
        email: "andi@example.test",
        address: "Jl. Test",
        notes: "Pickup near gate",
        maps_link: "https://maps.example.test/andi",
        class: "6B",
        distance_km: "4,5",
        route_id: "1",
        has_sibling_discount: "on",
        is_active: "on"
      }
    )
    expect(created.status).toBe(303)
    expect(created.headers.get("set-cookie")).toContain(
      "Contact created successfully"
    )

    const list = await handler(new Request(
      "http://localhost/dashboard/contacts?search=Andi",
      { headers: { cookie } }
    ))
    const body = await list.text()
    expect(body).toContain("Andi Family")
    expect(body).toContain("West")
    expect(body).toContain("Open map")
    const id = /\/dashboard\/contacts\/(\d+)\/edit/.exec(body)?.[1] ?? ""
    expect(id).not.toBe("")

    const portal = await postForm(
      handler,
      `/dashboard/contacts/${id}/portal-code`,
      cookie,
      { csrf_token: csrf, portal_code: "andi-829" }
    )
    expect(portal.status).toBe(303)
    expect(portal.headers.get("set-cookie")).toContain(
      "Link portal berhasil disimpan: andi-829"
    )

    const edit = await handler(new Request(
      `http://localhost/dashboard/contacts/${id}/edit`,
      { headers: { cookie } }
    ))
    const editBody = await edit.text()
    expect(edit.status).toBe(200)
    expect(editBody).toContain("http://localhost/p/andi-829")
    expect(editBody).toContain('value="andi-829"')
    expect(editBody).toContain('value="1" selected')
  })
})
