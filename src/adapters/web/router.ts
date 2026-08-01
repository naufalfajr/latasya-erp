import { HttpRouter } from "@effect/platform"
import { addAccountApiRoutes } from "./accounts-api.ts"
import { addApiTokenApiRoutes } from "./api-tokens-api.ts"
import { addAuditApiRoutes } from "./audit-api.ts"
import { addAuthApiRoutes } from "./auth-api.ts"
import { addBillApiRoutes } from "./bills-api.ts"
import { addContactApiRoutes } from "./contacts-api.ts"
import { addCreditNoteApiRoutes } from "./credit-notes-api.ts"
import { addDashboardApiRoutes } from "./dashboard-api.ts"
import { addExpenseApiRoutes } from "./expenses-api.ts"
import { addHealthRoute } from "./health.ts"
import { addIncomeApiRoutes } from "./income-api.ts"
import { addInvoiceApiRoutes } from "./invoices-api.ts"
import { addJournalApiRoutes } from "./journals-api.ts"
import { addOpenApiRoutes } from "./openapi.ts"
import { addRoleApiRoutes } from "./roles-api.ts"
import { addReportApiRoutes } from "./reports-api.ts"
import { addSchoolCalendarApiRoutes } from "./school-calendar-api.ts"
import { addStaticRoutes } from "./static.ts"
import { addUiAuthRoutes } from "./ui-auth.ts"
import { addUiAccessRoutes } from "./ui-access.ts"
import { addUiAccountRoutes } from "./ui-accounts.ts"
import { addUiBillRoutes } from "./ui-bills.ts"
import { addUiContactRoutes } from "./ui-contacts.ts"
import { addUiCreditNoteRoutes } from "./ui-credit-notes.ts"
import { addUiDashboardRoutes } from "./ui-dashboard.ts"
import { addUiJournalRoutes } from "./ui-journals.ts"
import { addUiIncomeExpenseRoutes } from "./ui-income-expenses.ts"
import { addUiInvoiceRoutes } from "./ui-invoices.ts"
import { addUiPartialRoutes } from "./ui-partials.ts"
import { addPublicRoutes } from "./ui-public.ts"
import { addUiReportRoutes } from "./ui-reports.ts"
import { addUiSettingsRoutes } from "./ui-settings.ts"
import { addUserApiRoutes } from "./users-api.ts"

const addUiRoutes = (development: boolean) =>
  <E, R>(router: HttpRouter.HttpRouter<E, R>) =>
    router.pipe(
      addUiAuthRoutes(development),
      addUiDashboardRoutes,
      addUiReportRoutes,
      addUiAccountRoutes,
      addUiPartialRoutes,
      addUiJournalRoutes,
      addUiIncomeExpenseRoutes,
      addUiContactRoutes(development),
      addUiInvoiceRoutes(development),
      addUiBillRoutes,
      addUiCreditNoteRoutes,
      addUiAccessRoutes,
      addUiSettingsRoutes
    )

const addAccountingApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addAccountApiRoutes,
  addContactApiRoutes,
  addJournalApiRoutes,
  addIncomeApiRoutes,
  addExpenseApiRoutes,
  addInvoiceApiRoutes,
  addBillApiRoutes,
  addCreditNoteApiRoutes,
  addReportApiRoutes
)

export const makeRouter = (version: string, development = false) =>
  HttpRouter.empty.pipe(
    addHealthRoute(version),
    addStaticRoutes,
    addPublicRoutes(development),
    addAuthApiRoutes(development),
    addUiRoutes(development),
    addOpenApiRoutes,
    addApiTokenApiRoutes,
    addAuditApiRoutes,
    addDashboardApiRoutes,
    addRoleApiRoutes,
    addUserApiRoutes,
    addAccountingApiRoutes,
    addSchoolCalendarApiRoutes
  )
