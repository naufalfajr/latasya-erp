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
import { Reporting, ReportingLive } from "./reporting.ts"

const temporaryDirectories: Array<string> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-reporting-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const base = Layer.merge(
    sqliteDatabaseLayer(databasePath),
    PasswordHasherLive
  )
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(base)))
  const layer = Layer.mergeAll(
    base,
    ReportingLive.pipe(Layer.provide(base))
  )
  const accounts = await Effect.runPromise(
    Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      const rows = yield* sql<{
        readonly id: number
        readonly code: string
      }>`
        SELECT id, code
        FROM accounts
        WHERE code IN (
          '1-1001',
          '1-1002',
          '1-1100',
          '1-1200',
          '4-1001',
          '5-1001'
        )
      `
      return Object.fromEntries(rows.map((row) => [row.code, row.id]))
    }).pipe(Effect.provide(layer))
  )
  return { accounts, layer }
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("Reporting", () => {
  test("generates reports and Jakarta-aligned dashboard trends", async () => {
    const { accounts, layer } = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const post = (
          date: string,
          posted: boolean,
          lines: ReadonlyArray<{
            readonly account: number
            readonly debit: number
            readonly credit: number
          }>
        ) => Effect.gen(function*() {
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
              ${date},
              ${`REF-${date}-${lines[0]?.account ?? 0}`},
              'report fixture',
              'manual',
              ${posted ? 1 : 0},
              1
            )
          `
          const ids = yield* sql<{ readonly id: number }>`
            SELECT last_insert_rowid() AS id
          `
          for (const line of lines) {
            yield* sql`
              INSERT INTO journal_lines (
                entry_id,
                account_id,
                debit,
                credit,
                memo
              )
              VALUES (
                ${ids[0]?.id ?? 0},
                ${line.account},
                ${line.debit},
                ${line.credit},
                ''
              )
            `
          }
        })
        const cash = accounts["1-1001"] ?? 0
        const bank = accounts["1-1002"] ?? 0
        const ar = accounts["1-1100"] ?? 0
        const prepaid = accounts["1-1200"] ?? 0
        const revenue = accounts["4-1001"] ?? 0
        const expense = accounts["5-1001"] ?? 0
        yield* post("2026-01-15", true, [
          { account: cash, debit: 1000, credit: 0 },
          { account: revenue, debit: 0, credit: 1000 }
        ])
        yield* post("2026-02-10", true, [
          { account: ar, debit: 500, credit: 0 },
          { account: revenue, debit: 0, credit: 500 }
        ])
        yield* post("2026-02-12", true, [
          { account: prepaid, debit: 200, credit: 0 },
          { account: cash, debit: 0, credit: 200 }
        ])
        yield* post("2026-02-20", true, [
          { account: expense, debit: 300, credit: 0 },
          { account: cash, debit: 0, credit: 300 }
        ])
        yield* post("2026-03-04", true, [
          { account: revenue, debit: 100, credit: 0 },
          { account: ar, debit: 0, credit: 100 }
        ])
        yield* post("2026-03-06", true, [
          { account: bank, debit: 400, credit: 0 },
          { account: cash, debit: 0, credit: 400 }
        ])
        yield* post("2026-03-08", false, [
          { account: cash, debit: 900, credit: 0 },
          { account: revenue, debit: 0, credit: 900 }
        ])

        const reporting = yield* Reporting
        return {
          trial: yield* reporting.trialBalance(
            "2026-02-01",
            "2026-03-20"
          ),
          profitLoss: yield* reporting.profitLoss(
            "2026-02-01",
            "2026-03-20"
          ),
          balanceSheet: yield* reporting.balanceSheet("2026-03-20"),
          cashFlow: yield* reporting.cashFlow(
            "2026-02-01",
            "2026-03-20"
          ),
          ledger: yield* reporting.generalLedger(
            cash,
            "2026-02-01",
            "2026-03-20"
          ),
          dashboard: yield* reporting.dashboardAt(
            "monthly",
            Date.UTC(2026, 2, 20, 11)
          ),
          quarterly: yield* reporting.dashboardAt(
            "quarterly",
            Date.UTC(2026, 1, 15, 10)
          )
        }
      }).pipe(Effect.provide(layer))
    )

    expect(result.trial.reduce(
      (sum, row) => sum + row.totalDebit,
      0
    )).toBe(1500)
    expect(result.trial.reduce(
      (sum, row) => sum + row.totalCredit,
      0
    )).toBe(1500)
    expect(result.profitLoss).toMatchObject({
      totalRevenue: 400,
      totalExpense: 300,
      netIncome: 100
    })
    expect(result.balanceSheet.assets.total).toBe(1100)
    expect(result.balanceSheet.retainedEarnings).toBe(1100)
    expect(result.balanceSheet.totalLiabEquity).toBe(1100)
    expect(result.cashFlow).toMatchObject({
      totalMovement: -500,
      openingCash: 1000,
      closingCash: 500,
      netCashChange: -500,
      cashConfigured: true
    })
    expect(result.ledger.map((entry) => entry.balance)).toEqual([
      -200,
      -500,
      -900
    ])
    expect(result.dashboard).toMatchObject({
      cashBalance: 500,
      cashConfigured: true,
      granularity: "monthly",
      asOf: "2026-03-20"
    })
    expect(result.dashboard.trends.at(-2)).toMatchObject({
      label: "Feb 2026",
      revenue: 500,
      expenses: 300,
      netIncome: 200,
      netCashMovement: -500,
      closingCash: 500
    })
    expect(result.dashboard.trends.at(-1)).toMatchObject({
      label: "Mar 2026",
      endDate: "2026-03-20",
      isPartial: true,
      revenue: -100,
      netCashMovement: 0,
      closingCash: 500
    })
    expect(result.quarterly.trends.at(-1)).toMatchObject({
      label: "Q1 2026",
      startDate: "2026-01-01",
      endDate: "2026-02-15",
      isPartial: true,
      revenue: 1500
    })
  })

  test("returns unavailable cash values without classified accounts", async () => {
    const { layer } = await setup()
    const dashboard = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        yield* sql`UPDATE accounts SET is_cash = 0`
        const reporting = yield* Reporting
        return {
          cashFlow: yield* reporting.cashFlow(
            "2026-01-01",
            "2026-12-31"
          ),
          dashboard: yield* reporting.dashboardAt(
            "monthly",
            Date.UTC(2026, 5, 5)
          )
        }
      }).pipe(Effect.provide(layer))
    )

    expect(dashboard.cashFlow.cashConfigured).toBe(false)
    expect(dashboard.dashboard.cashBalance).toBeNull()
    expect(dashboard.dashboard.trends.every((trend) =>
      trend.closingCash === null && trend.netCashMovement === null
    )).toBe(true)
  })
})
