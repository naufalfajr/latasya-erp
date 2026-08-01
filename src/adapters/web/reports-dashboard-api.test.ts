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
  const directory = mkdtempSync(join(tmpdir(), "latasya-reports-api-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  await Effect.runPromise(
    seedDefaultAdmin.pipe(Effect.provide(runtimeLayer(databasePath)))
  )
  const fixtures = await Effect.runPromise(
    Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      const accounts = yield* sql<{
        readonly id: number
        readonly code: string
      }>`
        SELECT id, code
        FROM accounts
        WHERE code IN ('1-1001', '4-1001')
      `
      const byCode = Object.fromEntries(
        accounts.map((account) => [account.code, account.id])
      )
      yield* sql`
        INSERT INTO journal_entries (
          entry_date,
          reference,
          description,
          source_type,
          is_posted,
          created_by
        )
        VALUES (
          '2026-07-10',
          'REPORT-1',
          'Report fixture',
          'manual',
          1,
          1
        )
      `
      const entries = yield* sql<{ readonly id: number }>`
        SELECT last_insert_rowid() AS id
      `
      yield* sql`
        INSERT INTO journal_lines (
          entry_id, account_id, debit, credit, memo
        )
        VALUES (
          ${entries[0]?.id ?? 0},
          ${byCode["1-1001"] ?? 0},
          750000,
          0,
          ''
        )
      `
      yield* sql`
        INSERT INTO journal_lines (
          entry_id, account_id, debit, credit, memo
        )
        VALUES (
          ${entries[0]?.id ?? 0},
          ${byCode["4-1001"] ?? 0},
          0,
          750000,
          ''
        )
      `
      return {
        cashId: byCode["1-1001"] ?? 0
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

const get = (
  handler: (request: Request) => Promise<Response>,
  cookie: string,
  path: string
) => handler(new Request(`http://localhost${path}`, {
  headers: { cookie }
}))

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("reports API", () => {
  test("returns all reports with integer-IDR strings", async () => {
    const { fixtures, cookie, handler } = await setup()
    const range = "from=2026-07-01&to=2026-07-31"
    const trial = await get(
      handler,
      cookie,
      `/api/v1/reports/trial-balance?${range}`
    )
    const trialBody = await trial.json() as {
      readonly data: {
        readonly rows: ReadonlyArray<{
          readonly total_debit: string
          readonly balance: string
        }>
        readonly total_debit: string
        readonly total_credit: string
      }
    }
    const profit = await get(
      handler,
      cookie,
      `/api/v1/reports/profit-loss?${range}`
    )
    const profitBody = await profit.json() as {
      readonly data: {
        readonly total_revenue: string
        readonly net_income: string
      }
    }
    const balance = await get(
      handler,
      cookie,
      "/api/v1/reports/balance-sheet?date=2026-07-31"
    )
    const balanceBody = await balance.json() as {
      readonly data: {
        readonly assets: { readonly total: string }
        readonly retained_earnings: string
      }
    }
    const cash = await get(
      handler,
      cookie,
      `/api/v1/reports/cash-flow?${range}`
    )
    const cashBody = await cash.json() as {
      readonly data: {
        readonly closing_cash: string
        readonly cash_configured: boolean
      }
    }
    const ledger = await get(
      handler,
      cookie,
      "/api/v1/reports/general-ledger" +
        `?account=${fixtures.cashId}&${range}`
    )
    const ledgerBody = await ledger.json() as {
      readonly data: {
        readonly account_id: number
        readonly entries: ReadonlyArray<{
          readonly debit: string
          readonly balance: string
        }>
      }
    }

    expect(trial.status).toBe(200)
    expect(trialBody.data).toMatchObject({
      total_debit: "750000",
      total_credit: "750000"
    })
    expect(trialBody.data.rows[0]?.total_debit).toBeString()
    expect(trialBody.data.rows[0]?.balance).toBeString()
    expect(profit.status).toBe(200)
    expect(profitBody.data).toMatchObject({
      total_revenue: "750000",
      net_income: "750000"
    })
    expect(balance.status).toBe(200)
    expect(balanceBody.data.assets.total).toBe("750000")
    expect(balanceBody.data.retained_earnings).toBe("750000")
    expect(cash.status).toBe(200)
    expect(cashBody.data).toMatchObject({
      closing_cash: "750000",
      cash_configured: true
    })
    expect(ledger.status).toBe(200)
    expect(ledgerBody.data.account_id).toBe(fixtures.cashId)
    expect(ledgerBody.data.entries[0]).toMatchObject({
      debit: "750000",
      balance: "750000"
    })
  })

  test("matches general-ledger errors and unavailable cash fields", async () => {
    const { databasePath, cookie, handler } = await setup()
    const missing = await get(
      handler,
      cookie,
      "/api/v1/reports/general-ledger"
    )
    const unknown = await get(
      handler,
      cookie,
      "/api/v1/reports/general-ledger?account=999999"
    )
    await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        yield* sql`UPDATE accounts SET is_cash = 0`
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const cash = await get(
      handler,
      cookie,
      "/api/v1/reports/cash-flow" +
        "?from=2026-07-01&to=2026-07-31"
    )
    const cashBody = await cash.json() as {
      readonly data: {
        readonly cash_configured: boolean
        readonly total_movement: string | null
        readonly closing_cash: string | null
      }
    }

    expect(missing.status).toBe(400)
    expect(unknown.status).toBe(404)
    expect(cash.status).toBe(200)
    expect(cashBody.data).toMatchObject({
      cash_configured: false,
      total_movement: null,
      closing_cash: null
    })
  })
})

describe("dashboard API", () => {
  test("returns six monthly or quarterly trends to any authenticated user", async () => {
    const { cookie, handler } = await setup()
    for (const granularity of [undefined, "monthly", "quarterly"]) {
      const response = await get(
        handler,
        cookie,
        "/api/v1/dashboard" +
          (granularity === undefined
            ? ""
            : `?granularity=${granularity}`)
      )
      const body = await response.json() as {
        readonly data: {
          readonly cash_balance: string | null
          readonly monthly_revenue: string
          readonly outstanding_invoices: string
          readonly granularity: string
          readonly trends: ReadonlyArray<{
            readonly revenue: string
            readonly net_cash_movement: string | null
          }>
        }
      }
      expect(response.status).toBe(200)
      expect(body.data.granularity).toBe(granularity ?? "monthly")
      expect(body.data.trends).toHaveLength(6)
      expect(body.data.monthly_revenue).toBeString()
      expect(body.data.outstanding_invoices).toBeString()
      expect(body.data.cash_balance).toBeString()
      expect(body.data.trends[0]?.revenue).toBeString()
    }
  })

  test("rejects present empty and unsupported granularity", async () => {
    const { cookie, handler } = await setup()
    for (const query of ["?granularity=", "?granularity=weekly"]) {
      const response = await get(
        handler,
        cookie,
        `/api/v1/dashboard${query}`
      )
      expect(response.status).toBe(400)
      expect(await response.json()).toMatchObject({
        code: "invalid_request",
        error: "unsupported dashboard granularity",
        fields: {
          granularity: "must be one of: monthly, quarterly"
        }
      })
    }
  })
})
