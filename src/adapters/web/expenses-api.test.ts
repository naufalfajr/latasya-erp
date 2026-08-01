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
  const directory = mkdtempSync(join(tmpdir(), "latasya-expenses-api-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  await Effect.runPromise(
    seedDefaultAdmin.pipe(Effect.provide(runtimeLayer(databasePath)))
  )
  const accounts = await Effect.runPromise(
    Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      const expenses = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts WHERE account_type = 'expense' LIMIT 1
      `
      const assets = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts WHERE account_type = 'asset' LIMIT 1
      `
      const revenues = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts WHERE account_type = 'revenue' LIMIT 1
      `
      return {
        expense: expenses[0]?.id ?? 0,
        asset: assets[0]?.id ?? 0,
        revenue: revenues[0]?.id ?? 0
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
    accounts,
    databasePath,
    cookie: login.headers.get("set-cookie")?.split(";")[0] ?? "",
    handler: web.handler
  }
}

const expenseBody = (
  expense: number,
  asset: number,
  amount = "50000",
  description = "Toll fee"
) => ({
  entry_date: "2026-05-10",
  description,
  amount,
  expense_account: expense,
  payment_account: asset
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

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("expenses API", () => {
  test("creates, paginates, gets, updates, audits, and deletes balanced expenses", async () => {
    const { accounts, databasePath, cookie, handler } = await setup()
    const first = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/expenses",
      expenseBody(accounts.expense, accounts.asset, "150000", "Fuel")
    )
    const firstBody = await first.json() as {
      readonly data: {
        readonly id: number
        readonly amount: string
        readonly expense_account: { readonly id: number }
        readonly payment_account: { readonly id: number }
      }
    }
    const id = firstBody.data.id
    for (let index = 0; index < 2; index += 1) {
      await request(
        handler,
        cookie,
        "POST",
        "/api/v1/expenses",
        expenseBody(
          accounts.expense,
          accounts.asset,
          String((index + 1) * 10000),
          `Parking ${index}`
        )
      )
    }
    const list = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/expenses?search=Parking&per_page=1&page=2"
    )
    const listBody = await list.json() as {
      readonly data: ReadonlyArray<{
        readonly expense_account?: unknown
      }>
      readonly meta: {
        readonly total: number
        readonly total_pages: number
      }
    }
    const get = await request(
      handler,
      cookie,
      "GET",
      `/api/v1/expenses/${id}`
    )
    const updated = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/expenses/${id}`,
      expenseBody(
        accounts.expense,
        accounts.asset,
        "90000",
        "Updated fuel purchase"
      )
    )
    const removed = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/expenses/${id}`
    )
    const auditActions = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE target_type = 'expense' AND target_id = ${id}
          ORDER BY id
        `
        return rows.map((row) => row.action)
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(first.status).toBe(201)
    expect(firstBody.data.amount).toBe("150000")
    expect(firstBody.data.expense_account.id).toBe(accounts.expense)
    expect(firstBody.data.payment_account.id).toBe(accounts.asset)
    expect(list.status).toBe(200)
    expect(listBody.data).toHaveLength(1)
    expect(listBody.data[0]?.expense_account).toBeUndefined()
    expect(listBody.meta).toMatchObject({ total: 2, total_pages: 2 })
    expect(get.status).toBe(200)
    expect(updated.status).toBe(200)
    expect((await updated.json() as {
      readonly data: { readonly amount: string }
    }).data.amount).toBe("90000")
    expect(removed.status).toBe(204)
    expect(auditActions).toEqual([
      "expense.create",
      "expense.update",
      "expense.delete"
    ])
  })

  test("matches validation, strict JSON, authentication, and capability behavior", async () => {
    const { accounts, databasePath, cookie, handler } = await setup()
    const missing = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/expenses",
      { description: "Missing fields" }
    )
    const unknown = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/expenses",
      {
        ...expenseBody(accounts.expense, accounts.asset),
        extra: true
      }
    )
    const anonymous = await request(
      handler,
      "",
      "GET",
      "/api/v1/expenses"
    )
    const plaintext = "lat_expense-no-scope"
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
            1, 'expense-no-scope', 'lat_expense', ${hash},
            '["reports.view"]'
          )
        `
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const forbidden = await request(
      handler,
      `Bearer ${plaintext}`,
      "POST",
      "/api/v1/expenses",
      expenseBody(accounts.expense, accounts.asset)
    )

    expect(missing.status).toBe(422)
    expect((await missing.json() as {
      readonly fields: Readonly<Record<string, string>>
    }).fields).toMatchObject({
      entry_date: "required",
      amount: "required",
      expense_account: "required",
      payment_account: "required"
    })
    expect(unknown.status).toBe(400)
    expect(anonymous.status).toBe(401)
    expect(forbidden.status).toBe(403)
    expect(await forbidden.text()).toContain(
      "expenses.manage capability required"
    )
  })

  test("replays idempotent creates and rejects changed bodies", async () => {
    const { accounts, databasePath, cookie, handler } = await setup()
    const key = "expense-idempotency-key"
    const body = expenseBody(
      accounts.expense,
      accounts.asset,
      "80000",
      "Idempotency test expense"
    )
    const first = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/expenses",
      body,
      key
    )
    const firstText = await first.text()
    const replay = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/expenses",
      body,
      key
    )
    const replayText = await replay.text()
    const conflict = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/expenses",
      { ...body, amount: "80001" },
      key
    )
    const count = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM journal_entries
          WHERE description = 'Idempotency test expense'
        `
        return rows[0]?.count ?? 0
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(first.status).toBe(201)
    expect(replay.status).toBe(201)
    expect(replayText).toBe(firstText)
    expect(conflict.status).toBe(409)
    expect(count).toBe(1)
  })

  test("returns 404 for missing and wrong-source entries", async () => {
    const { accounts, databasePath, cookie, handler } = await setup()
    const incomeId = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        yield* sql`
          INSERT INTO journal_entries (
            entry_date, reference, description, source_type,
            is_posted, created_by
          )
          VALUES (
            '2026-05-10', 'INC-1', 'Income entry', 'income', 1, 1
          )
        `
        const ids = yield* sql<{ readonly id: number }>`
          SELECT last_insert_rowid() AS id
        `
        const id = ids[0]?.id ?? 0
        yield* sql`
          INSERT INTO journal_lines (
            entry_id, account_id, debit, credit, memo
          )
          VALUES
            (${id}, ${accounts.asset}, 10000, 0, ''),
            (${id}, ${accounts.revenue}, 0, 10000, '')
        `
        return id
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const getWrong = await request(
      handler,
      cookie,
      "GET",
      `/api/v1/expenses/${incomeId}`
    )
    const updateWrong = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/expenses/${incomeId}`,
      expenseBody(accounts.expense, accounts.asset)
    )
    const removeMissing = await request(
      handler,
      cookie,
      "DELETE",
      "/api/v1/expenses/999999"
    )

    expect(getWrong.status).toBe(404)
    expect(updateWrong.status).toBe(404)
    expect(removeMissing.status).toBe(404)
  })
})
