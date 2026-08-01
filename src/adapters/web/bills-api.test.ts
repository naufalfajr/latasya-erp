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
  const directory = mkdtempSync(join(tmpdir(), "latasya-bills-api-"))
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
        VALUES ('API Bill Supplier', 'supplier', 1)
      `
      const contacts = yield* sql<{ readonly id: number }>`
        SELECT last_insert_rowid() AS id
      `
      const expense = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts WHERE code = '5-1001'
      `
      const bank = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts WHERE code = '1-1002'
      `
      return {
        contactId: contacts[0]?.id ?? 0,
        expenseId: expense[0]?.id ?? 0,
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

const billBody = (
  contactId: number,
  expenseId: number,
  unitPrice = "500000"
) => ({
  contact_id: contactId,
  bill_date: "2026-05-10",
  due_date: "2026-06-10",
  tax_amount: "0",
  notes: "test bill",
  lines: [{
    description: "Diesel",
    quantity: "1.00",
    unit_price: unitPrice,
    account_id: expenseId
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

const createBill = async (
  handler: (request: Request) => Promise<Response>,
  cookie: string,
  contactId: number,
  expenseId: number,
  key?: string
) => {
  const response = await request(
    handler,
    cookie,
    "POST",
    "/api/v1/bills",
    billBody(contactId, expenseId),
    key
  )
  const text = await response.text()
  return {
    response,
    text,
    body: JSON.parse(text) as {
      readonly id: number
      readonly bill_number: string
      readonly status: string
      readonly total: number
    }
  }
}

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("bills API", () => {
  test("creates, filters, gets, updates, audits, and deletes draft bills", async () => {
    const { databasePath, fixtures, cookie, handler } = await setup()
    const created = await createBill(
      handler,
      cookie,
      fixtures.contactId,
      fixtures.expenseId
    )
    const id = created.body.id
    const list = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/bills?status=draft&search=API&per_page=1"
    )
    const listBody = await list.json() as {
      readonly data: ReadonlyArray<{ readonly lines?: unknown }>
      readonly meta: { readonly total: number }
    }
    const get = await request(
      handler,
      cookie,
      "GET",
      `/api/v1/bills/${id}`
    )
    const getBody = await get.json() as {
      readonly id: number
      readonly lines: ReadonlyArray<{
        readonly quantity: number
        readonly amount: number
      }>
    }
    const update = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/bills/${id}`,
      {
        ...billBody(
          fixtures.contactId,
          fixtures.expenseId,
          "600000"
        ),
        tax_amount: "50000"
      }
    )
    const updateBody = await update.json() as {
      readonly total: number
    }
    const removed = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/bills/${id}`
    )
    const audits = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE target_type = 'bill' AND target_id = ${id}
          ORDER BY id
        `
        return rows.map((row) => row.action)
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(created.response.status).toBe(201)
    expect(created.body.bill_number).toMatch(/^BILL-\d{6}-\d{4}$/)
    expect(created.body).toMatchObject({ status: "draft", total: 500000 })
    expect(list.status).toBe(200)
    expect(listBody.meta.total).toBe(1)
    expect(listBody.data[0]?.lines).toBeUndefined()
    expect(get.status).toBe(200)
    expect(getBody.id).toBe(id)
    expect(getBody.lines[0]).toMatchObject({
      quantity: 100,
      amount: 500000
    })
    expect(update.status).toBe(200)
    expect(updateBody.total).toBe(650000)
    expect(removed.status).toBe(204)
    expect(audits).toEqual([
      "bill.create",
      "bill.update",
      "bill.delete"
    ])
  })

  test("idempotently creates, receives, and pays bills", async () => {
    const { databasePath, fixtures, cookie, handler } = await setup()
    const first = await createBill(
      handler,
      cookie,
      fixtures.contactId,
      fixtures.expenseId,
      "bill-create"
    )
    const replay = await createBill(
      handler,
      cookie,
      fixtures.contactId,
      fixtures.expenseId,
      "bill-create"
    )
    const id = first.body.id
    const received = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/bills/${id}/receive`,
      undefined,
      "bill-receive"
    )
    const receivedText = await received.text()
    const receiveReplay = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/bills/${id}/receive`,
      undefined,
      "bill-receive"
    )
    const receiveReplayText = await receiveReplay.text()
    const payment = {
      amount: "200000",
      payment_date: "2026-05-15",
      payment_account: fixtures.bankId
    }
    const partial = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/bills/${id}/payment`,
      payment,
      "bill-payment"
    )
    const partialText = await partial.text()
    const paymentReplay = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/bills/${id}/payment`,
      payment,
      "bill-payment"
    )
    const paymentReplayText = await paymentReplay.text()
    const paid = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/bills/${id}/payment`,
      {
        amount: "300000",
        payment_date: "2026-05-20",
        payment_account: fixtures.bankId
      }
    )
    const paidBody = await paid.json() as {
      readonly status: string
      readonly amount_paid: number
    }
    const counts = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const bills = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count FROM bills
        `
        const journals = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM journal_entries
          WHERE source_type = 'bill'
        `
        const payments = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM payments
          WHERE payment_type = 'bill' AND reference_id = ${id}
        `
        return {
          bills: bills[0]?.count ?? 0,
          journals: journals[0]?.count ?? 0,
          payments: payments[0]?.count ?? 0
        }
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(replay.text).toBe(first.text)
    expect(replay.body.id).toBe(id)
    expect(received.status).toBe(200)
    expect(receiveReplayText).toBe(receivedText)
    expect(partial.status).toBe(200)
    expect(paymentReplayText).toBe(partialText)
    expect(paidBody).toMatchObject({
      status: "paid",
      amount_paid: 500000
    })
    expect(counts).toEqual({ bills: 1, journals: 3, payments: 2 })
  })

  test("enforces strict validation, capability, lifecycle, and overpayment", async () => {
    const { databasePath, fixtures, cookie, handler } = await setup()
    const invalid = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/bills",
      { contact_id: 0, lines: [] }
    )
    const strict = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/bills",
      {
        ...billBody(fixtures.contactId, fixtures.expenseId),
        unknown: true
      }
    )
    const constraint = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/bills",
      billBody(999999, fixtures.expenseId)
    )
    const created = await createBill(
      handler,
      cookie,
      fixtures.contactId,
      fixtures.expenseId
    )
    const draftPayment = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/bills/${created.body.id}/payment`,
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
      `/api/v1/bills/${created.body.id}/receive`
    )
    const editReceived = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/bills/${created.body.id}`,
      billBody(fixtures.contactId, fixtures.expenseId)
    )
    const overpay = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/bills/${created.body.id}/payment`,
      {
        amount: "500001",
        payment_date: "2026-05-15",
        payment_account: fixtures.bankId
      }
    )
    const plaintext = "lat_bill-no-scope"
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
            1, 'bill-no-scope', 'lat_bill', ${hash},
            '["reports.view"]'
          )
        `
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const forbidden = await request(
      handler,
      `Bearer ${plaintext}`,
      "POST",
      "/api/v1/bills",
      billBody(fixtures.contactId, fixtures.expenseId)
    )

    expect(invalid.status).toBe(422)
    expect(strict.status).toBe(400)
    expect(constraint.status).toBe(422)
    expect(draftPayment.status).toBe(409)
    expect(editReceived.status).toBe(409)
    expect(overpay.status).toBe(409)
    expect(await overpay.text()).toContain("exceeds remaining balance")
    expect(forbidden.status).toBe(403)
  })
})
