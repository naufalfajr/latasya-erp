import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { SqlClient } from "@effect/sql"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { Accounts } from "../../domain/accounting/accounts.ts"
import { Invoices } from "../../domain/accounting/invoices.ts"
import { Contacts } from "../../domain/contacts/contacts.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { makeRouter } from "./router.ts"

const directories: Array<string> = []
const disposers: Array<() => Promise<void>> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-public-"))
  directories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const layer = runtimeLayer(databasePath)
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(layer)))
  const data = await Effect.runPromise(Effect.gen(function*() {
    const contacts = yield* Contacts
    const makeContact = (name: string, phone: string) => contacts.create({
      name,
      contactType: "customer",
      phone,
      email: "",
      address: "",
      notes: "",
      mapsLink: "",
      className: "6A",
      distanceKm: 3,
      hasSiblingDiscount: false,
      isReturnOnly: false,
      isActive: true
    })
    const [first, sibling, outsider] = yield* Effect.all([
      makeContact("Alya", "0812-3456-7890"),
      makeContact("Bima", "+62 812 3456 7890"),
      makeContact("Citra", "0822-1111-2222")
    ])
    const accounts = yield* Accounts
    const revenue = (yield* accounts.list({
      type: "revenue",
      search: ""
    })).find((account) => account.is_active)
    if (revenue === undefined) return yield* Effect.die("revenue account missing")
    const invoices = yield* Invoices
    const create = (contactId: number, date: string, description: string) =>
      invoices.create({
        contactId,
        invoiceDate: date,
        dueDate: `${date.slice(0, 8)}20`,
        taxAmount: 0,
        notes: "",
        lines: [{
          description,
          quantity: 100,
          unitPrice: 350000,
          accountId: revenue.id
        }]
      }, 1)
    const firstDraft = yield* create(
      first.id,
      "2026-07-01",
      "Draft service"
    )
    const firstIssued = yield* invoices.send(
      (yield* create(first.id, "2026-06-01", "June service")).id,
      1
    )
    const siblingIssued = yield* invoices.send(
      (yield* create(sibling.id, "2026-05-01", "May service")).id,
      1
    )
    const outsiderIssued = yield* invoices.send(
      (yield* create(outsider.id, "2026-04-01", "April service")).id,
      1
    )
    const sql = yield* SqlClient.SqlClient
    yield* sql`
      UPDATE contacts SET portal_code = 'family-829' WHERE id = ${first.id}
    `
    yield* sql`
      UPDATE contacts SET portal_code = 'other-771' WHERE id = ${outsider.id}
    `
    yield* sql`
      UPDATE company_profile
      SET
        name = 'Latasya Transport',
        phone = '0812-9999-0000',
        address = 'Jl. Sekolah 1',
        bank_name = 'Bank Test',
        bank_account_number = '123456789',
        bank_account_holder = 'PT Latasya'
      WHERE id = 1
    `
    return {
      firstDraft,
      firstIssued,
      siblingIssued,
      outsiderIssued
    }
  }).pipe(Effect.provide(layer)))
  const web = HttpApp.toWebHandlerLayer(makeRouter("test", true), layer)
  disposers.push(web.dispose)
  return { handler: web.handler, ...data }
}

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of directories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("public home and family portal", () => {
  test("renders the company home and family invoice portal", async () => {
    const {
      handler,
      firstDraft,
      firstIssued,
      siblingIssued
    } = await setup()
    const home = await handler(new Request("http://localhost/"))
    const homeBody = await home.text()
    expect(home.status).toBe(200)
    expect(homeBody).toContain("Latasya Transport")
    expect(homeBody).toContain("https://wa.me/6281299990000?")
    expect(homeBody).toContain("/dashboard/login")

    const invalid = await handler(new Request("http://localhost/p/nope"))
    expect(invalid.status).toBe(404)
    expect(invalid.headers.get("cache-control")).toBe("private, no-store")
    expect(await invalid.text()).toContain("Link Tidak Valid")

    const portal = await handler(new Request(
      "http://erp.local/p/FAMILY829"
    ))
    const body = await portal.text()
    expect(portal.status).toBe(200)
    expect(portal.headers.get("cache-control")).toBe("private, no-store")
    expect(body).toContain("Alya &amp; Bima")
    expect(body).toContain(firstIssued.invoice_number)
    expect(body).toContain(siblingIssued.invoice_number)
    expect(body).not.toContain(firstDraft.invoice_number)
    expect(body).toContain("http://erp.local/p/family-829")
    expect(body).toContain(
      `/p/family-829/invoice/${firstIssued.id}/pdf`
    )
    expect(body).toContain("Alya Juni 2026")
    expect(body).toContain("123456789")
    expect(body).toContain("Konfirmasi Sudah Bayar")
  })

  test("protects invoice PDFs by family and throttles guesses", async () => {
    const { handler, firstIssued, outsiderIssued } = await setup()
    const owned = await handler(new Request(
      `http://localhost/p/family-829/invoice/${firstIssued.id}/pdf`
    ))
    expect(owned.status).toBe(200)
    expect(owned.headers.get("content-type")).toBe("application/pdf")
    expect(owned.headers.get("cache-control")).toBe("private, no-store")
    expect((await owned.arrayBuffer()).byteLength).toBeGreaterThan(100)

    const foreign = await handler(new Request(
      `http://localhost/p/family-829/invoice/${outsiderIssued.id}/pdf`
    ))
    expect(foreign.status).toBe(404)
    expect(foreign.headers.get("cache-control")).toBe("private, no-store")

    let response: Response | undefined
    for (let attempt = 0; attempt < 6; attempt++) {
      response = await handler(new Request(
        `http://localhost/p/invalid-${attempt}`,
        { headers: { "cf-connecting-ip": "203.0.113.9" } }
      ))
    }
    expect(response?.status).toBe(429)
    expect(response?.headers.get("retry-after")).toBe("3600")
  })
})
