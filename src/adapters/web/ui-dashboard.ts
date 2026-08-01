import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import {
  Reporting,
  type DashboardData
} from "../../domain/accounting/reporting.ts"
import {
  protectedUiHandler,
  renderUiPage,
  uiPlainError
} from "./ui-auth.ts"

const templateDashboardData = (data: DashboardData) => ({
  CashBalance: data.cashBalance ?? 0,
  CashConfigured: data.cashConfigured,
  MonthlyRevenue: data.monthlyRevenue,
  MonthlyExpenses: data.monthlyExpenses,
  OutstandingInvoices: data.outstandingInvoices,
  OutstandingBills: data.outstandingBills,
  RecentTransactions: data.recentTransactions.map((item) => ({
    ID: item.id,
    EntryDate: item.entryDate,
    Reference: item.reference,
    Description: item.description,
    Amount: item.amount,
    SourceType: item.sourceType
  })),
  Granularity: data.granularity,
  AsOf: data.asOf,
  Trends: data.trends.map((trend) => {
    const json = {
      label: trend.label,
      start_date: trend.startDate,
      end_date: trend.endDate,
      is_partial: trend.isPartial,
      revenue: trend.revenue,
      expenses: trend.expenses,
      net_income: trend.netIncome,
      net_cash_movement: trend.netCashMovement,
      closing_cash: trend.closingCash
    }
    return {
      Label: trend.label,
      StartDate: trend.startDate,
      EndDate: trend.endDate,
      IsPartial: trend.isPartial,
      Revenue: trend.revenue,
      Expenses: trend.expenses,
      NetIncome: trend.netIncome,
      NetCashMovement: trend.netCashMovement,
      ClosingCash: trend.closingCash,
      toJSON: () => json
    }
  })
})

const dashboardPage = protectedUiHandler((authenticated, request) => {
    const query = new URL(request.url, "http://localhost").searchParams
    const present = query.has("granularity")
    const raw = query.get("granularity") ?? ""
    if (present && raw !== "monthly" && raw !== "quarterly") {
      return Effect.succeed(uiPlainError(
        400,
        "Invalid granularity parameter: use monthly or quarterly"
      ))
    }
    const granularity = present
      ? raw as "monthly" | "quarterly"
      : "monthly"
    return Effect.gen(function*() {
      const reporting = yield* Reporting
      const data = yield* reporting.dashboardAt(granularity, Date.now())
      return renderUiPage(
        request,
        "dashboard/index",
        "Dashboard",
        templateDashboardData(data),
        authenticated
      )
    }).pipe(
      Effect.catchTag(
        "ReportingStoreError",
        () => Effect.succeed(uiPlainError(500, "Internal Server Error"))
      )
    )
})
const addDashboardRoute = HttpRouter.get(
  "/dashboard/",
  Effect.gen(function*() {
    const request = yield* HttpServerRequest.HttpServerRequest
    if (new URL(request.originalUrl, "http://localhost").pathname ===
      "/dashboard") {
      return HttpServerResponse.empty({
        status: 301,
        headers: { location: "/dashboard/" }
      })
    }
    return yield* dashboardPage
  })
)

export const addUiDashboardRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(addDashboardRoute)
