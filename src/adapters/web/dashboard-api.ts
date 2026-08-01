import { HttpRouter } from "@effect/platform"
import { Effect } from "effect"
import { Reporting } from "../../domain/accounting/reporting.ts"
import { apiError, jsonResponse } from "./api-response.ts"
import { protectedApiHandler } from "./auth-api.ts"

const addDashboardRoute = HttpRouter.get(
  "/api/v1/dashboard",
  protectedApiHandler((_authentication, request) => {
    const query = new URL(request.url, "http://localhost").searchParams
    const present = query.has("granularity")
    const raw = query.get("granularity") ?? ""
    if (present && raw !== "monthly" && raw !== "quarterly") {
      return Effect.succeed(apiError(
        400,
        "invalid_request",
        "unsupported dashboard granularity",
        { granularity: "must be one of: monthly, quarterly" }
      ))
    }
    const granularity = present
      ? raw as "monthly" | "quarterly"
      : "monthly"
    return Effect.gen(function*() {
      const reporting = yield* Reporting
      const data = yield* reporting.dashboardAt(
        granularity,
        Date.now()
      )
      return jsonResponse({
        data: {
          cash_balance: data.cashBalance === null
            ? null
            : String(data.cashBalance),
          cash_configured: data.cashConfigured,
          monthly_revenue: String(data.monthlyRevenue),
          monthly_expenses: String(data.monthlyExpenses),
          outstanding_invoices: String(data.outstandingInvoices),
          outstanding_bills: String(data.outstandingBills),
          recent_transactions: data.recentTransactions.map((item) => ({
            id: item.id,
            entry_date: item.entryDate,
            reference: item.reference,
            description: item.description,
            amount: String(item.amount),
            source_type: item.sourceType
          })),
          granularity: data.granularity,
          as_of: data.asOf,
          trends: data.trends.map((trend) => ({
            label: trend.label,
            start_date: trend.startDate,
            end_date: trend.endDate,
            is_partial: trend.isPartial,
            revenue: String(trend.revenue),
            expenses: String(trend.expenses),
            net_income: String(trend.netIncome),
            net_cash_movement: trend.netCashMovement === null
              ? null
              : String(trend.netCashMovement),
            closing_cash: trend.closingCash === null
              ? null
              : String(trend.closingCash)
          }))
        }
      })
    }).pipe(
      Effect.catchTag(
        "ReportingStoreError",
        () => Effect.succeed(apiError(
          500,
          "internal_error",
          "failed to get dashboard data"
        ))
      )
    )
  })
)

export const addDashboardApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(addDashboardRoute)
