import accountsForm from "../../../templates/accounts/form.html" with { type: "text" }
import accountsIndex from "../../../templates/accounts/index.html" with { type: "text" }
import auditList from "../../../templates/audit/list.html" with { type: "text" }
import login from "../../../templates/auth/login.html" with { type: "text" }
import passwordChange from "../../../templates/auth/password_change.html" with { type: "text" }
import base from "../../../templates/base.html" with { type: "text" }
import billsForm from "../../../templates/bills/form.html" with { type: "text" }
import billsIndex from "../../../templates/bills/index.html" with { type: "text" }
import billsLine from "../../../templates/bills/line_partial.html" with { type: "text" }
import billsView from "../../../templates/bills/view.html" with { type: "text" }
import contactsForm from "../../../templates/contacts/form.html" with { type: "text" }
import contactsIndex from "../../../templates/contacts/index.html" with { type: "text" }
import creditNotesForm from "../../../templates/credit_notes/form.html" with { type: "text" }
import creditNotesIndex from "../../../templates/credit_notes/index.html" with { type: "text" }
import creditNotesLine from "../../../templates/credit_notes/line_partial.html" with { type: "text" }
import creditNotesView from "../../../templates/credit_notes/view.html" with { type: "text" }
import dashboardIndex from "../../../templates/dashboard/index.html" with { type: "text" }
import expensesForm from "../../../templates/expenses/form.html" with { type: "text" }
import expensesIndex from "../../../templates/expenses/index.html" with { type: "text" }
import homeIndex from "../../../templates/home/index.html" with { type: "text" }
import incomeForm from "../../../templates/income/form.html" with { type: "text" }
import incomeIndex from "../../../templates/income/index.html" with { type: "text" }
import invoicesForm from "../../../templates/invoices/form.html" with { type: "text" }
import invoicesIndex from "../../../templates/invoices/index.html" with { type: "text" }
import invoicesLine from "../../../templates/invoices/line_partial.html" with { type: "text" }
import invoicesPrint from "../../../templates/invoices/print.html" with { type: "text" }
import invoicesView from "../../../templates/invoices/view.html" with { type: "text" }
import journalsForm from "../../../templates/journals/form.html" with { type: "text" }
import journalsIndex from "../../../templates/journals/index.html" with { type: "text" }
import journalsLine from "../../../templates/journals/line_partial.html" with { type: "text" }
import journalsView from "../../../templates/journals/view.html" with { type: "text" }
import csrf from "../../../templates/partials/csrf.html" with { type: "text" }
import flash from "../../../templates/partials/flash.html" with { type: "text" }
import nav from "../../../templates/partials/nav.html" with { type: "text" }
import pagination from "../../../templates/partials/pagination.html" with { type: "text" }
import sidebar from "../../../templates/partials/sidebar.html" with { type: "text" }
import portalIndex from "../../../templates/portal/index.html" with { type: "text" }
import balanceSheet from "../../../templates/reports/balance_sheet.html" with { type: "text" }
import cashFlow from "../../../templates/reports/cash_flow.html" with { type: "text" }
import generalLedger from "../../../templates/reports/general_ledger.html" with { type: "text" }
import profitLoss from "../../../templates/reports/profit_loss.html" with { type: "text" }
import trialBalance from "../../../templates/reports/trial_balance.html" with { type: "text" }
import rolesForm from "../../../templates/roles/form.html" with { type: "text" }
import rolesIndex from "../../../templates/roles/index.html" with { type: "text" }
import apiTokens from "../../../templates/settings/api_tokens.html" with { type: "text" }
import apiTokenCreated from "../../../templates/settings/api_tokens_created.html" with { type: "text" }
import apiTokenForm from "../../../templates/settings/api_tokens_form.html" with { type: "text" }
import company from "../../../templates/settings/company.html" with { type: "text" }
import schoolCalendar from "../../../templates/settings/school_calendar.html" with { type: "text" }
import usersForm from "../../../templates/users/form.html" with { type: "text" }
import usersIndex from "../../../templates/users/index.html" with { type: "text" }
import { GoTemplateSet } from "./go-template.ts"

const asText = (source: unknown): string => source as string

const shared = [base, nav, sidebar, flash, csrf, pagination].map(asText)

const pages: Readonly<Record<string, string>> = {
  "accounts/form": asText(accountsForm),
  "accounts/index": asText(accountsIndex),
  "audit/list": asText(auditList),
  "auth/login": asText(login),
  "auth/password_change": asText(passwordChange),
  "bills/form": asText(billsForm),
  "bills/index": asText(billsIndex),
  "bills/view": asText(billsView),
  "contacts/form": asText(contactsForm),
  "contacts/index": asText(contactsIndex),
  "credit_notes/form": asText(creditNotesForm),
  "credit_notes/index": asText(creditNotesIndex),
  "credit_notes/view": asText(creditNotesView),
  "dashboard/index": asText(dashboardIndex),
  "expenses/form": asText(expensesForm),
  "expenses/index": asText(expensesIndex),
  "home/index": `{{define "index.html"}}${asText(homeIndex)}{{end}}`,
  "income/form": asText(incomeForm),
  "income/index": asText(incomeIndex),
  "invoices/form": asText(invoicesForm),
  "invoices/index": asText(invoicesIndex),
  "invoices/print":
    `{{define "print.html"}}${asText(invoicesPrint)}{{end}}`,
  "invoices/view": asText(invoicesView),
  "journals/form": asText(journalsForm),
  "journals/index": asText(journalsIndex),
  "journals/view": asText(journalsView),
  "portal/index": `{{define "index.html"}}${asText(portalIndex)}{{end}}`,
  "reports/balance_sheet": asText(balanceSheet),
  "reports/cash_flow": asText(cashFlow),
  "reports/general_ledger": asText(generalLedger),
  "reports/profit_loss": asText(profitLoss),
  "reports/trial_balance": asText(trialBalance),
  "roles/form": asText(rolesForm),
  "roles/index": asText(rolesIndex),
  "settings/api_tokens": asText(apiTokens),
  "settings/api_tokens_created": asText(apiTokenCreated),
  "settings/api_tokens_form": asText(apiTokenForm),
  "settings/company": asText(company),
  "settings/school_calendar": asText(schoolCalendar),
  "users/form": asText(usersForm),
  "users/index": asText(usersIndex)
}

const extras: Readonly<Record<string, string>> = {
  "bills/line_partial": asText(billsLine),
  "credit_notes/line_partial": asText(creditNotesLine),
  "invoices/line_partial": asText(invoicesLine),
  "journals/line_partial": asText(journalsLine)
}

export const templatePageNames = Object.freeze(Object.keys(pages))

export const pageTemplate = (
  page: string,
  extraTemplates: ReadonlyArray<string> = []
): GoTemplateSet => {
  const source = pages[page]
  if (source === undefined) {
    throw new Error(`unknown page template: ${page}`)
  }
  return new GoTemplateSet([
    ...shared,
    source,
    ...extraTemplates.map((name) => {
      const extra = extras[name]
      if (extra === undefined) {
        throw new Error(`unknown extra template: ${name}`)
      }
      return extra
    })
  ])
}

export const partialTemplate = (name: string): GoTemplateSet => {
  const source = extras[name]
  if (source === undefined) {
    throw new Error(`unknown partial template: ${name}`)
  }
  return new GoTemplateSet([source])
}
