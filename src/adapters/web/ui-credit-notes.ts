import { HttpRouter, HttpServerResponse } from "@effect/platform"
import { Effect } from "effect"
import { Accounts, type Account } from "../../domain/accounting/accounts.ts"
import {
  type CreditNote,
  type CreditNoteLineValues,
  CreditNotes
} from "../../domain/accounting/credit-notes.ts"
import { type Invoice, Invoices } from "../../domain/accounting/invoices.ts"
import { Audit, auditDiff } from "../../domain/audit/audit.ts"
import type { CookieAuthentication } from "../../domain/auth/authentication.ts"
import { Contacts } from "../../domain/contacts/contacts.ts"
import {
  dashboardBasePath,
  protectedUiHandler,
  renderUiPage,
  uiFlashCookie,
  uiPlainError,
  uiRedirect
} from "./ui-auth.ts"
import { requestMetadata } from "./request-metadata.ts"

type Values = {
  readonly id: number
  readonly contactId: number
  readonly invoiceId: number
  readonly cnDate: string
  readonly reason: string
  readonly taxAmount: number
  readonly notes: string
  readonly lines: ReadonlyArray<CreditNoteLineValues>
}

const reasons = [
  { Value: "cancellation", Label: "Cancellation" },
  { Value: "return", Label: "Return" },
  { Value: "discount", Label: "Discount" },
  { Value: "other", Label: "Other" }
] as const
const size = 50
const manage = (auth: CookieAuthentication) =>
  auth.user.role === "admin" ||
  auth.effectiveCapabilities.includes("invoices.manage")
const actor = (auth: CookieAuthentication) => ({
  id: auth.user.id,
  username: auth.user.username
})
const id = (value: string | undefined | null) => {
  const parsed = Number(value)
  return value !== undefined && value !== null && /^[+-]?\d+$/.test(value) &&
      Number.isSafeInteger(parsed)
    ? parsed
    : undefined
}
const integer = (value: string | null) => id(value) ?? 0
const idr = (value: string | null) => {
  const normalized = (value ?? "").trim()
    .replaceAll(".", "").replaceAll(",", "").replaceAll("Rp", "").trim()
  const parsed = Number(normalized)
  return /^[+-]?\d+$/.test(normalized) && Number.isSafeInteger(parsed)
    ? parsed
    : 0
}
const quantity = (value: string) => {
  const parsed = Number.parseFloat(value.trim().replaceAll(",", "."))
  return Number.isNaN(parsed) ? 0 : Math.round(parsed * 100)
}
const readLines = (form: URLSearchParams) => {
  const descriptions = form.getAll("line_description")
  const quantities = form.getAll("line_quantity")
  const prices = form.getAll("line_unit_price")
  const accounts = form.getAll("line_account_id")
  const result: Array<CreditNoteLineValues> = []
  descriptions.forEach((description, index) => {
    const unitPrice = idr(prices[index] ?? "")
    const accountId = integer(accounts[index] ?? "")
    if (description === "" && unitPrice === 0 && accountId === 0) return
    result.push({
      description,
      quantity: quantity(quantities[index] ?? "") || 100,
      unitPrice,
      accountId
    })
  })
  return result
}
const fromForm = (form: URLSearchParams, noteId = 0): Values => ({
  id: noteId,
  contactId: integer(form.get("contact_id")),
  invoiceId: integer(form.get("invoice_id")),
  cnDate: form.get("cn_date") ?? "",
  reason: form.get("reason") ?? "",
  taxAmount: idr(form.get("tax_amount")),
  notes: form.get("notes") ?? "",
  lines: readLines(form)
})
const fromNote = (note: CreditNote): Values => ({
  id: note.id,
  contactId: note.contact_id,
  invoiceId: note.invoice_id ?? 0,
  cnDate: note.cn_date,
  reason: note.reason,
  taxAmount: note.tax_amount,
  notes: note.notes,
  lines: (note.lines ?? []).map((line) => ({
    description: line.description,
    quantity: line.quantity,
    unitPrice: line.unit_price,
    accountId: line.account_id
  }))
})
const validate = (values: Values) => {
  const errors: Record<string, string> = {}
  if (values.contactId === 0) errors.contact_id = "Customer is required"
  if (values.cnDate === "") errors.cn_date = "Date is required"
  if (values.reason === "") errors.reason = "Reason is required"
  else if (!reasons.some((reason) => reason.Value === values.reason)) {
    errors.reason = "Invalid reason"
  }
  if (values.lines.length === 0) {
    errors.lines = "At least one line item is required"
  }
  values.lines.forEach((line, index) => {
    if (line.description === "") {
      errors[`line_${index}_desc`] = "Description required"
    }
    if (line.unitPrice <= 0) errors[`line_${index}_price`] = "Price required"
    if (line.accountId === 0) {
      errors[`line_${index}_account`] = "Account required"
    }
  })
  return errors
}
const accountData = (account: Account) => ({
  ID: account.id,
  Code: account.code,
  Name: account.name
})
const noteData = (note: CreditNote) => ({
  ID: note.id,
  CNNumber: note.cn_number,
  ContactID: note.contact_id,
  ContactName: note.contact_name ?? "",
  InvoiceID: note.invoice_id ?? 0,
  InvoiceNumber: note.invoice_number ?? "",
  CNDate: note.cn_date,
  Reason: note.reason,
  Status: note.status,
  Subtotal: note.subtotal,
  TaxAmount: note.tax_amount,
  Total: note.total,
  Notes: note.notes,
  JournalID: note.journal_id ?? 0,
  Lines: (note.lines ?? []).map((line) => ({
    Description: line.description,
    Quantity: line.quantity,
    UnitPrice: line.unit_price,
    Amount: line.amount,
    AccountID: line.account_id,
    AccountCode: line.account_code ?? "",
    AccountName: line.account_name ?? ""
  }))
})
const formNote = (values: Values) => ({
  ID: values.id,
  ContactID: values.contactId,
  InvoiceID: values.invoiceId,
  CNDate: values.cnDate,
  Reason: values.reason,
  TaxAmount: values.taxAmount,
  Notes: values.notes
})
const invoiceData = (invoice: Invoice) => ({
  ID: invoice.id,
  InvoiceNumber: invoice.invoice_number,
  ContactID: invoice.contact_id
})
const options = Effect.gen(function*() {
  const contacts = yield* Contacts
  const accounts = yield* Accounts
  const [customers, revenue] = yield* Effect.all([
    contacts.list({ type: "customer", search: "" }).pipe(
      Effect.orElseSucceed(() => [])
    ),
    accounts.list({ type: "revenue", search: "" }).pipe(
      Effect.orElseSucceed(() => [])
    )
  ])
  return {
    customers: customers.filter((item) => item.is_active),
    revenue: revenue.filter((item) => item.is_active)
  }
})
const renderForm = (
  auth: CookieAuthentication,
  request: Parameters<typeof renderUiPage>[0],
  values: Values,
  errors: Readonly<Record<string, string>>,
  isEdit: boolean,
  source?: Invoice
) => Effect.gen(function*() {
  const data = yield* options
  const response = renderUiPage(
    request,
    "credit_notes/form",
    isEdit ? "Edit Credit Note" : "New Credit Note",
    {
      CreditNote: formNote(values),
      Lines: values.lines.map((line) => ({
        Description: line.description,
        Quantity: line.quantity,
        UnitPrice: line.unitPrice,
        Amount: Math.round(line.quantity * line.unitPrice / 100),
        AccountID: line.accountId
      })),
      Contacts: data.customers.map((contact) => ({
        ID: contact.id,
        Name: contact.name
      })),
      RevenueAccounts: data.revenue.map(accountData),
      Reasons: reasons,
      Errors: errors,
      IsEdit: isEdit,
      SourceInvoice: source === undefined ? null : invoiceData(source)
    },
    auth,
    ["credit_notes/line_partial"]
  )
  return response
})
const notFound = () => uiPlainError(404, "404 page not found")
const internal = () => uiPlainError(500, "Internal Server Error")
const message = (error: unknown) =>
  typeof error === "object" && error !== null && "message" in error
    ? String(error.message)
    : String(error)
const pageUrl = (page: number, status: string, search: string) => {
  const query = new URLSearchParams()
  if (status) query.set("status", status)
  if (search) query.set("search", search)
  query.set("page", String(page))
  return `${dashboardBasePath}/credit-notes?${query}`
}
const valuesObject = (values: Values) => ({
  contactId: values.contactId,
  ...(values.invoiceId === 0 ? {} : { invoiceId: values.invoiceId }),
  cnDate: values.cnDate,
  reason: values.reason,
  taxAmount: values.taxAmount,
  notes: values.notes,
  lines: values.lines
})

const listRoute = HttpRouter.get(`${dashboardBasePath}/credit-notes`,
  protectedUiHandler((auth, request) => {
    const query = new URL(request.url, "http://localhost").searchParams
    const status = query.get("status") ?? ""
    const search = query.get("search") ?? ""
    const requested = Math.max(1, integer(query.get("page")))
    return Effect.gen(function*() {
      const notes = yield* CreditNotes
      let result = yield* notes.list({
        status,
        search,
        limit: size,
        offset: (requested - 1) * size
      })
      const totalPages = Math.max(1, Math.ceil(result.total / size))
      const page = Math.min(requested, totalPages)
      if (page !== requested) {
        result = yield* notes.list({
          status,
          search,
          limit: size,
          offset: (page - 1) * size
        })
      }
      return renderUiPage(request, "credit_notes/index", "Credit Notes", {
        CreditNotes: result.creditNotes.map(noteData),
        Filter: status,
        Search: search,
        Pagination: {
          Page: page,
          PageSize: size,
          Total: result.total,
          TotalPages: totalPages,
          HasPrev: page > 1,
          HasNext: page < totalPages,
          PrevURL: pageUrl(page - 1, status, search),
          NextURL: pageUrl(page + 1, status, search)
        }
      }, auth)
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))

const newRoute = HttpRouter.get(`${dashboardBasePath}/credit-notes/new`,
  protectedUiHandler((auth, request) => {
    const invoiceId = id(
      new URL(request.url, "http://localhost").searchParams.get("invoice_id")
    )
    if (invoiceId === undefined) {
      return renderForm(auth, request, {
        id: 0,
        contactId: 0,
        invoiceId: 0,
        cnDate: "",
        reason: "cancellation",
        taxAmount: 0,
        notes: "",
        lines: [{ description: "", quantity: 100, unitPrice: 0, accountId: 0 }]
      }, {}, false)
    }
    return Effect.gen(function*() {
      const invoices = yield* Invoices
      const invoice = yield* invoices.get(invoiceId)
      return yield* renderForm(auth, request, {
        id: 0,
        contactId: invoice.contact_id,
        invoiceId: invoice.id,
        cnDate: "",
        reason: "cancellation",
        taxAmount: Number(invoice.tax_amount),
        notes: "",
        lines: (invoice.lines ?? []).map((line) => ({
          description: line.description,
          quantity: Math.round(Number(line.quantity) * 100),
          unitPrice: Number(line.unit_price),
          accountId: line.account_id
        }))
      }, {}, false, invoice)
    }).pipe(
      Effect.catchAll(() => renderForm(auth, request, {
        id: 0,
        contactId: 0,
        invoiceId: 0,
        cnDate: "",
        reason: "cancellation",
        taxAmount: 0,
        notes: "",
        lines: [{ description: "", quantity: 100, unitPrice: 0, accountId: 0 }]
      }, {}, false))
    )
  }))

const createRoute = HttpRouter.post(`${dashboardBasePath}/credit-notes`,
  protectedUiHandler((auth, request, form) => {
    if (!manage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    const values = fromForm(form)
    const errors = validate(values)
    if (Object.keys(errors).length) {
      return renderForm(auth, request, values, errors, false)
    }
    return Effect.gen(function*() {
      const notes = yield* CreditNotes
      const created = yield* notes.create(valuesObject(values), auth.user.id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "credit_note.create",
        actor: actor(auth),
        targetType: "credit_note",
        targetId: created.id,
        targetLabel: created.cn_number,
        metadata: { after: {
          contact_id: values.contactId,
          invoice_id: values.invoiceId || null,
          cn_date: values.cnDate,
          reason: values.reason,
          total: created.total,
          line_count: values.lines.length
        } }
      })
      return uiRedirect(`${dashboardBasePath}/credit-notes/${created.id}`, {
        "set-cookie": uiFlashCookie("Credit note created")
      })
    }).pipe(Effect.catchAll((error) =>
      renderForm(auth, request, values, { general: message(error) }, false)
    ))
  }))

const viewRoute = HttpRouter.get(`${dashboardBasePath}/credit-notes/:id`,
  protectedUiHandler((auth, request) => Effect.gen(function*() {
    const noteId = id((yield* HttpRouter.params).id)
    if (noteId === undefined) return notFound()
    const notes = yield* CreditNotes
    const note = yield* notes.get(noteId)
    return renderUiPage(
      request,
      "credit_notes/view",
      `Credit Note ${note.cn_number}`,
      { CreditNote: noteData(note) },
      auth
    )
  }).pipe(Effect.catchAll(() => Effect.succeed(notFound())))))

const editRoute = HttpRouter.get(`${dashboardBasePath}/credit-notes/:id/edit`,
  protectedUiHandler((auth, request) => Effect.gen(function*() {
    const noteId = id((yield* HttpRouter.params).id)
    if (noteId === undefined) return notFound()
    const notes = yield* CreditNotes
    const note = yield* notes.get(noteId)
    if (note.status !== "draft") {
      return uiRedirect(`${dashboardBasePath}/credit-notes/${noteId}`, {
        "set-cookie": uiFlashCookie("Can only edit draft credit notes")
      })
    }
    return yield* renderForm(auth, request, fromNote(note), {}, true)
  }).pipe(Effect.catchAll(() => Effect.succeed(notFound())))))

const updateRoute = HttpRouter.post(`${dashboardBasePath}/credit-notes/:id`,
  protectedUiHandler((auth, request, form) => {
    if (!manage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const noteId = id((yield* HttpRouter.params).id)
      if (noteId === undefined) return notFound()
      const values = fromForm(form, noteId)
      const errors = validate(values)
      if (Object.keys(errors).length) {
        return yield* renderForm(auth, request, values, errors, true)
      }
      const notes = yield* CreditNotes
      const result = yield* notes.update(noteId, valuesObject(values))
      const metadata = auditDiff(
        {
          contact_id: result.before.contact_id,
          invoice_id: result.before.invoice_id ?? null,
          cn_date: result.before.cn_date,
          reason: result.before.reason,
          tax_amount: result.before.tax_amount,
          notes: result.before.notes,
          total: result.before.total
        },
        {
          contact_id: values.contactId,
          invoice_id: values.invoiceId || null,
          cn_date: values.cnDate,
          reason: values.reason,
          tax_amount: values.taxAmount,
          notes: values.notes,
          total: result.after.total
        },
        [
          "contact_id", "invoice_id", "cn_date", "reason",
          "tax_amount", "notes", "total"
        ]
      )
      if (metadata) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "credit_note.update",
          actor: actor(auth),
          targetType: "credit_note",
          targetId: noteId,
          targetLabel: result.before.cn_number,
          metadata
        })
      }
      return uiRedirect(`${dashboardBasePath}/credit-notes/${noteId}`, {
        "set-cookie": uiFlashCookie("Credit note updated")
      })
    }).pipe(Effect.catchAll((error) =>
      Effect.gen(function*() {
        const noteId = id((yield* HttpRouter.params).id) ?? 0
        return yield* renderForm(
          auth,
          request,
          fromForm(form, noteId),
          { general: message(error) },
          true
        )
      })
    ))
  }))

const stateRoute = (
  action: "issue" | "void",
  flash: string
) => HttpRouter.post(`${dashboardBasePath}/credit-notes/:id/${action}`,
  protectedUiHandler((auth, request) => {
    if (!manage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const noteId = id((yield* HttpRouter.params).id)
      if (noteId === undefined) return notFound()
      const notes = yield* CreditNotes
      const updated = action === "issue"
        ? yield* notes.issue(noteId, auth.user.id)
        : yield* notes.void(noteId, auth.user.id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: `credit_note.${action}`,
        actor: actor(auth),
        targetType: "credit_note",
        targetId: noteId,
        targetLabel: updated.cn_number,
        metadata: {
          after: { status: updated.status },
          ...(action === "issue" ? { journal_id: updated.journal_id } : {}),
          invoice_id: updated.invoice_id
        }
      })
      return uiRedirect(`${dashboardBasePath}/credit-notes/${noteId}`, {
        "set-cookie": uiFlashCookie(flash)
      })
    }).pipe(Effect.catchAll((error) =>
      Effect.gen(function*() {
        const noteId = id((yield* HttpRouter.params).id) ?? 0
        return uiRedirect(`${dashboardBasePath}/credit-notes/${noteId}`, {
          "set-cookie": uiFlashCookie(`Error: ${message(error)}`)
        })
      })
    ))
  }))

const deleteRoute = HttpRouter.del(`${dashboardBasePath}/credit-notes/:id`,
  protectedUiHandler((auth, request) => {
    if (!manage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const noteId = id((yield* HttpRouter.params).id)
      if (noteId === undefined) return notFound()
      const notes = yield* CreditNotes
      const removed = yield* notes.remove(noteId)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "credit_note.delete",
        actor: actor(auth),
        targetType: "credit_note",
        targetId: noteId,
        targetLabel: removed.cn_number,
        metadata: { before: {
          contact_id: removed.contact_id,
          invoice_id: removed.invoice_id,
          cn_date: removed.cn_date,
          status: removed.status,
          total: removed.total
        } }
      })
      return request.headers["hx-request"] === "true"
        ? HttpServerResponse.empty({ status: 200 })
        : uiRedirect(`${dashboardBasePath}/credit-notes`, {
          "set-cookie": uiFlashCookie("Credit note deleted")
        })
    }).pipe(Effect.catchAll((error) =>
      Effect.gen(function*() {
        const noteId = id((yield* HttpRouter.params).id) ?? 0
        return uiRedirect(`${dashboardBasePath}/credit-notes/${noteId}`, {
          "set-cookie": uiFlashCookie(`Error: ${message(error)}`)
        })
      })
    ))
  }))

export const addUiCreditNoteRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  listRoute,
  newRoute,
  createRoute,
  viewRoute,
  editRoute,
  updateRoute,
  deleteRoute,
  stateRoute("issue", "Credit note issued — journal entry posted"),
  stateRoute("void", "Credit note voided")
)
