import { HttpRouter } from "@effect/platform"
import { Effect } from "effect"
import { Reporting } from "../../domain/accounting/reporting.ts"
import { apiError, jsonResponse } from "./api-response.ts"
import { protectedApiHandler } from "./auth-api.ts"

const localDate = (date: Date) =>
  `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}` +
  `-${String(date.getDate()).padStart(2, "0")}`

const dateRange = (url: string) => {
  const query = new URL(url, "http://localhost").searchParams
  let from = query.get("from") ?? ""
  let to = query.get("to") ?? ""
  if (from === "" || to === "") {
    const now = new Date()
    from =
      `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01`
    to = localDate(now)
  }
  return { from, to }
}

const internal = (message: string) =>
  Effect.succeed(apiError(500, "internal_error", message))

const addTrialBalanceRoute = HttpRouter.get(
  "/api/v1/reports/trial-balance",
  protectedApiHandler((_authentication, request) => {
    const { from, to } = dateRange(request.url)
    return Effect.gen(function*() {
      const reporting = yield* Reporting
      const rows = yield* reporting.trialBalance(from, to)
      return jsonResponse({
        data: {
          rows: rows.map((row) => ({
            account_id: row.accountId,
            account_code: row.accountCode,
            account_name: row.accountName,
            account_type: row.accountType,
            normal_balance: row.normalBalance,
            total_debit: String(row.totalDebit),
            total_credit: String(row.totalCredit),
            balance: String(row.balance)
          })),
          total_debit: String(rows.reduce(
            (sum, row) => sum + row.totalDebit,
            0
          )),
          total_credit: String(rows.reduce(
            (sum, row) => sum + row.totalCredit,
            0
          )),
          from,
          to
        }
      })
    }).pipe(
      Effect.catchTag(
        "ReportingStoreError",
        () => internal("failed to generate trial balance")
      )
    )
  })
)

const addProfitLossRoute = HttpRouter.get(
  "/api/v1/reports/profit-loss",
  protectedApiHandler((_authentication, request) => {
    const { from, to } = dateRange(request.url)
    return Effect.gen(function*() {
      const reporting = yield* Reporting
      const report = yield* reporting.profitLoss(from, to)
      const row = (item: typeof report.revenue[number]) => ({
        account_code: item.accountCode,
        account_name: item.accountName,
        account_type: item.accountType,
        amount: String(item.amount)
      })
      return jsonResponse({
        data: {
          revenue: report.revenue.map(row),
          expenses: report.expenses.map(row),
          total_revenue: String(report.totalRevenue),
          total_expense: String(report.totalExpense),
          net_income: String(report.netIncome),
          from,
          to
        }
      })
    }).pipe(
      Effect.catchTag(
        "ReportingStoreError",
        () => internal("failed to generate profit & loss")
      )
    )
  })
)

const section = (
  value: {
    readonly accounts: ReadonlyArray<{
      readonly accountCode: string
      readonly accountName: string
      readonly balance: number
    }>
    readonly total: number
  }
) => ({
  accounts: value.accounts.map((account) => ({
    account_code: account.accountCode,
    account_name: account.accountName,
    balance: String(account.balance)
  })),
  total: String(value.total)
})

const addBalanceSheetRoute = HttpRouter.get(
  "/api/v1/reports/balance-sheet",
  protectedApiHandler((_authentication, request) => {
    const query = new URL(request.url, "http://localhost").searchParams
    const asOf = query.get("date") || localDate(new Date())
    return Effect.gen(function*() {
      const reporting = yield* Reporting
      const report = yield* reporting.balanceSheet(asOf)
      return jsonResponse({
        data: {
          assets: section(report.assets),
          liabilities: section(report.liabilities),
          equity: section(report.equity),
          retained_earnings: String(report.retainedEarnings),
          total_liab_equity: String(report.totalLiabEquity),
          as_of: asOf
        }
      })
    }).pipe(
      Effect.catchTag(
        "ReportingStoreError",
        () => internal("failed to generate balance sheet")
      )
    )
  })
)

const addCashFlowRoute = HttpRouter.get(
  "/api/v1/reports/cash-flow",
  protectedApiHandler((_authentication, request) => {
    const { from, to } = dateRange(request.url)
    return Effect.gen(function*() {
      const reporting = yield* Reporting
      const report = yield* reporting.cashFlow(from, to)
      return jsonResponse({
        data: {
          movements: report.movements.map((item) => ({
            account_code: item.accountCode,
            account_name: item.accountName,
            amount: String(item.amount)
          })),
          total_movement: report.cashConfigured
            ? String(report.totalMovement)
            : null,
          net_cash_change: report.cashConfigured
            ? String(report.netCashChange)
            : null,
          opening_cash: report.cashConfigured
            ? String(report.openingCash)
            : null,
          closing_cash: report.cashConfigured
            ? String(report.closingCash)
            : null,
          from,
          to,
          cash_configured: report.cashConfigured
        }
      })
    }).pipe(
      Effect.catchTag(
        "ReportingStoreError",
        () => internal("failed to generate cash flow")
      )
    )
  })
)

const parseAccountId = (value: string | null) => {
  if (value === null || !/^[+-]?\d+$/.test(value)) {
    return undefined
  }
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed > 0
    ? parsed
    : undefined
}

const addGeneralLedgerRoute = HttpRouter.get(
  "/api/v1/reports/general-ledger",
  protectedApiHandler((_authentication, request) => {
    const { from, to } = dateRange(request.url)
    const query = new URL(request.url, "http://localhost").searchParams
    const accountId = parseAccountId(query.get("account"))
    if (accountId === undefined) {
      return Effect.succeed(apiError(
        400,
        "invalid_request",
        "account query parameter required (integer account id)"
      ))
    }
    return Effect.gen(function*() {
      const reporting = yield* Reporting
      const account = yield* reporting.account(accountId)
      if (account === undefined) {
        return apiError(404, "not_found", "account not found")
      }
      const entries = yield* reporting.generalLedger(
        accountId,
        from,
        to
      )
      return jsonResponse({
        data: {
          account_id: accountId,
          account_code: account.code,
          account_name: account.name,
          entries: entries.map((entry) => ({
            entry_date: entry.entryDate,
            reference: entry.reference,
            description: entry.description,
            debit: String(entry.debit),
            credit: String(entry.credit),
            balance: String(entry.balance)
          })),
          from,
          to
        }
      })
    }).pipe(
      Effect.catchTag(
        "ReportingStoreError",
        () => internal("failed to generate general ledger")
      )
    )
  })
)

export const addReportApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addTrialBalanceRoute,
  addProfitLossRoute,
  addBalanceSheetRoute,
  addCashFlowRoute,
  addGeneralLedgerRoute
)
