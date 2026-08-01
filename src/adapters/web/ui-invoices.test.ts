import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { SqlClient } from "@effect/sql"
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

const temporaryDirectories: Array<string> = []
const disposers: Array<() => Promise<void>> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-invoices-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const layer = runtimeLayer(databasePath)
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(layer)))
  const prepared = await Effect.runPromise(
    Effect.gen(function*() {
      const authentication = yield* Authentication
      const loggedIn = yield* authentication.login("admin", "admin")
      yield* authentication.changePassword(
        loggedIn.user,
        "admin",
        "invoice-password",
        "invoice-password"
      )
      const contacts = yield* Contacts
      const customer = yield* contacts.create({
        name: "Keluarga Andi",
        contactType: "customer",
        phone: "0812-3456-7890",
        email: "andi@example.test",
        address: "Jl. Sekolah",
        notes: "",
        mapsLink: "",
        className: "6A",
        distanceKm: 3,
        hasSiblingDiscount: false,
        isReturnOnly: false,
        isActive: true
      })
      const accounts = yield* Accounts
      const [revenue, assets] = yield* Effect.all([
        accounts.list({ type: "revenue", search: "" }),
        accounts.list({ type: "asset", search: "" })
      ])
      const revenueAccount = revenue.find((account) => account.is_active)
      const assetAccount = assets.find((account) => account.is_active)
      if (revenueAccount === undefined || assetAccount === undefined) {
        return yield* Effect.die("seed accounts missing")
      }
      const sql = yield* SqlClient.SqlClient
      yield* sql`
        UPDATE company_profile
        SET
          default_revenue_account_id = ${revenueAccount.id},
          recurring_description_template = 'Antar jemput {month} {year}'
        WHERE id = 1
      `
      return {
        session: loggedIn,
        customer,
        revenueAccount,
        assetAccount
      }
    }).pipe(Effect.provide(layer))
  )
  const web = HttpApp.toWebHandlerLayer(makeRouter("test", true), layer)
  disposers.push(web.dispose)
  return {
    handler: web.handler,
    cookie: `session_id=${prepared.session.sessionId}`,
    csrf: prepared.session.csrfToken,
    customerId: prepared.customer.id,
    revenueId: prepared.revenueAccount.id,
    assetId: prepared.assetAccount.id
  }
}

const formRequest = (
  handler: (request: Request) => Promise<Response>,
  path: string,
  cookie: string,
  form: URLSearchParams,
  method = "POST"
) => handler(new Request(`http://localhost${path}`, {
  method,
  headers: {
    cookie,
    "content-type": "application/x-www-form-urlencoded"
  },
  body: form
}))

const invoiceForm = (
  csrf: string,
  customerId: number,
  revenueId: number,
  description = "Antar jemput Juli 2026"
) =>
  new URLSearchParams([
    ["csrf_token", csrf],
    ["contact_id", String(customerId)],
    ["invoice_date", "2026-06-01"],
    ["due_date", "2026-06-11"],
    ["line_description", description],
    ["line_account_id", String(revenueId)],
    ["line_quantity", "1"],
    ["line_unit_price", "350000"],
    ["tax_amount", "0"],
    ["notes", "Bayar sebelum jatuh tempo"]
  ])

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("server-rendered invoices", () => {
  test("renders the form and preserves legacy validation messages", async () => {
    const { handler, cookie, csrf } = await setup()
    const page = await handler(new Request(
      "http://localhost/dashboard/invoices/new",
      { headers: { cookie } }
    ))
    const pageBody = await page.text()
    expect(page.status).toBe(200)
    expect(pageBody).toContain("New Invoice")
    expect(pageBody).toContain("Keluarga Andi")
    expect(pageBody).toContain('data-price="350000"')
    expect(pageBody).toContain('data-default-account="')

    const invalid = await formRequest(
      handler,
      "/dashboard/invoices",
      cookie,
      new URLSearchParams({ csrf_token: csrf })
    )
    const body = await invalid.text()
    expect(invalid.status).toBe(200)
    expect(body).toContain("Customer is required")
    expect(body).toContain('name="invoice_date"\n' +
      '                        class="input input-error"')
    expect(body).toContain('name="due_date"\n' +
      '                        class="input input-error"')
    expect(body).toContain("At least one line item is required")
  })

  test("runs create, print, send, share, payment, and delete workflows", async () => {
    const {
      handler,
      cookie,
      csrf,
      customerId,
      revenueId,
      assetId
    } = await setup()
    const created = await formRequest(
      handler,
      "/dashboard/invoices",
      cookie,
      invoiceForm(csrf, customerId, revenueId)
    )
    expect(created.status).toBe(303)
    const location = created.headers.get("location") ?? ""
    expect(location).toMatch(/^\/dashboard\/invoices\/\d+$/)

    const detail = await handler(new Request(`http://localhost${location}`, {
      headers: { cookie }
    }))
    const detailBody = await detail.text()
    expect(detail.status).toBe(200)
    expect(detailBody).toContain("Keluarga Andi")
    expect(detailBody).toContain("Antar jemput Juli 2026")
    expect(detailBody).toContain("Rp 350.000")
    expect(detailBody).toContain("Mark as Sent")

    const print = await handler(new Request(
      `http://localhost${location}/print`,
      { headers: { cookie } }
    ))
    expect(print.status).toBe(200)
    expect(await print.text()).toContain("Latasya Transport")

    const pdf = await handler(new Request(
      `http://localhost${location}/pdf`,
      { headers: { cookie } }
    ))
    expect(pdf.status).toBe(200)
    expect(pdf.headers.get("content-type")).toBe("application/pdf")
    expect(new Uint8Array(await pdf.arrayBuffer()).slice(0, 5))
      .toEqual(new TextEncoder().encode("%PDF-"))

    const draftShare = await handler(new Request(
      `http://localhost${location}/whatsapp`,
      { headers: { cookie } }
    ))
    expect(draftShare.status).toBe(303)
    expect(draftShare.headers.get("set-cookie")).toContain(
      "Kirim invoice ini dulu"
    )

    const sent = await formRequest(
      handler,
      `${location}/send`,
      cookie,
      new URLSearchParams({ csrf_token: csrf })
    )
    expect(sent.status).toBe(303)

    const shared = await handler(new Request(
      `http://localhost${location}/whatsapp`,
      { headers: { cookie } }
    ))
    expect(shared.status).toBe(302)
    const whatsapp = shared.headers.get("location") ?? ""
    expect(whatsapp).toStartWith("https://wa.me/6281234567890?")
    expect(decodeURIComponent(whatsapp)).toContain("/p/keluarga-")

    const paid = await formRequest(
      handler,
      `${location}/payment`,
      cookie,
      new URLSearchParams({
        csrf_token: csrf,
        amount: "100000",
        payment_date: "2026-07-05",
        payment_account: String(assetId)
      })
    )
    expect(paid.status).toBe(303)
    const afterPayment = await handler(new Request(
      `http://localhost${location}`,
      { headers: { cookie } }
    ))
    const afterPaymentBody = await afterPayment.text()
    expect(afterPaymentBody).toContain(">partial</span>")
    expect(afterPaymentBody).toContain("Rp 100.000")
    expect(afterPaymentBody).toContain("Rp 250.000")

    const second = await formRequest(
      handler,
      "/dashboard/invoices",
      cookie,
      invoiceForm(csrf, customerId, revenueId, "Draft to delete")
    )
    const secondLocation = second.headers.get("location") ?? ""
    const deleted = await formRequest(
      handler,
      secondLocation,
      cookie,
      new URLSearchParams({ csrf_token: csrf }),
      "DELETE"
    )
    expect(deleted.status).toBe(303)
    expect(deleted.headers.get("location")).toBe("/dashboard/invoices")
  })

  test("bulk actions and recurring generation use legacy flashes", async () => {
    const {
      handler,
      cookie,
      csrf,
      customerId,
      revenueId
    } = await setup()
    const created = await formRequest(
      handler,
      "/dashboard/invoices",
      cookie,
      invoiceForm(csrf, customerId, revenueId, "Bulk invoice")
    )
    const id = (created.headers.get("location") ?? "").split("/").at(-1) ?? ""

    const bulk = await formRequest(
      handler,
      "/dashboard/invoices/bulk-send",
      cookie,
      new URLSearchParams([
        ["csrf_token", csrf],
        ["ids", id]
      ])
    )
    expect(bulk.status).toBe(303)
    expect(bulk.headers.get("set-cookie")).toContain(
      "Marked 1 invoice(s) as sent"
    )

    const recurring = await formRequest(
      handler,
      "/dashboard/invoices/generate-recurring",
      cookie,
      new URLSearchParams({ csrf_token: csrf })
    )
    expect(recurring.status).toBe(303)
    expect(recurring.headers.get("set-cookie")).toContain(
      "Generated 1 draft invoice(s)"
    )
  })
})
