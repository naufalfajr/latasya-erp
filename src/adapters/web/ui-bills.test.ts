import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { Accounts } from "../../domain/accounting/accounts.ts"
import { Authentication } from "../../domain/auth/authentication.ts"
import { Contacts } from "../../domain/contacts/contacts.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { makeRouter } from "./router.ts"

const directories: Array<string> = []
const disposers: Array<() => Promise<void>> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-bills-"))
  directories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const layer = runtimeLayer(databasePath)
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(layer)))
  const data = await Effect.runPromise(Effect.gen(function*() {
    const authentication = yield* Authentication
    const session = yield* authentication.login("admin", "admin")
    yield* authentication.changePassword(
      session.user,
      "admin",
      "bills-password",
      "bills-password"
    )
    const contacts = yield* Contacts
    const supplier = yield* contacts.create({
      name: "Bengkel Maju",
      contactType: "supplier",
      phone: "",
      email: "",
      address: "",
      notes: "",
      mapsLink: "",
      className: "",
      distanceKm: 0,
      hasSiblingDiscount: false,
      isReturnOnly: false,
      isActive: true
    })
    const accounts = yield* Accounts
    const [expenses, assets] = yield* Effect.all([
      accounts.list({ type: "expense", search: "" }),
      accounts.list({ type: "asset", search: "" })
    ])
    return {
      session,
      supplier,
      expense: expenses.find((item) => item.is_active)!,
      asset: assets.find((item) => item.is_active)!
    }
  }).pipe(Effect.provide(layer)))
  const web = HttpApp.toWebHandlerLayer(makeRouter("test", true), layer)
  disposers.push(web.dispose)
  return {
    handler: web.handler,
    cookie: `session_id=${data.session.sessionId}`,
    csrf: data.session.csrfToken,
    supplier: data.supplier.id,
    expense: data.expense.id,
    asset: data.asset.id
  }
}

const submit = (
  handler: (request: Request) => Promise<Response>,
  path: string,
  cookie: string,
  body: URLSearchParams,
  method = "POST"
) => handler(new Request(`http://localhost${path}`, {
  method,
  headers: {
    cookie,
    "content-type": "application/x-www-form-urlencoded"
  },
  body
}))

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of directories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("server-rendered bills", () => {
  test("runs the complete payable lifecycle", async () => {
    const { handler, cookie, csrf, supplier, expense, asset } = await setup()
    const formPage = await handler(new Request(
      "http://localhost/dashboard/bills/new",
      { headers: { cookie } }
    ))
    expect(formPage.status).toBe(200)
    expect(await formPage.text()).toContain("Bengkel Maju")

    const invalid = await submit(
      handler,
      "/dashboard/bills",
      cookie,
      new URLSearchParams({ csrf_token: csrf })
    )
    expect(await invalid.text()).toContain(
      'name="contact_id" class="select select-error"'
    )

    const created = await submit(
      handler,
      "/dashboard/bills",
      cookie,
      new URLSearchParams([
        ["csrf_token", csrf],
        ["contact_id", String(supplier)],
        ["bill_date", "2026-07-01"],
        ["due_date", "2026-07-15"],
        ["line_description", "Service kendaraan"],
        ["line_quantity", "1"],
        ["line_unit_price", "500000"],
        ["line_account_id", String(expense)],
        ["tax_amount", "0"],
        ["notes", "Invoice supplier"]
      ])
    )
    expect(created.status).toBe(303)
    const location = created.headers.get("location") ?? ""
    const view = await handler(new Request(`http://localhost${location}`, {
      headers: { cookie }
    }))
    const viewBody = await view.text()
    expect(viewBody).toContain("Service kendaraan")
    expect(viewBody).toContain("Rp 500.000")

    const received = await submit(
      handler,
      `${location}/receive`,
      cookie,
      new URLSearchParams({ csrf_token: csrf })
    )
    expect(received.status).toBe(303)
    expect(received.headers.get("set-cookie")).toContain(
      "Bill received %E2%80%94 journal entry created"
    )

    const payment = await submit(
      handler,
      `${location}/payment`,
      cookie,
      new URLSearchParams({
        csrf_token: csrf,
        amount: "200000",
        payment_date: "2026-07-05",
        payment_account: String(asset)
      })
    )
    expect(payment.status).toBe(303)
    const partial = await handler(new Request(`http://localhost${location}`, {
      headers: { cookie }
    }))
    const partialBody = await partial.text()
    expect(partialBody).toContain(">partial</span>")
    expect(partialBody).toContain("Rp 300.000")
  })
})
