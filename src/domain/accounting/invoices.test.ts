import { afterEach, describe, expect, test } from "bun:test"
import { SqlClient } from "@effect/sql"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect, Layer } from "effect"
import { sqliteDatabaseLayer } from "../../adapters/sqlite/database.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { PasswordHasherLive } from "../auth/password.ts"
import { Invoices, InvoicesLive, validateInvoice } from "./invoices.ts"
import { JournalsLive } from "./journals.ts"

const temporaryDirectories: Array<string> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-invoices-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const base = Layer.merge(
    sqliteDatabaseLayer(databasePath),
    PasswordHasherLive
  )
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(base)))
  const journals = JournalsLive.pipe(Layer.provide(base))
  const layer = Layer.mergeAll(
    base,
    journals,
    InvoicesLive.pipe(Layer.provide(Layer.merge(base, journals)))
  )
  const fixtures = await Effect.runPromise(
    Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      yield* sql`
        INSERT INTO contacts (
          name, contact_type, is_active
        )
        VALUES ('Invoice Customer', 'customer', 1)
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
    }).pipe(Effect.provide(layer))
  )
  return { fixtures, layer }
}

const values = (contactId: number, revenueId: number) => ({
  contactId,
  invoiceDate: "2026-05-10",
  dueDate: "2026-06-10",
  taxAmount: 0,
  notes: "test invoice",
  lines: [{
    description: "School bus service",
    quantity: 100,
    unitPrice: 500000,
    accountId: revenueId
  }]
})

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("Invoices", () => {
  test("creates, filters, updates, and deletes draft invoices", async () => {
    const { fixtures, layer } = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const invoices = yield* Invoices
        const created = yield* invoices.create(
          values(fixtures.contactId, fixtures.revenueId),
          1
        )
        const listed = yield* invoices.list({
          status: "draft",
          search: "Invoice Customer",
          limit: 1,
          offset: 0
        })
        const updated = yield* invoices.update(created.id, {
          ...values(fixtures.contactId, fixtures.revenueId),
          taxAmount: 50000
        })
        const removed = yield* invoices.remove(created.id)
        return { created, listed, updated, removed }
      }).pipe(Effect.provide(layer))
    )

    expect(result.created.invoice_number).toMatch(/^INV-\d{6}-\d{4}$/)
    expect(result.created.total).toBe("500000")
    expect(result.created.lines?.[0]).toMatchObject({
      quantity: "1.00",
      unit_price: "500000",
      amount: "500000"
    })
    expect(result.listed.total).toBe(1)
    expect(result.listed.invoices[0]?.lines).toBeUndefined()
    expect(result.updated.after.total).toBe("550000")
    expect(result.removed.id).toBe(result.created.id)
  })

  test("posts receivable and payment journals through the invoice lifecycle", async () => {
    const { fixtures, layer } = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const invoices = yield* Invoices
        const created = yield* invoices.create(
          values(fixtures.contactId, fixtures.revenueId),
          1
        )
        const sent = yield* invoices.send(created.id, 1)
        const partial = yield* invoices.recordPayment(
          created.id,
          200000,
          "2026-05-15",
          fixtures.bankId,
          1
        )
        const paid = yield* invoices.recordPayment(
          created.id,
          300000,
          "2026-05-20",
          fixtures.bankId,
          1
        )
        const sql = yield* SqlClient.SqlClient
        const journalCounts = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM journal_entries
          WHERE source_type = 'invoice'
        `
        const paymentCounts = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM payments
          WHERE payment_type = 'invoice'
            AND reference_id = ${created.id}
        `
        return {
          sent,
          partial,
          paid,
          journals: journalCounts[0]?.count ?? 0,
          payments: paymentCounts[0]?.count ?? 0
        }
      }).pipe(Effect.provide(layer))
    )

    expect(result.sent.status).toBe("sent")
    expect(result.sent.journal_id).not.toBeNull()
    expect(result.partial).toMatchObject({
      status: "partial",
      amount_paid: "200000",
      amount_due: "300000"
    })
    expect(result.paid).toMatchObject({
      status: "paid",
      amount_paid: "500000",
      amount_due: "0"
    })
    expect(result.journals).toBe(3)
    expect(result.payments).toBe(2)
  })

  test("protects lifecycle states and detects overpayment", async () => {
    const { fixtures, layer } = await setup()
    const errors = await Effect.runPromise(
      Effect.gen(function*() {
        const invoices = yield* Invoices
        const created = yield* invoices.create(
          values(fixtures.contactId, fixtures.revenueId),
          1
        )
        const draftPayment = yield* Effect.either(
          invoices.recordPayment(
            created.id,
            1,
            "2026-05-15",
            fixtures.bankId,
            1
          )
        )
        yield* invoices.send(created.id, 1)
        const edit = yield* Effect.either(
          invoices.update(
            created.id,
            values(fixtures.contactId, fixtures.revenueId)
          )
        )
        const overpayment = yield* Effect.either(
          invoices.recordPayment(
            created.id,
            500001,
            "2026-05-15",
            fixtures.bankId,
            1
          )
        )
        return { draftPayment, edit, overpayment }
      }).pipe(Effect.provide(layer))
    )

    expect(errors.draftPayment._tag).toBe("Left")
    expect(errors.edit._tag).toBe("Left")
    expect(errors.overpayment._tag).toBe("Left")
    if (errors.overpayment._tag === "Left") {
      expect(errors.overpayment.left._tag).toBe("InvoiceOverpayment")
    }
  })

  test("bulk operations delete drafts, skip non-drafts, and send drafts", async () => {
    const { fixtures, layer } = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const invoices = yield* Invoices
        const first = yield* invoices.create(
          values(fixtures.contactId, fixtures.revenueId),
          1
        )
        const second = yield* invoices.create(
          values(fixtures.contactId, fixtures.revenueId),
          1
        )
        yield* invoices.send(second.id, 1)
        const removed = yield* invoices.bulkDelete([
          first.id,
          second.id,
          999999
        ])
        const third = yield* invoices.create(
          values(fixtures.contactId, fixtures.revenueId),
          1
        )
        const sent = yield* invoices.bulkSend(
          [third.id, second.id, 999999],
          1
        )
        return { removed, sent }
      }).pipe(Effect.provide(layer))
    )

    expect(result.removed.deleted).toHaveLength(1)
    expect(result.removed.skipped).toHaveLength(2)
    expect(result.sent.sent).toHaveLength(1)
    expect(result.sent.skipped).toHaveLength(2)
    expect(result.sent.failed).toEqual([])
  })
})

describe("validateInvoice", () => {
  test("parses two-decimal quantities exactly like the Go endpoint", () => {
    const valid = validateInvoice({
      contactId: 1,
      invoiceDate: "2026-05-10",
      dueDate: "2026-06-10",
      taxAmount: "",
      notes: "",
      lines: [{
        description: "Service",
        quantity: "1.999",
        unitPrice: "500000",
        accountId: 1
      }]
    })
    const invalid = validateInvoice({
      contactId: 1,
      invoiceDate: "2026-05-10",
      dueDate: "2026-06-10",
      taxAmount: "0",
      notes: "",
      lines: [{
        description: "Service",
        quantity: "1.2.3",
        unitPrice: "500000",
        accountId: 1
      }]
    })

    expect(valid.lines?.[0]?.quantity).toBe(199)
    expect(valid.taxAmount).toBe(0)
    expect(invalid.fields["lines[0].quantity"]).toBe("must be positive")
  })
})
