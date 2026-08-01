import { HttpRouter, HttpServerResponse } from "@effect/platform"
import { Effect } from "effect"
import { Accounts } from "../../domain/accounting/accounts.ts"
import { protectedUiHandler } from "./ui-auth.ts"
import { partialTemplate } from "./template-assets.ts"

const renderLine = (
  partial: string,
  definition: string,
  accountType: string
) =>
  protectedUiHandler(() =>
    Effect.gen(function*() {
      const accounts = yield* Accounts
      const values = yield* accounts.list({
        type: accountType,
        search: ""
      }).pipe(Effect.orElseSucceed(() => []))
      const html = partialTemplate(partial).render(definition, {
        Accounts: values
          .filter((account) => account.is_active)
          .map((account) => ({
            ID: account.id,
            Code: account.code,
            Name: account.name
          }))
      })
      return HttpServerResponse.text(html, {
        headers: { "content-type": "text/html; charset=utf-8" }
      })
    })
  )

const addJournalLine = HttpRouter.get(
  "/dashboard/htmx/journal-line",
  renderLine("journals/line_partial", "journal-line", "")
)

const addInvoiceLine = HttpRouter.get(
  "/dashboard/htmx/invoice-line",
  renderLine("invoices/line_partial", "invoice-line", "revenue")
)

const addBillLine = HttpRouter.get(
  "/dashboard/htmx/bill-line",
  renderLine("bills/line_partial", "bill-line", "expense")
)

const addCreditNoteLine = HttpRouter.get(
  "/dashboard/htmx/credit-note-line",
  renderLine(
    "credit_notes/line_partial",
    "credit-note-line",
    "revenue"
  )
)

export const addUiPartialRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) =>
  router.pipe(
    addJournalLine,
    addInvoiceLine,
    addBillLine,
    addCreditNoteLine
  )
