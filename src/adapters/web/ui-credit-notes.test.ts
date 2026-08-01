import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { Accounts } from "../../domain/accounting/accounts.ts"
import { Invoices } from "../../domain/accounting/invoices.ts"
import { Authentication } from "../../domain/auth/authentication.ts"
import { Contacts } from "../../domain/contacts/contacts.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { makeRouter } from "./router.ts"

const directories: Array<string> = []
const disposers: Array<() => Promise<void>> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-cn-"))
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
      "credit-note-password",
      "credit-note-password"
    )
    const contacts = yield* Contacts
    const customer = yield* contacts.create({
      name: "Keluarga Rina",
      contactType: "customer",
      phone: "",
      email: "",
      address: "",
      notes: "",
      mapsLink: "",
      className: "5A",
      distanceKm: 2,
      hasSiblingDiscount: false,
      isReturnOnly: false,
      isActive: true
    })
    const accounts = yield* Accounts
    const revenue = (yield* accounts.list({
      type: "revenue",
      search: ""
    })).find((item) => item.is_active)!
    const invoices = yield* Invoices
    const invoice = yield* invoices.create({
      contactId: customer.id,
      invoiceDate: "2026-07-01",
      dueDate: "2026-07-11",
      taxAmount: 0,
      notes: "",
      lines: [{
        description: "Antar jemput",
        quantity: 100,
        unitPrice: 350000,
        accountId: revenue.id
      }]
    }, session.user.id)
    const sent = yield* invoices.send(invoice.id, session.user.id)
    return { session, customer, revenue, invoice: sent }
  }).pipe(Effect.provide(layer)))
  const web = HttpApp.toWebHandlerLayer(makeRouter("test", true), layer)
  disposers.push(web.dispose)
  return {
    handler: web.handler,
    cookie: `session_id=${data.session.sessionId}`,
    csrf: data.session.csrfToken,
    customer: data.customer.id,
    revenue: data.revenue.id,
    invoice: data.invoice
  }
}

const submit = (
  handler: (request: Request) => Promise<Response>,
  path: string,
  cookie: string,
  body: URLSearchParams
) => handler(new Request(`http://localhost${path}`, {
  method: "POST",
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

describe("server-rendered credit notes", () => {
  test("prefills an invoice and runs issue and void", async () => {
    const { handler, cookie, csrf, customer, revenue, invoice } =
      await setup()
    const blank = await handler(new Request(
      "http://localhost/dashboard/credit-notes/new",
      { headers: { cookie } }
    ))
    expect(blank.status).toBe(200)
    const form = await handler(new Request(
      `http://localhost/dashboard/credit-notes/new?invoice_id=${invoice.id}`,
      { headers: { cookie } }
    ))
    const formBody = await form.text()
    expect(form.status).toBe(200)
    expect(formBody).toContain(`Pre-filled from invoice`)
    expect(formBody).toContain(invoice.invoice_number)
    expect(formBody).toContain("Antar jemput")

    const created = await submit(
      handler,
      "/dashboard/credit-notes",
      cookie,
      new URLSearchParams([
        ["csrf_token", csrf],
        ["contact_id", String(customer)],
        ["invoice_id", String(invoice.id)],
        ["cn_date", "2026-07-05"],
        ["reason", "discount"],
        ["line_description", "Discount correction"],
        ["line_quantity", "1"],
        ["line_unit_price", "100000"],
        ["line_account_id", String(revenue)],
        ["tax_amount", "0"],
        ["notes", "Approved discount"]
      ])
    )
    expect(created.status).toBe(303)
    const location = created.headers.get("location") ?? ""
    const view = await handler(new Request(`http://localhost${location}`, {
      headers: { cookie }
    }))
    const viewBody = await view.text()
    expect(viewBody).toContain("Discount correction")
    expect(viewBody).toContain("Rp 100.000")

    const issued = await submit(
      handler,
      `${location}/issue`,
      cookie,
      new URLSearchParams({ csrf_token: csrf })
    )
    expect(issued.status).toBe(303)
    expect(issued.headers.get("set-cookie")).toContain(
      "Credit note issued %E2%80%94 journal entry posted"
    )
    const invoiceView = await handler(new Request(
      `http://localhost/dashboard/invoices/${invoice.id}`,
      { headers: { cookie } }
    ))
    const invoiceBody = await invoiceView.text()
    expect(invoiceBody).toContain("Credited")
    expect(invoiceBody).toContain("Rp 250.000")

    const voided = await submit(
      handler,
      `${location}/void`,
      cookie,
      new URLSearchParams({ csrf_token: csrf })
    )
    expect(voided.status).toBe(303)
    const afterVoid = await handler(new Request(
      `http://localhost${location}`,
      { headers: { cookie } }
    ))
    expect(await afterVoid.text()).toContain(">void</span>")
  })
})
