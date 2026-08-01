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
  const directory = mkdtempSync(join(tmpdir(), "latasya-cn-api-"))
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
        INSERT INTO contacts (name, contact_type, is_active)
        VALUES ('API Credit Customer', 'customer', 1)
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
          'INV-API-CREDIT',
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

const creditNoteBody = (
  contactId: number,
  revenueId: number,
  invoiceId?: number,
  unitPrice = "1000000"
) => ({
  contact_id: contactId,
  ...(invoiceId === undefined ? {} : { invoice_id: invoiceId }),
  cn_date: "2026-04-12",
  reason: "cancellation",
  tax_amount: invoiceId === undefined ? "0" : "100000",
  notes: "API credit note",
  lines: [{
    description: "Refund",
    quantity: "1.00",
    unit_price: unitPrice,
    account_id: revenueId
  }]
})

const request = (
  handler: (request: Request) => Promise<Response>,
  cookie: string,
  method: string,
  path: string,
  body?: unknown,
  idempotencyKey?: string
) => handler(new Request(`http://localhost${path}`, {
  method,
  headers: {
    cookie,
    ...(body === undefined ? {} : { "content-type": "application/json" }),
    ...(idempotencyKey === undefined
      ? {}
      : { "idempotency-key": idempotencyKey })
  },
  ...(body === undefined ? {} : { body: JSON.stringify(body) })
}))

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("credit notes API", () => {
  test("creates, lists, gets, updates, audits, and deletes drafts", async () => {
    const { databasePath, fixtures, cookie, handler } = await setup()
    const create = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/credit-notes",
      creditNoteBody(fixtures.contactId, fixtures.revenueId)
    )
    const created = await create.json() as {
      readonly id: number
      readonly cn_number: string
      readonly total: number
      readonly invoice_id?: number
      readonly lines: ReadonlyArray<unknown>
    }
    const list = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/credit-notes?status=draft&search=API&per_page=1"
    )
    const listed = await list.json() as {
      readonly data: ReadonlyArray<{ readonly lines?: unknown }>
      readonly meta: { readonly total: number }
    }
    const get = await request(
      handler,
      cookie,
      "GET",
      `/api/v1/credit-notes/${created.id}`
    )
    const fetched = await get.json() as {
      readonly id: number
      readonly lines: ReadonlyArray<unknown>
    }
    const update = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/credit-notes/${created.id}`,
      {
        ...creditNoteBody(
          fixtures.contactId,
          fixtures.revenueId,
          undefined,
          "600000"
        ),
        reason: "discount"
      }
    )
    const updated = await update.json() as {
      readonly reason: string
      readonly total: number
    }
    const removed = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/credit-notes/${created.id}`
    )
    const audits = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE
            target_type = 'credit_note'
            AND target_id = ${created.id}
          ORDER BY id
        `
        return rows.map((row) => row.action)
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(create.status).toBe(201)
    expect(created.cn_number).toMatch(/^CN-\d{6}-\d{4}$/)
    expect(created).toMatchObject({ total: 1000000 })
    expect(created.invoice_id).toBeUndefined()
    expect(created.lines).toHaveLength(1)
    expect(list.status).toBe(200)
    expect(listed.meta.total).toBe(1)
    expect(listed.data[0]?.lines).toBeUndefined()
    expect(get.status).toBe(200)
    expect(fetched).toMatchObject({ id: created.id })
    expect(fetched.lines).toHaveLength(1)
    expect(update.status).toBe(200)
    expect(updated).toEqual(expect.objectContaining({
      reason: "discount",
      total: 600000
    }))
    expect(removed.status).toBe(204)
    expect(await removed.text()).toBe("")
    expect(audits).toEqual([
      "credit_note.create",
      "credit_note.update",
      "credit_note.delete"
    ])
  })

  test("idempotently creates, issues, and voids linked credit notes", async () => {
    const { databasePath, fixtures, cookie, handler } = await setup()
    const body = creditNoteBody(
      fixtures.contactId,
      fixtures.revenueId,
      fixtures.invoiceId
    )
    const first = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/credit-notes",
      body,
      "credit-create"
    )
    const firstText = await first.text()
    const replay = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/credit-notes",
      body,
      "credit-create"
    )
    const replayText = await replay.text()
    const created = JSON.parse(firstText) as { readonly id: number }
    const issue = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/credit-notes/${created.id}/issue`,
      undefined,
      "credit-issue"
    )
    const issueText = await issue.text()
    const issueReplay = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/credit-notes/${created.id}/issue`,
      undefined,
      "credit-issue"
    )
    const issueReplayText = await issueReplay.text()
    const voided = await request(
      handler,
      cookie,
      "POST",
      `/api/v1/credit-notes/${created.id}/void`
    )
    const voidedBody = await voided.json() as {
      readonly status: string
      readonly journal_id: number
    }
    const database = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const invoice = yield* sql<{
          readonly status: string
          readonly amount_credited: number
        }>`
          SELECT status, amount_credited
          FROM invoices
          WHERE id = ${fixtures.invoiceId}
        `
        const counts = yield* sql<{
          readonly credit_notes: number
          readonly journals: number
          readonly audits: number
        }>`
          SELECT
            (SELECT COUNT(*) FROM credit_notes) AS credit_notes,
            (
              SELECT COUNT(*) FROM journal_entries
              WHERE source_type = 'credit_note'
            ) AS journals,
            (
              SELECT COUNT(*) FROM audit_log
              WHERE target_type = 'credit_note'
            ) AS audits
        `
        return { invoice: invoice[0], counts: counts[0] }
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(first.status).toBe(201)
    expect(replay.status).toBe(201)
    expect(replayText).toBe(firstText)
    expect(issue.status).toBe(200)
    expect(issueReplay.status).toBe(200)
    expect(issueReplayText).toBe(issueText)
    expect(JSON.parse(issueText)).toMatchObject({ status: "issued" })
    expect(voided.status).toBe(200)
    expect(voidedBody.status).toBe("void")
    expect(database.invoice).toEqual({
      status: "sent",
      amount_credited: 0
    })
    expect(database.counts).toEqual({
      credit_notes: 1,
      journals: 2,
      audits: 3
    })
  })

  test("rejects unknown fields and invalid values", async () => {
    const { fixtures, cookie, handler } = await setup()
    const unknown = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/credit-notes",
      {
        ...creditNoteBody(fixtures.contactId, fixtures.revenueId),
        unexpected: true
      }
    )
    const invalid = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/credit-notes",
      {
        ...creditNoteBody(fixtures.contactId, fixtures.revenueId),
        reason: "refund",
        lines: [{
          description: "Refund",
          quantity: "1.000",
          unit_price: "100",
          account_id: fixtures.revenueId
        }]
      }
    )
    const invalidBody = await invalid.json() as {
      readonly code: string
      readonly fields: Readonly<Record<string, string>>
    }

    expect(unknown.status).toBe(400)
    expect(invalid.status).toBe(422)
    expect(invalidBody.code).toBe("validation_failed")
    expect(invalidBody.fields).toEqual({
      reason: "must be one of: cancellation, return, discount, other",
      "lines[0].quantity": "invalid quantity"
    })
  })
})
