import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"

export type TrialBalanceRow = {
  readonly accountId: number
  readonly accountCode: string
  readonly accountName: string
  readonly accountType: string
  readonly normalBalance: string
  readonly totalDebit: number
  readonly totalCredit: number
  readonly balance: number
}

export type ProfitLossRow = {
  readonly accountCode: string
  readonly accountName: string
  readonly accountType: string
  readonly amount: number
}

export type ProfitLossReport = {
  readonly revenue: ReadonlyArray<ProfitLossRow>
  readonly expenses: ReadonlyArray<ProfitLossRow>
  readonly totalRevenue: number
  readonly totalExpense: number
  readonly netIncome: number
}

export type BalanceSheetRow = {
  readonly accountCode: string
  readonly accountName: string
  readonly balance: number
}

export type BalanceSheetSection = {
  readonly accounts: ReadonlyArray<BalanceSheetRow>
  readonly total: number
}

export type BalanceSheetReport = {
  readonly assets: BalanceSheetSection
  readonly liabilities: BalanceSheetSection
  readonly equity: BalanceSheetSection
  readonly retainedEarnings: number
  readonly totalLiabEquity: number
}

export type CashFlowReport = {
  readonly movements: ReadonlyArray<{
    readonly accountCode: string
    readonly accountName: string
    readonly amount: number
  }>
  readonly totalMovement: number
  readonly netCashChange: number
  readonly openingCash: number
  readonly closingCash: number
  readonly cashConfigured: boolean
}

export type GeneralLedgerEntry = {
  readonly entryDate: string
  readonly reference: string
  readonly description: string
  readonly sourceType: string
  readonly debit: number
  readonly credit: number
  readonly balance: number
}

export type DashboardTrend = {
  readonly label: string
  readonly startDate: string
  readonly endDate: string
  readonly isPartial: boolean
  readonly revenue: number
  readonly expenses: number
  readonly netIncome: number
  readonly netCashMovement: number | null
  readonly closingCash: number | null
}

export type DashboardData = {
  readonly cashBalance: number | null
  readonly cashConfigured: boolean
  readonly monthlyRevenue: number
  readonly monthlyExpenses: number
  readonly outstandingInvoices: number
  readonly outstandingBills: number
  readonly recentTransactions: ReadonlyArray<{
    readonly id: number
    readonly entryDate: string
    readonly reference: string
    readonly description: string
    readonly amount: number
    readonly sourceType: string
  }>
  readonly granularity: "monthly" | "quarterly"
  readonly asOf: string
  readonly trends: ReadonlyArray<DashboardTrend>
}

export class ReportingStoreError extends Data.TaggedError(
  "ReportingStoreError"
)<{
  readonly cause: unknown
}> {}

export interface Reporting {
  readonly trialBalance: (
    from: string,
    to: string
  ) => Effect.Effect<ReadonlyArray<TrialBalanceRow>, ReportingStoreError>
  readonly profitLoss: (
    from: string,
    to: string
  ) => Effect.Effect<ProfitLossReport, ReportingStoreError>
  readonly balanceSheet: (
    asOf: string
  ) => Effect.Effect<BalanceSheetReport, ReportingStoreError>
  readonly cashFlow: (
    from: string,
    to: string
  ) => Effect.Effect<CashFlowReport, ReportingStoreError>
  readonly account: (
    id: number
  ) => Effect.Effect<
    { readonly code: string; readonly name: string } | undefined,
    ReportingStoreError
  >
  readonly generalLedger: (
    accountId: number,
    from: string,
    to: string
  ) => Effect.Effect<ReadonlyArray<GeneralLedgerEntry>, ReportingStoreError>
  readonly dashboardAt: (
    granularity: "monthly" | "quarterly",
    atMilliseconds: number
  ) => Effect.Effect<DashboardData, ReportingStoreError>
}

export const Reporting = Context.GenericTag<Reporting>("latasya/Reporting")

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new ReportingStoreError({ cause }))
  )

const withDateRange = (
  base: string,
  from: string,
  to: string,
  alias = "je"
) => {
  let query = base
  const params: Array<unknown> = []
  if (from !== "") {
    query += ` AND ${alias}.entry_date >= ?`
    params.push(from)
  }
  if (to !== "") {
    query += ` AND ${alias}.entry_date <= ?`
    params.push(to)
  }
  return { query, params }
}

type TrialBalanceDbRow = {
  readonly account_id: number
  readonly account_code: string
  readonly account_name: string
  readonly account_type: string
  readonly normal_balance: string
  readonly total_debit: number
  readonly total_credit: number
}

type ProfitLossDbRow = {
  readonly account_code: string
  readonly account_name: string
  readonly account_type: string
  readonly net_amount: number
}

type BalanceSheetDbRow = {
  readonly account_code: string
  readonly account_name: string
  readonly account_type: string
  readonly normal_balance: string
  readonly total_debit: number
  readonly total_credit: number
}

type MonthTrend = {
  readonly month: string
  readonly startDate: string
  readonly endDate: string
  readonly isPartial: boolean
  revenue: number
  expenses: number
  netIncome: number
  netCashMovement: number | null
  closingCash: number | null
}

const pad2 = (value: number) => String(value).padStart(2, "0")

const dateString = (year: number, month: number, day: number) =>
  `${year}-${pad2(month + 1)}-${pad2(day)}`

const monthStart = (year: number, month: number) =>
  new Date(Date.UTC(year, month, 1))

const addMonths = (date: Date, months: number) =>
  new Date(Date.UTC(
    date.getUTCFullYear(),
    date.getUTCMonth() + months,
    1
  ))

const endOfMonth = (date: Date) =>
  new Date(Date.UTC(
    date.getUTCFullYear(),
    date.getUTCMonth() + 1,
    0
  ))

const formatDate = (date: Date) =>
  dateString(
    date.getUTCFullYear(),
    date.getUTCMonth(),
    date.getUTCDate()
  )

const monthNames = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec"
] as const

const aggregateBucket = (
  bucket: ReadonlyArray<MonthTrend>,
  bucketSize: number,
  cashConfigured: boolean
): DashboardTrend => {
  const first = bucket[0] as MonthTrend
  const last = bucket[bucket.length - 1] as MonthTrend
  const year = Number(first.startDate.slice(0, 4))
  const month = Number(first.startDate.slice(5, 7))
  const label = bucketSize === 1
    ? `${monthNames[month - 1]} ${year}`
    : `Q${Math.floor((month - 1) / 3) + 1} ${year}`
  const revenue = bucket.reduce((sum, item) => sum + item.revenue, 0)
  const expenses = bucket.reduce((sum, item) => sum + item.expenses, 0)
  return {
    label,
    startDate: first.startDate,
    endDate: last.endDate,
    isPartial: last.isPartial,
    revenue,
    expenses,
    netIncome: revenue - expenses,
    netCashMovement: cashConfigured
      ? bucket.reduce(
        (sum, item) => sum + (item.netCashMovement ?? 0),
        0
      )
      : null,
    closingCash: cashConfigured ? last.closingCash : null
  }
}

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient

  const trialBalance: Reporting["trialBalance"] = (from, to) => {
    const dateRange = withDateRange(`
      SELECT
        a.id AS account_id,
        a.code AS account_code,
        a.name AS account_name,
        a.account_type,
        a.normal_balance,
        COALESCE(SUM(jl.debit), 0) AS total_debit,
        COALESCE(SUM(jl.credit), 0) AS total_credit
      FROM accounts a
      LEFT JOIN journal_lines jl ON jl.account_id = a.id
      LEFT JOIN journal_entries je
        ON je.id = jl.entry_id AND je.is_posted = 1
      WHERE 1 = 1`, from, to)
    const query = `${dateRange.query}
      GROUP BY
        a.id, a.code, a.name, a.account_type, a.normal_balance
      HAVING
        COALESCE(SUM(jl.debit), 0) != 0
        OR COALESCE(SUM(jl.credit), 0) != 0
      ORDER BY a.code
    `
    return store(sql.unsafe<TrialBalanceDbRow>(
      query,
      dateRange.params
    )).pipe(
      Effect.map((rows) => rows.map((row) => ({
        accountId: row.account_id,
        accountCode: row.account_code,
        accountName: row.account_name,
        accountType: row.account_type,
        normalBalance: row.normal_balance,
        totalDebit: row.total_debit,
        totalCredit: row.total_credit,
        balance: row.total_debit - row.total_credit
      })))
    )
  }

  const profitLoss: Reporting["profitLoss"] = (from, to) => {
    const dateRange = withDateRange(`
      SELECT
        a.code AS account_code,
        a.name AS account_name,
        a.account_type,
        COALESCE(SUM(jl.credit), 0)
          - COALESCE(SUM(jl.debit), 0) AS net_amount
      FROM accounts a
      JOIN journal_lines jl ON jl.account_id = a.id
      JOIN journal_entries je
        ON je.id = jl.entry_id AND je.is_posted = 1
      WHERE a.account_type IN ('revenue', 'expense')`, from, to)
    const query = `${dateRange.query}
      GROUP BY a.id, a.code, a.name, a.account_type
      HAVING net_amount != 0
      ORDER BY a.code
    `
    return store(sql.unsafe<ProfitLossDbRow>(
      query,
      dateRange.params
    )).pipe(
      Effect.map((rows) => {
        const revenue: Array<ProfitLossRow> = []
        const expenses: Array<ProfitLossRow> = []
        let totalRevenue = 0
        let totalExpense = 0
        for (const row of rows) {
          if (row.account_type === "revenue") {
            revenue.push({
              accountCode: row.account_code,
              accountName: row.account_name,
              accountType: row.account_type,
              amount: row.net_amount
            })
            totalRevenue += row.net_amount
          } else {
            expenses.push({
              accountCode: row.account_code,
              accountName: row.account_name,
              accountType: row.account_type,
              amount: -row.net_amount
            })
            totalExpense += -row.net_amount
          }
        }
        return {
          revenue,
          expenses,
          totalRevenue,
          totalExpense,
          netIncome: totalRevenue - totalExpense
        }
      })
    )
  }

  const balanceSheet: Reporting["balanceSheet"] = (asOf) =>
    Effect.gen(function*() {
      const rows = yield* store(sql<BalanceSheetDbRow>`
        SELECT
          a.code AS account_code,
          a.name AS account_name,
          a.account_type,
          a.normal_balance,
          COALESCE(SUM(jl.debit), 0) AS total_debit,
          COALESCE(SUM(jl.credit), 0) AS total_credit
        FROM accounts a
        JOIN journal_lines jl ON jl.account_id = a.id
        JOIN journal_entries je
          ON je.id = jl.entry_id AND je.is_posted = 1
        WHERE
          a.account_type IN ('asset', 'liability', 'equity')
          AND je.entry_date <= ${asOf}
        GROUP BY
          a.id, a.code, a.name, a.account_type, a.normal_balance
        HAVING total_debit != 0 OR total_credit != 0
        ORDER BY a.code
      `)
      const assets: Array<BalanceSheetRow> = []
      const liabilities: Array<BalanceSheetRow> = []
      const equity: Array<BalanceSheetRow> = []
      let assetTotal = 0
      let liabilityTotal = 0
      let equityTotal = 0
      for (const row of rows) {
        const balance = row.normal_balance === "debit"
          ? row.total_debit - row.total_credit
          : row.total_credit - row.total_debit
        const item = {
          accountCode: row.account_code,
          accountName: row.account_name,
          balance
        }
        if (row.account_type === "asset") {
          assets.push(item)
          assetTotal += balance
        } else if (row.account_type === "liability") {
          liabilities.push(item)
          liabilityTotal += balance
        } else if (row.account_type === "equity") {
          equity.push(item)
          equityTotal += balance
        }
      }
      const retainedEarnings = (yield* profitLoss("", asOf)).netIncome
      return {
        assets: { accounts: assets, total: assetTotal },
        liabilities: {
          accounts: liabilities,
          total: liabilityTotal
        },
        equity: { accounts: equity, total: equityTotal },
        retainedEarnings,
        totalLiabEquity:
          liabilityTotal + equityTotal + retainedEarnings
      }
    })

  const cashFlow: Reporting["cashFlow"] = (from, to) =>
    Effect.gen(function*() {
      const count = yield* store(sql<{ readonly count: number }>`
        SELECT COUNT(*) AS count FROM accounts WHERE is_cash = 1
      `)
      if ((count[0]?.count ?? 0) === 0) {
        return {
          movements: [],
          totalMovement: 0,
          netCashChange: 0,
          openingCash: 0,
          closingCash: 0,
          cashConfigured: false
        }
      }
      const range = withDateRange(`
        SELECT
          a.code AS account_code,
          a.name AS account_name,
          SUM(jl.debit - jl.credit) AS amount
        FROM journal_lines jl
        JOIN journal_entries je
          ON je.id = jl.entry_id AND je.is_posted = 1
        JOIN accounts a
          ON a.id = jl.account_id AND a.is_cash = 1
        WHERE 1 = 1`, from, to)
      const movements = yield* store(sql.unsafe<{
        readonly account_code: string
        readonly account_name: string
        readonly amount: number
      }>(`${range.query}
        GROUP BY a.id, a.code, a.name
        HAVING amount != 0
        ORDER BY a.code
      `, range.params))
      let openingCash = 0
      if (from !== "") {
        const opening = yield* store(sql<{ readonly amount: number }>`
          SELECT
            COALESCE(SUM(jl.debit) - SUM(jl.credit), 0) AS amount
          FROM journal_lines jl
          JOIN journal_entries je
            ON je.id = jl.entry_id AND je.is_posted = 1
          JOIN accounts a
            ON a.id = jl.account_id AND a.is_cash = 1
          WHERE je.entry_date < ${from}
        `)
        openingCash = opening[0]?.amount ?? 0
      }
      let closingQuery = `
        SELECT
          COALESCE(SUM(jl.debit) - SUM(jl.credit), 0) AS amount
        FROM journal_lines jl
        JOIN journal_entries je
          ON je.id = jl.entry_id AND je.is_posted = 1
        JOIN accounts a
          ON a.id = jl.account_id AND a.is_cash = 1
        WHERE 1 = 1
      `
      const closingParams: Array<unknown> = []
      if (to !== "") {
        closingQuery += " AND je.entry_date <= ?"
        closingParams.push(to)
      }
      const closing = yield* store(sql.unsafe<{ readonly amount: number }>(
        closingQuery,
        closingParams
      ))
      const closingCash = closing[0]?.amount ?? 0
      return {
        movements: movements.map((row) => ({
          accountCode: row.account_code,
          accountName: row.account_name,
          amount: row.amount
        })),
        totalMovement: movements.reduce(
          (sum, row) => sum + row.amount,
          0
        ),
        netCashChange: closingCash - openingCash,
        openingCash,
        closingCash,
        cashConfigured: true
      }
    })

  const account: Reporting["account"] = (id) =>
    store(sql<{ readonly code: string; readonly name: string }>`
      SELECT code, name FROM accounts WHERE id = ${id}
    `).pipe(Effect.map((rows) => rows[0]))

  const generalLedger: Reporting["generalLedger"] = (
    accountId,
    from,
    to
  ) => {
    const range = withDateRange(`
      SELECT
        je.entry_date,
        COALESCE(je.reference, '') AS reference,
        je.description,
        COALESCE(je.source_type, '') AS source_type,
        jl.debit,
        jl.credit
      FROM journal_lines jl
      JOIN journal_entries je
        ON je.id = jl.entry_id AND je.is_posted = 1
      WHERE jl.account_id = ?`, from, to)
    const params = [accountId, ...range.params]
    return store(sql.unsafe<{
      readonly entry_date: string
      readonly reference: string
      readonly description: string
      readonly source_type: string
      readonly debit: number
      readonly credit: number
    }>(`${range.query} ORDER BY je.entry_date, je.id`, params)).pipe(
      Effect.map((rows) => {
        let balance = 0
        return rows.map((row) => {
          balance += row.debit - row.credit
          return {
            entryDate: row.entry_date,
            reference: row.reference,
            description: row.description,
            sourceType: row.source_type,
            debit: row.debit,
            credit: row.credit,
            balance
          }
        })
      })
    )
  }

  const dashboardAt: Reporting["dashboardAt"] = (
    granularity,
    atMilliseconds
  ) =>
    Effect.gen(function*() {
      const jakarta = new Date(atMilliseconds + 7 * 60 * 60 * 1000)
      const year = jakarta.getUTCFullYear()
      const month = jakarta.getUTCMonth()
      const day = jakarta.getUTCDate()
      const asOf = dateString(year, month, day)
      const bucketSize = granularity === "quarterly" ? 3 : 1
      const monthsElapsed = month % bucketSize + 1
      const monthsNeeded = 5 * bucketSize + monthsElapsed
      const currentStart = monthStart(year, month)
      const rangeStart = addMonths(currentStart, -(monthsNeeded - 1))
      const startDate = formatDate(rangeStart)
      const months: Array<MonthTrend> = []
      const indices = new Map<string, number>()
      for (let index = 0; index < monthsNeeded; index += 1) {
        const start = addMonths(rangeStart, index)
        const partial =
          start.getUTCFullYear() === year &&
          start.getUTCMonth() === month
        const end = partial
          ? new Date(Date.UTC(year, month, day))
          : endOfMonth(start)
        const key =
          `${start.getUTCFullYear()}-${pad2(start.getUTCMonth() + 1)}`
        indices.set(key, index)
        months.push({
          month: key,
          startDate: formatDate(start),
          endDate: formatDate(end),
          isPartial: partial,
          revenue: 0,
          expenses: 0,
          netIncome: 0,
          netCashMovement: null,
          closingCash: null
        })
      }

      const profitability = yield* store(sql<{
        readonly month: string
        readonly revenue: number
        readonly expenses: number
      }>`
        SELECT
          substr(je.entry_date, 1, 7) AS month,
          COALESCE(SUM(
            CASE
              WHEN a.account_type = 'revenue'
              THEN jl.credit - jl.debit
              ELSE 0
            END
          ), 0) AS revenue,
          COALESCE(SUM(
            CASE
              WHEN a.account_type = 'expense'
              THEN jl.debit - jl.credit
              ELSE 0
            END
          ), 0) AS expenses
        FROM journal_lines jl
        JOIN journal_entries je
          ON je.id = jl.entry_id AND je.is_posted = 1
        JOIN accounts a ON a.id = jl.account_id
        WHERE
          je.entry_date >= ${startDate}
          AND je.entry_date <= ${asOf}
          AND a.account_type IN ('revenue', 'expense')
        GROUP BY substr(je.entry_date, 1, 7)
      `)
      for (const row of profitability) {
        const index = indices.get(row.month)
        if (index !== undefined) {
          const item = months[index] as MonthTrend
          item.revenue = row.revenue
          item.expenses = row.expenses
          item.netIncome = row.revenue - row.expenses
        }
      }

      const cashCount = yield* store(sql<{ readonly count: number }>`
        SELECT COUNT(*) AS count FROM accounts WHERE is_cash = 1
      `)
      const cashConfigured = (cashCount[0]?.count ?? 0) > 0
      if (cashConfigured) {
        const opening = yield* store(sql<{ readonly amount: number }>`
          SELECT
            COALESCE(SUM(jl.debit - jl.credit), 0) AS amount
          FROM journal_lines jl
          JOIN journal_entries je
            ON je.id = jl.entry_id AND je.is_posted = 1
          JOIN accounts a
            ON a.id = jl.account_id AND a.is_cash = 1
          WHERE je.entry_date < ${startDate}
        `)
        let closing = opening[0]?.amount ?? 0
        const movementRows = yield* store(sql<{
          readonly month: string
          readonly movement: number
        }>`
          SELECT
            substr(je.entry_date, 1, 7) AS month,
            COALESCE(SUM(jl.debit - jl.credit), 0) AS movement
          FROM journal_lines jl
          JOIN journal_entries je
            ON je.id = jl.entry_id AND je.is_posted = 1
          JOIN accounts a
            ON a.id = jl.account_id AND a.is_cash = 1
          WHERE
            je.entry_date >= ${startDate}
            AND je.entry_date <= ${asOf}
          GROUP BY substr(je.entry_date, 1, 7)
        `)
        const movements = new Map(
          movementRows.map((row) => [row.month, row.movement])
        )
        for (const item of months) {
          const movement = movements.get(item.month) ?? 0
          closing += movement
          item.netCashMovement = movement
          item.closingCash = closing
        }
      }

      const trends: Array<DashboardTrend> = []
      let index = 0
      while (trends.length < 5) {
        trends.push(aggregateBucket(
          months.slice(index, index + bucketSize),
          bucketSize,
          cashConfigured
        ))
        index += bucketSize
      }
      trends.push(aggregateBucket(
        months.slice(index),
        bucketSize,
        cashConfigured
      ))

      const outstandingInvoices = yield* store(sql<{
        readonly amount: number
      }>`
        SELECT COALESCE(SUM(total - amount_paid), 0) AS amount
        FROM invoices
        WHERE status IN ('sent', 'partial', 'overdue')
      `)
      const outstandingBills = yield* store(sql<{
        readonly amount: number
      }>`
        SELECT COALESCE(SUM(total - amount_paid), 0) AS amount
        FROM bills
        WHERE status IN ('received', 'partial', 'overdue')
      `)
      const recent = yield* store(sql<{
        readonly id: number
        readonly entry_date: string
        readonly reference: string
        readonly description: string
        readonly amount: number
        readonly source_type: string
      }>`
        SELECT
          je.id,
          je.entry_date,
          COALESCE(je.reference, '') AS reference,
          je.description,
          COALESCE(SUM(jl.debit), 0) AS amount,
          COALESCE(je.source_type, 'manual') AS source_type
        FROM journal_entries je
        LEFT JOIN journal_lines jl ON jl.entry_id = je.id
        WHERE je.is_posted = 1 AND je.entry_date <= ${asOf}
        GROUP BY
          je.id,
          je.entry_date,
          je.reference,
          je.description,
          je.source_type
        ORDER BY je.entry_date DESC, je.id DESC
        LIMIT 10
      `)
      const current = trends[trends.length - 1] as DashboardTrend
      return {
        cashBalance: cashConfigured ? current.closingCash : null,
        cashConfigured,
        monthlyRevenue: current.revenue,
        monthlyExpenses: current.expenses,
        outstandingInvoices: outstandingInvoices[0]?.amount ?? 0,
        outstandingBills: outstandingBills[0]?.amount ?? 0,
        recentTransactions: recent.map((row) => ({
          id: row.id,
          entryDate: row.entry_date,
          reference: row.reference,
          description: row.description,
          amount: row.amount,
          sourceType: row.source_type
        })),
        granularity,
        asOf,
        trends
      }
    })

  return Reporting.of({
    trialBalance,
    profitLoss,
    balanceSheet,
    cashFlow,
    account,
    generalLedger,
    dashboardAt
  })
})

export const ReportingLive = Layer.effect(Reporting, make)
