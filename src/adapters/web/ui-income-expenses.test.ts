import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { Accounts } from "../../domain/accounting/accounts.ts"
import { Authentication } from "../../domain/auth/authentication.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { makeRouter } from "./router.ts"

const temporaryDirectories: Array<string> = []
const disposers: Array<() => Promise<void>> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-ui-cashflow-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const layer = runtimeLayer(databasePath)
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(layer)))
  const state = await Effect.runPromise(
    Effect.gen(function*() {
      const authentication = yield* Authentication
      const loggedIn = yield* authentication.login("admin", "admin")
      yield* authentication.changePassword(
        loggedIn.user,
        "admin",
        "cashflow-password",
        "cashflow-password"
      )
      const accounts = yield* Accounts
      return {
        loggedIn,
        assets: yield* accounts.list({ type: "asset", search: "" }),
        revenue: yield* accounts.list({ type: "revenue", search: "" }),
        expenses: yield* accounts.list({ type: "expense", search: "" })
      }
    }).pipe(Effect.provide(layer))
  )
  const web = HttpApp.toWebHandlerLayer(makeRouter("test", true), layer)
  disposers.push(web.dispose)
  return {
    handler: web.handler,
    cookie: `session_id=${state.loggedIn.sessionId}`,
    csrf: state.loggedIn.csrfToken,
    assetId: state.assets[0]?.id ?? 0,
    revenueId: state.revenue[0]?.id ?? 0,
    expenseId: state.expenses[0]?.id ?? 0
  }
}

const postForm = (
  handler: (request: Request) => Promise<Response>,
  path: string,
  cookie: string,
  values: Readonly<Record<string, string>>
) => handler(new Request(`http://localhost${path}`, {
  method: "POST",
  headers: {
    cookie,
    "content-type": "application/x-www-form-urlencoded"
  },
  body: new URLSearchParams(values)
}))

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("server-rendered income and expenses", () => {
  test("renders both forms and preserves legacy validation messages", async () => {
    const { handler, cookie, csrf } = await setup()
    for (const [path, title] of [
      ["/dashboard/income/new", "Record Income"],
      ["/dashboard/expenses/new", "Record Expense"]
    ] as const) {
      const response = await handler(new Request(`http://localhost${path}`, {
        headers: { cookie }
      }))
      expect(response.status).toBe(200)
      expect(await response.text()).toContain(
        `<title>${title} — Latasya ERP</title>`
      )
    }

    const invalid = await postForm(
      handler,
      "/dashboard/income",
      cookie,
      { csrf_token: csrf }
    )
    const body = await invalid.text()
    expect(body).toContain("Date is required")
    expect(body).toContain("Description is required")
    expect(body).toContain("Amount must be greater than 0")
    expect(body).toContain("Revenue account is required")
    expect(body).toContain("Deposit account is required")
  })

  test("records income and expense into their filtered lists", async () => {
    const {
      handler,
      cookie,
      csrf,
      assetId,
      revenueId,
      expenseId
    } = await setup()
    const income = await postForm(
      handler,
      "/dashboard/income",
      cookie,
      {
        csrf_token: csrf,
        entry_date: "2026-07-26",
        description: "Browser income",
        amount: "2.500.000",
        revenue_account: String(revenueId),
        deposit_account: String(assetId)
      }
    )
    const expense = await postForm(
      handler,
      "/dashboard/expenses",
      cookie,
      {
        csrf_token: csrf,
        entry_date: "2026-07-26",
        description: "Browser expense",
        amount: "750000",
        expense_account: String(expenseId),
        payment_account: String(assetId),
        vehicle_id: "1"
      }
    )

    expect(income.status).toBe(303)
    expect(income.headers.get("set-cookie")).toContain(
      "Income recorded successfully"
    )
    expect(expense.status).toBe(303)
    expect(expense.headers.get("set-cookie")).toContain(
      "Expense recorded successfully"
    )

    const incomeList = await handler(new Request(
      "http://localhost/dashboard/income?search=Browser",
      { headers: { cookie } }
    ))
    const expenseList = await handler(new Request(
      "http://localhost/dashboard/expenses?search=Browser",
      { headers: { cookie } }
    ))
    const incomeBody = await incomeList.text()
    const expenseBody = await expenseList.text()
    expect(incomeBody).toContain("Browser income")
    expect(incomeBody).toContain("Rp 2.500.000")
    expect(expenseBody).toContain("Browser expense")
    expect(expenseBody).toContain("Rp 750.000")
    expect(expenseBody).toContain("LA001")
  })
})
