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
  const directory = mkdtempSync(join(tmpdir(), "latasya-income-api-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  await Effect.runPromise(
    seedDefaultAdmin.pipe(Effect.provide(runtimeLayer(databasePath)))
  )
  const accounts = await Effect.runPromise(
    Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      const revenues = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts WHERE account_type = 'revenue' LIMIT 1
      `
      const assets = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts WHERE account_type = 'asset' LIMIT 1
      `
      const expenses = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts WHERE account_type = 'expense' LIMIT 1
      `
      return {
        revenue: revenues[0]?.id ?? 0,
        asset: assets[0]?.id ?? 0,
        expense: expenses[0]?.id ?? 0
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

const incomeBody = (
  revenue: number,
  asset: number,
  amount = "750000",
  description = "Charter bus payment"
) => ({
  entry_date: "2026-05-10",
  description,
  amount,
  revenue_account: revenue,
  deposit_account: asset
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

describe("income API", () => {
  test("creates balanced entries, paginates, gets, updates, audits, and deletes", async () => {
    const { accounts, databasePath, cookie, handler } = await setup()
    const first = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/income",
      incomeBody(accounts.revenue, accounts.asset)
    )
    const firstBody = await first.json() as {
      readonly data: {
        readonly id: number
        readonly reference: string
        readonly amount: string
        readonly revenue_account: { readonly id: number }
        readonly deposit_account: { readonly id: number }
      }
    }
    const id = firstBody.data.id
    for (let index = 0; index < 2; index += 1) {
      await request(
        handler,
        cookie,
        "POST",
        "/api/v1/income",
        incomeBody(
          accounts.revenue,
          accounts.asset,
          String((index + 2) * 1000),
          `Payment ${index}`
        )
      )
    }
    const list = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/income?from=2026-05-01&to=2026-05-31&search=Payment&per_page=1&page=2"
    )
    const listBody = await list.json() as {
      readonly data: ReadonlyArray<{
        readonly revenue_account?: unknown
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
      `/api/v1/income/${id}`
    )
    const updated = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/income/${id}`,
      {
        ...incomeBody(
          accounts.revenue,
          accounts.asset,
          "900000",
          "Updated charter payment"
        ),
        entry_date: "2026-05-15"
      }
    )
    const removed = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/income/${id}`
    )
    const database = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const balances = yield* sql<{
          readonly debit: number
          readonly credit: number
        }>`
          SELECT
            COALESCE(SUM(debit), 0) AS debit,
            COALESCE(SUM(credit), 0) AS credit
          FROM journal_lines
          WHERE entry_id = ${id}
        `
        const audits = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE target_type = 'income' AND target_id = ${id}
          ORDER BY id
        `
        return {
          balance: balances[0],
          actions: audits.map((row) => row.action)
        }
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(first.status).toBe(201)
    expect(firstBody.data.reference).toMatch(/^JE-\d{6}-\d{4}$/)
    expect(firstBody.data.amount).toBe("750000")
    expect(firstBody.data.revenue_account.id).toBe(accounts.revenue)
    expect(firstBody.data.deposit_account.id).toBe(accounts.asset)
    expect(list.status).toBe(200)
    expect(listBody.data).toHaveLength(1)
    expect(listBody.data[0]?.revenue_account).toBeUndefined()
    expect(listBody.meta).toMatchObject({ total: 3, total_pages: 3 })
    expect(get.status).toBe(200)
    expect(updated.status).toBe(200)
    expect((await updated.json() as {
      readonly data: { readonly amount: string }
    }).data.amount).toBe("900000")
    expect(removed.status).toBe(204)
    expect(database.balance).toEqual({ debit: 0, credit: 0 })
    expect(database.actions).toEqual([
      "income.create",
      "income.update",
      "income.delete"
    ])
  })

  test("matches validation, strict JSON, authentication, and capability behavior", async () => {
    const { accounts, databasePath, cookie, handler } = await setup()
    const missing = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/income",
      { description: "Missing fields" }
    )
    const invalidType = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/income",
      {
        ...incomeBody(accounts.revenue, accounts.asset),
        amount: 1000
      }
    )
    const unknown = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/income",
      {
        ...incomeBody(accounts.revenue, accounts.asset),
        extra: true
      }
    )
    const anonymous = await request(
      handler,
      "",
      "GET",
      "/api/v1/income"
    )
    const plaintext = "lat_income-no-scope"
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
            1, 'income-no-scope', 'lat_income', ${hash},
            '["reports.view"]'
          )
        `
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const forbidden = await request(
      handler,
      `Bearer ${plaintext}`,
      "POST",
      "/api/v1/income",
      incomeBody(accounts.revenue, accounts.asset)
    )
    const missingBody = await missing.json() as {
      readonly fields: Readonly<Record<string, string>>
    }

    expect(missing.status).toBe(422)
    expect(missingBody.fields).toMatchObject({
      entry_date: "required",
      amount: "required",
      revenue_account: "required",
      deposit_account: "required"
    })
    expect(invalidType.status).toBe(400)
    expect(unknown.status).toBe(400)
    expect(anonymous.status).toBe(401)
    expect(forbidden.status).toBe(403)
    expect(await forbidden.text()).toContain(
      "income.manage capability required"
    )
  })

  test("replays idempotent creates and rejects changed bodies", async () => {
    const { accounts, databasePath, cookie, handler } = await setup()
    const key = "income-idempotency-key"
    const body = incomeBody(
      accounts.revenue,
      accounts.asset,
      "300000",
      "Idempotency test income"
    )
    const first = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/income",
      body,
      key
    )
    const firstText = await first.text()
    const replay = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/income",
      body,
      key
    )
    const replayText = await replay.text()
    const conflict = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/income",
      { ...body, amount: "300001" },
      key
    )
    const count = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM journal_entries
          WHERE description = 'Idempotency test income'
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
    const expenseId = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        yield* sql`
          INSERT INTO journal_entries (
            entry_date, reference, description, source_type,
            is_posted, created_by
          )
          VALUES (
            '2026-05-10', 'EXP-1', 'Expense entry', 'expense', 1, 1
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
            (${id}, ${accounts.expense}, 10000, 0, ''),
            (${id}, ${accounts.asset}, 0, 10000, '')
        `
        return id
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const getWrong = await request(
      handler,
      cookie,
      "GET",
      `/api/v1/income/${expenseId}`
    )
    const updateWrong = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/income/${expenseId}`,
      incomeBody(accounts.revenue, accounts.asset)
    )
    const removeMissing = await request(
      handler,
      cookie,
      "DELETE",
      "/api/v1/income/999999"
    )

    expect(getWrong.status).toBe(404)
    expect(updateWrong.status).toBe(404)
    expect(removeMissing.status).toBe(404)
  })
})
