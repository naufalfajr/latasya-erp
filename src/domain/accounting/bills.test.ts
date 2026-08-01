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
import { Bills, BillsLive, validateBill } from "./bills.ts"
import { JournalsLive } from "./journals.ts"

const temporaryDirectories: Array<string> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-bills-"))
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
    BillsLive.pipe(Layer.provide(Layer.merge(base, journals)))
  )
  const fixtures = await Effect.runPromise(
    Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      yield* sql`
        INSERT INTO contacts (
          name, contact_type, is_active
        )
        VALUES ('Bill Supplier', 'supplier', 1)
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
    }).pipe(Effect.provide(layer))
  )
  return { fixtures, layer }
}

const values = (contactId: number, expenseId: number) => ({
  contactId,
  billDate: "2026-05-10",
  dueDate: "2026-06-10",
  taxAmount: 0,
  notes: "fuel bill",
  lines: [{
    description: "Diesel",
    quantity: 100,
    unitPrice: 500000,
    accountId: expenseId
  }]
})

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("Bills", () => {
  test("creates, filters, updates, and deletes draft bills", async () => {
    const { fixtures, layer } = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const bills = yield* Bills
        const created = yield* bills.create(
          values(fixtures.contactId, fixtures.expenseId),
          1
        )
        const listed = yield* bills.list({
          status: "draft",
          search: "Bill Supplier",
          limit: 50,
          offset: 0
        })
        const updated = yield* bills.update(created.id, {
          ...values(fixtures.contactId, fixtures.expenseId),
          taxAmount: 50000
        })
        const removed = yield* bills.remove(created.id)
        return { created, listed, updated, removed }
      }).pipe(Effect.provide(layer))
    )

    expect(result.created.bill_number).toMatch(/^BILL-\d{6}-\d{4}$/)
    expect(result.created.total).toBe(500000)
    expect(result.created.lines?.[0]).toMatchObject({
      quantity: 100,
      unit_price: 500000,
      amount: 500000
    })
    expect(result.listed.total).toBe(1)
    expect(result.listed.bills[0]?.lines).toBeUndefined()
    expect(result.updated.after.total).toBe(550000)
    expect(result.removed.id).toBe(result.created.id)
  })

  test("posts payable and payment journals through the bill lifecycle", async () => {
    const { fixtures, layer } = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const bills = yield* Bills
        const created = yield* bills.create(
          values(fixtures.contactId, fixtures.expenseId),
          1
        )
        const received = yield* bills.receive(created.id, 1)
        const partial = yield* bills.recordPayment(
          created.id,
          200000,
          "2026-05-15",
          fixtures.bankId,
          1
        )
        const paid = yield* bills.recordPayment(
          created.id,
          300000,
          "2026-05-20",
          fixtures.bankId,
          1
        )
        const sql = yield* SqlClient.SqlClient
        const journals = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM journal_entries
          WHERE source_type = 'bill'
        `
        return {
          received,
          partial,
          paid,
          journals: journals[0]?.count ?? 0
        }
      }).pipe(Effect.provide(layer))
    )

    expect(result.received.status).toBe("received")
    expect(result.received.journal_id).toBeDefined()
    expect(result.partial).toMatchObject({
      status: "partial",
      amount_paid: 200000
    })
    expect(result.paid).toMatchObject({
      status: "paid",
      amount_paid: 500000
    })
    expect(result.journals).toBe(3)
  })

  test("rejects payments for drafts, paid bills, and overpayments", async () => {
    const { fixtures, layer } = await setup()
    const errors = await Effect.runPromise(
      Effect.gen(function*() {
        const bills = yield* Bills
        const created = yield* bills.create(
          values(fixtures.contactId, fixtures.expenseId),
          1
        )
        const draft = yield* Effect.either(
          bills.recordPayment(
            created.id,
            1,
            "2026-05-15",
            fixtures.bankId,
            1
          )
        )
        yield* bills.receive(created.id, 1)
        const over = yield* Effect.either(
          bills.recordPayment(
            created.id,
            500001,
            "2026-05-15",
            fixtures.bankId,
            1
          )
        )
        return { draft, over }
      }).pipe(Effect.provide(layer))
    )

    expect(errors.draft._tag).toBe("Left")
    expect(errors.over._tag).toBe("Left")
  })
})

describe("validateBill", () => {
  test("defaults zero quantity and rejects more than two decimals", () => {
    const base = {
      contactId: 1,
      billDate: "2026-05-10",
      dueDate: "2026-06-10",
      taxAmount: "",
      notes: ""
    }
    const valid = validateBill({
      ...base,
      lines: [{
        description: "Fuel",
        quantity: "",
        unitPrice: "1000",
        accountId: 1
      }]
    })
    const invalid = validateBill({
      ...base,
      lines: [{
        description: "Fuel",
        quantity: "1.999",
        unitPrice: "1000",
        accountId: 1
      }]
    })

    expect(valid.lines?.[0]?.quantity).toBe(100)
    expect(valid.taxAmount).toBe(0)
    expect(invalid.fields["lines[0].quantity"]).toBe("invalid quantity")
  })
})
