import { HttpRouter } from "@effect/platform"
import { Effect, Option } from "effect"
import {
  type Account,
  Accounts
} from "../../domain/accounting/accounts.ts"
import {
  Reporting,
  type BalanceSheetReport,
  type CashFlowReport,
  type GeneralLedgerEntry,
  type ProfitLossReport,
  type TrialBalanceRow
} from "../../domain/accounting/reporting.ts"
import {
  protectedUiHandler,
  renderUiPage,
  uiPlainError
} from "./ui-auth.ts"

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

const accountRow = (account: Account) => ({
  ID: account.id,
  Code: account.code,
  Name: account.name,
  AccountType: account.account_type,
  NormalBalance: account.normal_balance,
  IsActive: account.is_active
})

const trialBalanceRow = (row: TrialBalanceRow) => ({
  AccountID: row.accountId,
  AccountCode: row.accountCode,
  AccountName: row.accountName,
  AccountType: row.accountType,
  NormalBalance: row.normalBalance,
  TotalDebit: row.totalDebit,
  TotalCredit: row.totalCredit,
  Balance: row.balance
})

const profitLossData = (report: ProfitLossReport) => {
  const row = (item: ProfitLossReport["revenue"][number]) => ({
    AccountCode: item.accountCode,
    AccountName: item.accountName,
    AccountType: item.accountType,
    Amount: item.amount
  })
  return {
    Revenue: report.revenue.map(row),
    Expenses: report.expenses.map(row),
    TotalRevenue: report.totalRevenue,
    TotalExpense: report.totalExpense,
    NetIncome: report.netIncome
  }
}

const balanceSheetData = (report: BalanceSheetReport) => {
  const section = (value: BalanceSheetReport["assets"]) => ({
    Accounts: value.accounts.map((account) => ({
      AccountCode: account.accountCode,
      AccountName: account.accountName,
      Balance: account.balance
    })),
    Total: value.total
  })
  return {
    Assets: section(report.assets),
    Liabilities: section(report.liabilities),
    Equity: section(report.equity),
    RetainedEarnings: report.retainedEarnings,
    TotalLiabEquity: report.totalLiabEquity
  }
}

const cashFlowData = (report: CashFlowReport) => ({
  Movements: report.movements.map((item) => ({
    AccountCode: item.accountCode,
    AccountName: item.accountName,
    Amount: item.amount
  })),
  TotalMovement: report.totalMovement,
  NetCashChange: report.netCashChange,
  OpeningCash: report.openingCash,
  ClosingCash: report.closingCash,
  CashConfigured: report.cashConfigured
})

const ledgerEntry = (entry: GeneralLedgerEntry) => ({
  EntryDate: entry.entryDate,
  Reference: entry.reference,
  Description: entry.description,
  SourceType: entry.sourceType,
  Debit: entry.debit,
  Credit: entry.credit,
  Balance: entry.balance
})

const internalError = () =>
  Effect.succeed(uiPlainError(500, "Internal Server Error"))

const addTrialBalanceRoute = HttpRouter.get(
  "/dashboard/reports/trial-balance",
  protectedUiHandler((authenticated, request) => {
    const { from, to } = dateRange(request.url)
    return Effect.gen(function*() {
      const reporting = yield* Reporting
      const rows = yield* reporting.trialBalance(from, to)
      return renderUiPage(
        request,
        "reports/trial_balance",
        "Trial Balance",
        {
          Rows: rows.map(trialBalanceRow),
          TotalDebit: rows.reduce((sum, row) => sum + row.totalDebit, 0),
          TotalCredit: rows.reduce((sum, row) => sum + row.totalCredit, 0),
          From: from,
          To: to
        },
        authenticated
      )
    }).pipe(Effect.catchTag("ReportingStoreError", internalError))
  })
)

const addProfitLossRoute = HttpRouter.get(
  "/dashboard/reports/profit-loss",
  protectedUiHandler((authenticated, request) => {
    const { from, to } = dateRange(request.url)
    return Effect.gen(function*() {
      const reporting = yield* Reporting
      const report = yield* reporting.profitLoss(from, to)
      return renderUiPage(
        request,
        "reports/profit_loss",
        "Profit & Loss",
        { Report: profitLossData(report), From: from, To: to },
        authenticated
      )
    }).pipe(Effect.catchTag("ReportingStoreError", internalError))
  })
)

const addBalanceSheetRoute = HttpRouter.get(
  "/dashboard/reports/balance-sheet",
  protectedUiHandler((authenticated, request) => {
    const query = new URL(request.url, "http://localhost").searchParams
    const asOf = query.get("date") || localDate(new Date())
    return Effect.gen(function*() {
      const reporting = yield* Reporting
      const report = yield* reporting.balanceSheet(asOf)
      return renderUiPage(
        request,
        "reports/balance_sheet",
        "Balance Sheet",
        { Report: balanceSheetData(report), AsOf: asOf },
        authenticated
      )
    }).pipe(Effect.catchTag("ReportingStoreError", internalError))
  })
)

const addCashFlowRoute = HttpRouter.get(
  "/dashboard/reports/cash-flow",
  protectedUiHandler((authenticated, request) => {
    const { from, to } = dateRange(request.url)
    return Effect.gen(function*() {
      const reporting = yield* Reporting
      const report = yield* reporting.cashFlow(from, to)
      return renderUiPage(
        request,
        "reports/cash_flow",
        "Cash Flow",
        { Report: cashFlowData(report), From: from, To: to },
        authenticated
      )
    }).pipe(Effect.catchTag("ReportingStoreError", internalError))
  })
)

const atoi = (value: string | null) => {
  const parsed = Number.parseInt(value ?? "", 10)
  return Number.isNaN(parsed) ? 0 : parsed
}

const addGeneralLedgerRoute = HttpRouter.get(
  "/dashboard/reports/general-ledger",
  protectedUiHandler((authenticated, request) => {
    const { from, to } = dateRange(request.url)
    const accountId = atoi(
      new URL(request.url, "http://localhost").searchParams.get("account")
    )
    return Effect.gen(function*() {
      const accounts = yield* Accounts
      const reporting = yield* Reporting
      const allAccounts = yield* accounts.list({ type: "", search: "" }).pipe(
        Effect.orElseSucceed(() => [])
      )
      let entries: ReadonlyArray<GeneralLedgerEntry> = []
      let selectedAccount: Account | undefined
      if (accountId > 0) {
        entries = yield* reporting.generalLedger(accountId, from, to)
        selectedAccount = Option.getOrUndefined(
          yield* accounts.get(accountId).pipe(Effect.option)
        )
      }
      const totalDebit = entries.reduce((sum, entry) => sum + entry.debit, 0)
      const totalCredit = entries.reduce(
        (sum, entry) => sum + entry.credit,
        0
      )
      const net = selectedAccount?.normal_balance === "credit"
        ? totalCredit - totalDebit
        : totalDebit - totalCredit
      return renderUiPage(
        request,
        "reports/general_ledger",
        "General Ledger",
        {
          Accounts: allAccounts.filter((account) => account.is_active).map(
            accountRow
          ),
          Entries: entries.map(ledgerEntry),
          SelectedAccount: selectedAccount === undefined
            ? null
            : accountRow(selectedAccount),
          AccountID: accountId,
          From: from,
          To: to,
          TotalDebit: totalDebit,
          TotalCredit: totalCredit,
          Net: net
        },
        authenticated
      )
    }).pipe(Effect.catchTag("ReportingStoreError", internalError))
  })
)

export const addUiReportRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) =>
  router.pipe(
    addTrialBalanceRoute,
    addProfitLossRoute,
    addBalanceSheetRoute,
    addCashFlowRoute,
    addGeneralLedgerRoute
  )
