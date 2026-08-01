import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { SqlClient } from "@effect/sql"
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
  const directory = mkdtempSync(join(tmpdir(), "latasya-invoices-api-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  await Effect.runPromise(
    seedDefaultAdmin.pipe(Effect.provide(runtimeLayer(databasePath)))
  )
  const fixtures = await Effect.runPromise(
    Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      yield* sql`
        INSERT INTO contacts (
          name, contact_type, is_active
        )
        VALUES ('API Invoice Customer', 'customer', 1)
      `
      const contacts = yield* sql<{ readonly id: number }>`
        SELECT last_insert_rowid() AS id
      `
      const revenue = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts WHERE code = '4-1001'
      `
      const bank = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts WHERE code = '1-1002'
      `
      return {
        contactId: contacts[0]?.id ?? 0,
        revenueId: revenue[0]?.id ?? 0,
        bankId: bank[0]?.id ?? 0
      }
    }).pipe(Effect.provide(runtimeLayer(databasePath)))
  )
  const web = HttpApp.toWebHandlerLayer(
    makeRouter("test", true),
    runtimeLayer(databasePath)
  )
  disposers.push(web.dispose)
  const login = await web.handler(new Request(
    "http://localhost/api/v1/auth/login",
    {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ username: "admin", password: "admin" })
    }
  ))
  return {
    databasePath,
    fixtures,
    cookie: login.headers.get("set-cookie")?.split(";")[0] ?? "",
    handler: web.handler
  }
}

const invoiceBody = (
  contactId: number,
  revenueId: number,
  unitPrice = "500000"
) => ({
  contact_id: contactId,
  invoice_date: "2026-05-10",
  due_date: "2026-06-10",
  tax_amount: "0",
  notes: "test invoice",
  lines: [{
    description: "School bus service",
    quantity: "1.00",
    unit_price: unitPrice,
    account_id: revenueId
  }]
})

const request = (
  handler: (request: Request) => Promise<Response>,
  authentication: string,
  method: string,
  path: string,
  body?: unknown,
  idempotencyKey?: string
) => handler(new Request(`http://localhost${path}`, {
  method,
  headers: {
    ...(authentication.startsWith("Bearer ")
      ? { authorization: authentication }
      : { cookie: authentication }),
    ...(body === undefined ? {} : { "content-type": "application/json" }),
    ...(idempotencyKey === undefined
      ? {}
      : { "idempotency-key": idempotencyKey })
  },
  ...(body === undefined ? {} : { body: JSON.stringify(body) })
}))

const createInvoice = async (
  handler: (request: Request) => Promise<Response>,
  cookie: string,
  contactId: number,
  revenueId: number,
  key?: string
) => {
  const response = await request(
    handler,
    cookie,
    "POST",
    "/api/v1/invoices",
    invoiceBody(contactId, revenueId),
    key
  )
  const text = await response.text()
  const body = JSON.parse(text) as {
    readonly data: {
      readonly id: number
      readonly invoice_number: string
      readonly status: string
    }
  }
  return { body, response, text }
}

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("invoices API", () => {
  test("creates, lists, gets, updates, audits, and deletes draft invoices", async () => {
    const { databasePath, fixtures, cookie, handler } = await setup()
    const created = await createInvoice(
      handler,
      cookie,
      fixtures.contactId,
      fixtures.revenueId
    )
    const id = created.body.data.id
    const list = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/invoices?status=draft&search=API&per_page=1"
    )
    const listBody = await list.json() as {
      readonly data: ReadonlyArray<{
        readonly lines?: unknown
        readonly paid_date: string
      }>
      readonly meta: { readonly total: number }
    }
    const get = await request(
      handler,
      cookie,
      "GET",
      `/api/v1/invoices/${id}`
    )
    const getBody = await get.json() as {
      readonly data: {
        readonly total: string
        readonly amount_due: string
        readonly lines: ReadonlyArray<{
          readonly quantity: string
          readonly amount: string
        }>
        readonly credit_notes?: unknown
      }
    }
    const update = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/invoices/${id}`,
      {
        ...invoiceBody(
          fixtures.contactId,
          fixtures.revenueId,
          "600000"
        ),
        tax_amount: "50000"
      }
    )
    const updateBody = await update.json() as {
      readonly data: { readonly total: string }
    }
    const pdf = await request(
      handler,
      cookie,
      "GET",
      `/api/v1/invoices/${id}/pdf`
    )
    const pdfBody = new Uint8Array(await pdf.arrayBuffer())
    const removed = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/invoices/${id}`
    )
    const audits = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE target_type = 'invoice' AND target_id = ${id}
          ORDER BY id
        `
        return rows.map((row) => row.action)
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(created.response.status).toBe(201)
    expect(created.body.data.invoice_number).toMatch(/^INV-\d{6}-\d{4}$/)
    expect(created.body.data.status).toBe("draft")
    expect(list.status).toBe(200)
    expect(listBody.meta.total).toBe(1)
    expect(listBody.data[0]?.lines).toBeUndefined()
    expect(listBody.data[0]?.paid_date).toBeDefined()
    expect(getBody.data).toMatchObject({
      total: "500000",
      amount_due: "500000"
    })
    expect(getBody.data.lines[0]).toMatchObject({
      quantity: "1.00",
      amount: "500000"
    })
    expect(getBody.data.credit_notes).toBeUndefined()
    expect(update.status).toBe(200)
    expect(updateBody.data.total).toBe("650000")
    expect(pdf.status).toBe(200)
    expect(pdf.headers.get("content-type")).toBe("application/pdf")
    expect(pdf.headers.get("content-disposition")).toContain(
      `${created.body.data.invoice_number}.pdf`
    )
    expect(new TextDecoder().decode(pdfBody.slice(0, 8))).toBe("%PDF-1.4")
    expect(removed.status).toBe(204)
    expect(audits).toEqual([
      "invoice.create",
      "invoice.update",
      "invoice.delete"
    ])
  })

  test("idempotently creates, sends, and records partial and full payments", async () => {
    const { databasePath, fixtures, cookie, handler } = await setup()
    const first = await createInvoice(
      handler,
      cookie,
      fixtures.contactId,
      fixtures.revenueId,
      "invoice-create"
    )
    const replay = await createInvoice(
      handler,
      cookie,
      fixtures.contactId,
      fixtures.revenueId,
      "invoice-create"
    )
    const id = first.body.data.id
    const send = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/invoices/${id}/send`,
      undefined,
      "invoice-send"
    )
    const sendText = await send.text()
    const sendReplay = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/invoices/${id}/send`,
      undefined,
      "invoice-send"
    )
    const sendReplayText = await sendReplay.text()
    const partialBody = {
      amount: "200000",
      payment_date: "2026-05-15",
      payment_account: fixtures.bankId
    }
    const partial = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/invoices/${id}/payment`,
      partialBody,
      "invoice-payment"
    )
    const partialText = await partial.text()
    const paymentReplay = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/invoices/${id}/payment`,
      partialBody,
      "invoice-payment"
    )
    const paymentReplayText = await paymentReplay.text()
    const paid = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/invoices/${id}/payment`,
      {
        amount: "300000",
        payment_date: "2026-05-20",
        payment_account: fixtures.bankId
      }
    )
    const paidBody = await paid.json() as {
      readonly data: {
        readonly status: string
        readonly amount_paid: string
      }
    }
    const counts = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const invoices = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count FROM invoices
        `
        const journals = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM journal_entries
          WHERE source_type = 'invoice'
        `
        const payments = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM payments
          WHERE payment_type = 'invoice' AND reference_id = ${id}
        `
        return {
          invoices: invoices[0]?.count ?? 0,
          journals: journals[0]?.count ?? 0,
          payments: payments[0]?.count ?? 0
        }
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(replay.text).toBe(first.text)
    expect(replay.body.data.id).toBe(id)
    expect(send.status).toBe(200)
    expect(sendReplay.status).toBe(200)
    expect(sendReplayText).toBe(sendText)
    expect(partial.status).toBe(200)
    expect(paymentReplayText).toBe(partialText)
    expect(paidBody.data).toMatchObject({
      status: "paid",
      amount_paid: "500000"
    })
    expect(counts).toEqual({
      invoices: 1,
      journals: 3,
      payments: 2
    })
  })

  test("enforces validation, scope, state conflicts, and overpayment", async () => {
    const { databasePath, fixtures, cookie, handler } = await setup()
    const invalid = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/invoices",
      { contact_id: 0, lines: [] }
    )
    const strict = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/invoices",
      {
        ...invoiceBody(fixtures.contactId, fixtures.revenueId),
        lines: [{
          ...invoiceBody(
            fixtures.contactId,
            fixtures.revenueId
          ).lines[0],
          unknown: true
        }]
      }
    )
    const created = await createInvoice(
      handler,
      cookie,
      fixtures.contactId,
      fixtures.revenueId
    )
    const id = created.body.data.id
    const draftPayment = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/invoices/${id}/payment`,
      {
        amount: "1",
        payment_date: "2026-05-15",
        payment_account: fixtures.bankId
      }
    )
    await request(
      handler,
      cookie,
      "POST",
      `/api/v1/invoices/${id}/send`
    )
    const editSent = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/invoices/${id}`,
      invoiceBody(fixtures.contactId, fixtures.revenueId)
    )
    const overpay = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/invoices/${id}/payment`,
      {
        amount: "500001",
        payment_date: "2026-05-15",
        payment_account: fixtures.bankId
      }
    )
    const plaintext = "lat_invoice-no-scope"
    const hash = new Bun.CryptoHasher("sha256")
      .update(plaintext)
      .digest("hex")
    await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        yield* sql`
          INSERT INTO api_tokens (
            user_id, name, token_prefix, token_hash, scopes
          )
          VALUES (
            1, 'invoice-no-scope', 'lat_invoice', ${hash},
            '["reports.view"]'
          )
        `
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const forbidden = await request(
      handler,
      `Bearer ${plaintext}`,
      "POST",
      "/api/v1/invoices",
      invoiceBody(fixtures.contactId, fixtures.revenueId)
    )

    expect(invalid.status).toBe(422)
    expect(strict.status).toBe(400)
    expect(draftPayment.status).toBe(409)
    expect(editSent.status).toBe(409)
    expect(overpay.status).toBe(422)
    expect((await overpay.json() as {
      readonly fields: { readonly amount: string }
    }).fields.amount).toBe("exceeds remaining balance")
    expect(forbidden.status).toBe(403)
  })

  test("bulk deletes drafts and bulk sends remaining draft invoices", async () => {
    const { fixtures, cookie, handler } = await setup()
    const first = await createInvoice(
      handler,
      cookie,
      fixtures.contactId,
      fixtures.revenueId
    )
    const second = await createInvoice(
      handler,
      cookie,
      fixtures.contactId,
      fixtures.revenueId
    )
    await request(
      handler,
      cookie,
      "POST",
      `/api/v1/invoices/${second.body.data.id}/send`
    )
    const deleted = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/invoices/bulk-delete",
      {
        ids: [first.body.data.id, second.body.data.id, 999999]
      }
    )
    const deletedBody = await deleted.json() as {
      readonly data: {
        readonly deleted: number
        readonly skipped: ReadonlyArray<number>
      }
    }
    const third = await createInvoice(
      handler,
      cookie,
      fixtures.contactId,
      fixtures.revenueId
    )
    const sent = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/invoices/bulk-send",
      {
        ids: [third.body.data.id, second.body.data.id, 999999]
      }
    )
    const sentBody = await sent.json() as {
      readonly data: {
        readonly sent: number
        readonly skipped: ReadonlyArray<number>
        readonly failed: ReadonlyArray<unknown>
      }
    }
    const empty = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/invoices/bulk-send",
      { ids: [] }
    )

    expect(deleted.status).toBe(200)
    expect(deletedBody.data.deleted).toBe(1)
    expect(deletedBody.data.skipped).toHaveLength(2)
    expect(sent.status).toBe(200)
    expect(sentBody.data.sent).toBe(1)
    expect(sentBody.data.skipped).toHaveLength(2)
    expect(sentBody.data.failed).toEqual([])
    expect(empty.status).toBe(422)
  })

  test("generates recurring drafts once per customer and month", async () => {
    const { databasePath, cookie, handler } = await setup()
    const generated = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/invoices/generate-recurring",
      undefined,
      "recurring-run"
    )
    const generatedText = await generated.text()
    const generatedBody = JSON.parse(generatedText) as {
      readonly data: {
        readonly created: number
        readonly skipped: number
        readonly effective_days: number
        readonly multiplier_percent: number
        readonly items: ReadonlyArray<{
          readonly result: string
          readonly invoice_number?: string
        }>
      }
    }
    const replay = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/invoices/generate-recurring",
      undefined,
      "recurring-run"
    )
    const replayText = await replay.text()
    const count = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count FROM invoices
        `
        return rows[0]?.count ?? 0
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(generated.status).toBe(200)
    expect(generatedBody.data.created).toBe(1)
    expect(generatedBody.data.skipped).toBe(0)
    expect(generatedBody.data.effective_days).toBeGreaterThan(0)
    expect([75, 85, 100]).toContain(
      generatedBody.data.multiplier_percent
    )
    expect(generatedBody.data.items[0]?.result).toBe("created")
    expect(generatedBody.data.items[0]?.invoice_number)
      .toMatch(/^INV-\d{6}-\d{4}$/)
    expect(replayText).toBe(generatedText)
    expect(count).toBe(1)
  })
})
