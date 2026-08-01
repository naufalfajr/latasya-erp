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
import {
  CreditNotes,
  CreditNotesLive,
  validateCreditNote
} from "./credit-notes.ts"
import { JournalsLive } from "./journals.ts"

const temporaryDirectories: Array<string> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-credit-notes-"))
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
    CreditNotesLive.pipe(Layer.provide(Layer.merge(base, journals)))
  )
  const fixtures = await Effect.runPromise(
    Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      yield* sql`
        INSERT INTO contacts (name, contact_type, is_active)
        VALUES ('Credit Customer', 'customer', 1)
      `
      const contacts = yield* sql<{ readonly id: number }>`
        SELECT last_insert_rowid() AS id
      `
      const contactId = contacts[0]?.id ?? 0
      yield* sql`
        INSERT INTO invoices (
          invoice_number,
          contact_id,
          invoice_date,
          due_date,
          status,
          subtotal,
          tax_amount,
          total,
          amount_paid,
          amount_credited,
          notes,
          created_by
        )
        VALUES (
          'INV-CREDIT-1',
          ${contactId},
          '2026-04-04',
          '2026-04-30',
          'sent',
          1000000,
          100000,
          1100000,
          0,
          0,
          '',
          1
        )
      `
      const invoices = yield* sql<{ readonly id: number }>`
        SELECT last_insert_rowid() AS id
      `
      const revenue = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts WHERE code = '4-1001'
      `
      return {
        contactId,
        invoiceId: invoices[0]?.id ?? 0,
        revenueId: revenue[0]?.id ?? 0
      }
    }).pipe(Effect.provide(layer))
  )
  return { fixtures, layer }
}

const values = (
  contactId: number,
  revenueId: number,
  invoiceId?: number
) => ({
  contactId,
  ...(invoiceId === undefined ? {} : { invoiceId }),
  cnDate: "2026-04-12",
  reason: "cancellation",
  taxAmount: invoiceId === undefined ? 0 : 100000,
  notes: "credit",
  lines: [{
    description: "Refund",
    quantity: 100,
    unitPrice: 1000000,
    accountId: revenueId
  }]
})

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("CreditNotes", () => {
  test("creates, filters, updates, and deletes drafts", async () => {
    const { fixtures, layer } = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const creditNotes = yield* CreditNotes
        const created = yield* creditNotes.create(
          values(fixtures.contactId, fixtures.revenueId),
          1
        )
        const listed = yield* creditNotes.list({
          status: "draft",
          search: "Credit Customer",
          limit: 50,
          offset: 0
        })
        const updated = yield* creditNotes.update(created.id, {
          ...values(fixtures.contactId, fixtures.revenueId),
          reason: "discount",
          lines: [{
            description: "Adjusted refund",
            quantity: 100,
            unitPrice: 600000,
            accountId: fixtures.revenueId
          }]
        })
        const removed = yield* creditNotes.remove(created.id)
        return { created, listed, updated, removed }
      }).pipe(Effect.provide(layer))
    )

    expect(result.created.cn_number).toMatch(/^CN-\d{6}-\d{4}$/)
    expect(result.created).toMatchObject({
      status: "draft",
      total: 1000000
    })
    expect(result.listed.total).toBe(1)
    expect(result.listed.creditNotes[0]?.lines).toBeUndefined()
    expect(result.updated.after).toMatchObject({
      reason: "discount",
      total: 600000
    })
    expect(result.removed.id).toBe(result.created.id)
  })

  test("issues and voids a full linked credit note", async () => {
    const { fixtures, layer } = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const creditNotes = yield* CreditNotes
        const created = yield* creditNotes.create(
          values(
            fixtures.contactId,
            fixtures.revenueId,
            fixtures.invoiceId
          ),
          1
        )
        const issued = yield* creditNotes.issue(created.id, 1)
        const sql = yield* SqlClient.SqlClient
        const afterIssue = yield* sql<{
          readonly status: string
          readonly amount_credited: number
        }>`
          SELECT status, amount_credited
          FROM invoices
          WHERE id = ${fixtures.invoiceId}
        `
        const voided = yield* creditNotes.void(created.id, 1)
        const afterVoid = yield* sql<{
          readonly status: string
          readonly amount_credited: number
        }>`
          SELECT status, amount_credited
          FROM invoices
          WHERE id = ${fixtures.invoiceId}
        `
        const journalCount = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM journal_entries
          WHERE source_type = 'credit_note'
        `
        return {
          issued,
          afterIssue: afterIssue[0],
          voided,
          afterVoid: afterVoid[0],
          journalCount: journalCount[0]?.count ?? 0
        }
      }).pipe(Effect.provide(layer))
    )

    expect(result.issued.status).toBe("issued")
    expect(result.issued.journal_id).toBeDefined()
    expect(result.afterIssue).toEqual({
      status: "cancelled",
      amount_credited: 1100000
    })
    expect(result.voided.status).toBe("void")
    expect(result.voided.journal_id).toBe(result.issued.journal_id)
    expect(result.afterVoid).toEqual({
      status: "sent",
      amount_credited: 0
    })
    expect(result.journalCount).toBe(2)
  })

  test("rejects excess tax and excess invoice credit", async () => {
    const { fixtures, layer } = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const creditNotes = yield* CreditNotes
        const excessiveTax = yield* creditNotes.create({
          ...values(
            fixtures.contactId,
            fixtures.revenueId,
            fixtures.invoiceId
          ),
          taxAmount: 100001
        }, 1)
        const taxError = yield* Effect.either(
          creditNotes.issue(excessiveTax.id, 1)
        )
        const excessiveCredit = yield* creditNotes.create({
          ...values(
            fixtures.contactId,
            fixtures.revenueId,
            fixtures.invoiceId
          ),
          taxAmount: 0,
          lines: [{
            description: "Too much",
            quantity: 100,
            unitPrice: 1100001,
            accountId: fixtures.revenueId
          }]
        }, 1)
        const creditError = yield* Effect.either(
          creditNotes.issue(excessiveCredit.id, 1)
        )
        return { taxError, creditError }
      }).pipe(Effect.provide(layer))
    )

    expect(result.taxError._tag).toBe("Left")
    expect(result.creditError._tag).toBe("Left")
  })
})

describe("validateCreditNote", () => {
  test("matches reason, amount, and quantity validation", () => {
    const valid = validateCreditNote({
      contactId: 1,
      cnDate: "2026-04-12",
      reason: "return",
      taxAmount: "",
      notes: "",
      lines: [{
        description: "Refund",
        quantity: "",
        unitPrice: "1000",
        accountId: 2
      }]
    })
    const invalid = validateCreditNote({
      contactId: 0,
      cnDate: "",
      reason: "refund",
      taxAmount: "-1",
      notes: "",
      lines: [{
        description: "Refund",
        quantity: "1.000",
        unitPrice: "1000",
        accountId: 2
      }]
    })

    expect(valid.lines?.[0]?.quantity).toBe(100)
    expect(valid.taxAmount).toBe(0)
    expect(invalid.fields).toEqual({
      contact_id: "required",
      cn_date: "required",
      reason: "must be one of: cancellation, return, discount, other",
      tax_amount: "invalid amount",
      "lines[0].quantity": "invalid quantity"
    })
  })
})
