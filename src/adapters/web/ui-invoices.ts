import {
  HttpRouter,
  HttpServerResponse
} from "@effect/platform"
import { SqlClient } from "@effect/sql"
import { Effect } from "effect"
import {
  Accounts,
  type Account
} from "../../domain/accounting/accounts.ts"
import {
  type CreditNoteSummary,
  type Invoice,
  type InvoiceLineValues,
  Invoices
} from "../../domain/accounting/invoices.ts"
import { Audit, auditDiff } from "../../domain/audit/audit.ts"
import type { CookieAuthentication } from "../../domain/auth/authentication.ts"
import {
  type CompanyProfile,
  CompanyProfiles
} from "../../domain/company/profile.ts"
import {
  type Contact,
  Contacts
} from "../../domain/contacts/contacts.ts"
import { renderInvoicePdf } from "../../domain/documents/invoice-pdf.ts"
import {
  dashboardBasePath,
  protectedUiHandler,
  renderUiPage,
  uiFlashCookie,
  uiPlainError,
  uiRedirect
} from "./ui-auth.ts"
import { pageTemplate } from "./template-assets.ts"
import { requestMetadata } from "./request-metadata.ts"

type FormLine = {
  readonly description: string
  readonly quantity: number
  readonly unitPrice: number
  readonly accountId: number
}

type FormValues = {
  readonly id: number
  readonly contactId: number
  readonly invoiceDate: string
  readonly dueDate: string
  readonly taxAmount: number
  readonly notes: string
  readonly lines: ReadonlyArray<FormLine>
}

const pageSize = 50

const hasManage = (authenticated: CookieAuthentication) =>
  authenticated.user.role === "admin" ||
  authenticated.effectiveCapabilities.includes("invoices.manage")

const actor = (authenticated: CookieAuthentication) => ({
  id: authenticated.user.id,
  username: authenticated.user.username
})

const parseId = (value: string | undefined) => {
  if (value === undefined || !/^[+-]?\d+$/.test(value)) {
    return undefined
  }
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : undefined
}

const parseIntOrZero = (value: string | null) => {
  const parsed = Number.parseInt(value ?? "", 10)
  return Number.isNaN(parsed) ? 0 : parsed
}

const parseIdr = (value: string | null) => {
  const normalized = (value ?? "")
    .trim()
    .replaceAll(".", "")
    .replaceAll(",", "")
    .replaceAll("Rp", "")
    .trim()
  if (!/^[+-]?\d+$/.test(normalized)) {
    return 0
  }
  const amount = Number(normalized)
  return Number.isSafeInteger(amount) ? amount : 0
}

const parseQuantity = (value: string) => {
  const parsed = Number.parseFloat(value.trim().replaceAll(",", "."))
  return Number.isNaN(parsed) ? 0 : Math.round(parsed * 100)
}

const linesFromForm = (form: URLSearchParams): ReadonlyArray<FormLine> => {
  const descriptions = form.getAll("line_description")
  const quantities = form.getAll("line_quantity")
  const prices = form.getAll("line_unit_price")
  const accountIds = form.getAll("line_account_id")
  const lines: Array<FormLine> = []
  descriptions.forEach((description, index) => {
    let quantity = parseQuantity(quantities[index] ?? "")
    if (quantity === 0) {
      quantity = 100
    }
    const unitPrice = parseIdr(prices[index] ?? "")
    const accountId = parseIntOrZero(accountIds[index] ?? "")
    if (description === "" && unitPrice === 0 && accountId === 0) {
      return
    }
    lines.push({ description, quantity, unitPrice, accountId })
  })
  return lines
}

const valuesFromForm = (form: URLSearchParams, id = 0): FormValues => ({
  id,
  contactId: parseIntOrZero(form.get("contact_id")),
  invoiceDate: form.get("invoice_date") ?? "",
  dueDate: form.get("due_date") ?? "",
  taxAmount: parseIdr(form.get("tax_amount")),
  notes: form.get("notes") ?? "",
  lines: linesFromForm(form)
})

const validation = (values: FormValues) => {
  const errors: Record<string, string> = {}
  if (values.contactId === 0) {
    errors.contact_id = "Customer is required"
  }
  if (values.invoiceDate === "") {
    errors.invoice_date = "Invoice date is required"
  }
  if (values.dueDate === "") {
    errors.due_date = "Due date is required"
  }
  if (values.lines.length === 0) {
    errors.lines = "At least one line item is required"
  }
  values.lines.forEach((line, index) => {
    if (line.description === "") {
      errors[`line_${index}_desc`] = "Description required"
    }
    if (line.unitPrice <= 0) {
      errors[`line_${index}_price`] = "Price required"
    }
    if (line.accountId === 0) {
      errors[`line_${index}_account`] = "Account required"
    }
  })
  return errors
}

const domainLines = (
  lines: ReadonlyArray<FormLine>
): ReadonlyArray<InvoiceLineValues> =>
  lines.map((line) => ({
    description: line.description,
    quantity: line.quantity,
    unitPrice: line.unitPrice,
    accountId: line.accountId
  }))

const valuesFromInvoice = (invoice: Invoice): FormValues => ({
  id: invoice.id,
  contactId: invoice.contact_id,
  invoiceDate: invoice.invoice_date,
  dueDate: invoice.due_date,
  taxAmount: Number(invoice.tax_amount),
  notes: invoice.notes,
  lines: (invoice.lines ?? []).map((line) => ({
    description: line.description,
    quantity: Math.round(Number(line.quantity) * 100),
    unitPrice: Number(line.unit_price),
    accountId: line.account_id
  }))
})

const contactPrice = (contact: Contact) => {
  let price = contact.distance_km < 4
    ? 350000
    : contact.distance_km < 7
    ? 400000
    : contact.distance_km < 10
    ? 450000
    : contact.distance_km < 13
    ? 500000
    : 550000
  if (contact.has_sibling_discount) {
    price -= 50000
  }
  if (contact.is_return_only) {
    price -= 50000
  }
  return price
}

const accountData = (account: Account) => ({
  ID: account.id,
  Code: account.code,
  Name: account.name
})

const invoiceData = (invoice: Invoice) => ({
  ID: invoice.id,
  InvoiceNumber: invoice.invoice_number,
  ContactID: invoice.contact_id,
  ContactName: invoice.contact_name ?? "",
  InvoiceDate: invoice.invoice_date,
  DueDate: invoice.due_date,
  PaidDate: invoice.paid_date ?? "",
  Status: invoice.status,
  Subtotal: Number(invoice.subtotal),
  TaxAmount: Number(invoice.tax_amount),
  Total: Number(invoice.total),
  AmountPaid: Number(invoice.amount_paid),
  AmountCredited: Number(invoice.amount_credited),
  AmountDue: Number(invoice.amount_due),
  Notes: invoice.notes,
  JournalID: invoice.journal_id ?? 0,
  Lines: (invoice.lines ?? []).map((line) => ({
    ID: line.id,
    InvoiceID: line.invoice_id,
    Description: line.description,
    Quantity: Math.round(Number(line.quantity) * 100),
    UnitPrice: Number(line.unit_price),
    Amount: Number(line.amount),
    AccountID: line.account_id,
    AccountCode: line.account_code ?? "",
    AccountName: line.account_name ?? ""
  }))
})

const formInvoiceData = (values: FormValues) => ({
  ID: values.id,
  ContactID: values.contactId,
  InvoiceDate: values.invoiceDate,
  DueDate: values.dueDate,
  TaxAmount: values.taxAmount,
  Notes: values.notes
})

const creditNoteData = (note: CreditNoteSummary) => ({
  ID: note.id,
  CNNumber: note.cn_number,
  CNDate: note.cn_date,
  Reason: note.reason,
  Status: note.status,
  Total: Number(note.total)
})

const companyData = (company: CompanyProfile) => ({
  Name: company.name,
  Tagline: company.tagline,
  Address: company.address,
  Phone: company.phone,
  Email: company.email,
  NPWP: company.npwp,
  BankName: company.bank_name,
  BankAccountNumber: company.bank_account_number,
  BankAccountHolder: company.bank_account_holder,
  InvoiceFooter: company.invoice_footer
})

const formOptions = Effect.gen(function*() {
  const contacts = yield* Contacts
  const accounts = yield* Accounts
  const profiles = yield* CompanyProfiles
  const [customers, revenue, assets, profile] = yield* Effect.all([
    contacts.list({ type: "customer", search: "" }).pipe(
      Effect.orElseSucceed(() => [])
    ),
    accounts.list({ type: "revenue", search: "" }).pipe(
      Effect.orElseSucceed(() => [])
    ),
    accounts.list({ type: "asset", search: "" }).pipe(
      Effect.orElseSucceed(() => [])
    ),
    profiles.get.pipe(Effect.option)
  ])
  return {
    customers: customers.filter((contact) => contact.is_active),
    revenue: revenue.filter((account) => account.is_active),
    assets: assets.filter((account) => account.is_active),
    profile
  }
})

const renderForm = (
  authenticated: CookieAuthentication,
  request: Parameters<typeof renderUiPage>[0],
  values: FormValues,
  errors: Readonly<Record<string, string>>,
  isEdit: boolean
) =>
  Effect.gen(function*() {
    const options = yield* formOptions
    const profile = options.profile._tag === "Some"
      ? options.profile.value
      : undefined
    return renderUiPage(
      request,
      "invoices/form",
      isEdit ? "Edit Invoice" : "New Invoice",
      {
        Invoice: formInvoiceData(values),
        Lines: values.lines.map((line) => ({
          Description: line.description,
          Quantity: line.quantity,
          UnitPrice: line.unitPrice,
          Amount: Math.round(line.quantity * line.unitPrice / 100),
          AccountID: line.accountId
        })),
        Contacts: options.customers.map((contact) => ({
          ID: contact.id,
          Name: contact.name,
          Price: contactPrice(contact)
        })),
        RevenueAccounts: options.revenue.map(accountData),
        AssetAccounts: options.assets.map(accountData),
        DefaultRevenueAccountID:
          profile?.default_revenue_account_id ?? 0,
        RecurringDescriptionTemplate:
          profile?.recurring_description_template ?? "",
        Errors: errors,
        IsEdit: isEdit
      },
      authenticated,
      ["invoices/line_partial"]
    )
  })

const pageNumber = (value: string | null) => {
  const parsed = Number.parseInt(value ?? "", 10)
  return Number.isNaN(parsed) || parsed < 1 ? 1 : parsed
}

const pageUrl = (
  page: number,
  status: string,
  search: string
) => {
  const values = new URLSearchParams()
  if (status !== "") {
    values.set("status", status)
  }
  if (search !== "") {
    values.set("search", search)
  }
  values.set("page", String(page))
  return `${dashboardBasePath}/invoices?${values.toString()}`
}

const notFound = () => uiPlainError(404, "404 page not found")
const internal = () => uiPlainError(500, "Internal Server Error")

const addListRoute = HttpRouter.get(
  `${dashboardBasePath}/invoices`,
  protectedUiHandler((authenticated, request) => {
    const query = new URL(request.url, "http://localhost").searchParams
    const status = query.get("status") ?? ""
    const search = query.get("search") ?? ""
    const requestedPage = pageNumber(query.get("page"))
    return Effect.gen(function*() {
      const invoices = yield* Invoices
      let result = yield* invoices.list({
        status,
        search,
        limit: pageSize,
        offset: (requestedPage - 1) * pageSize
      })
      const totalPages = Math.max(1, Math.ceil(result.total / pageSize))
      const page = Math.min(requestedPage, totalPages)
      if (page !== requestedPage) {
        result = yield* invoices.list({
          status,
          search,
          limit: pageSize,
          offset: (page - 1) * pageSize
        })
      }
      return renderUiPage(
        request,
        "invoices/index",
        "Invoices",
        {
          Invoices: result.invoices.map(invoiceData),
          Filter: status,
          Search: search,
          Pagination: {
            Page: page,
            PageSize: pageSize,
            Total: result.total,
            TotalPages: totalPages,
            HasPrev: page > 1,
            HasNext: page < totalPages,
            PrevURL: pageUrl(page - 1, status, search),
            NextURL: pageUrl(page + 1, status, search)
          }
        },
        authenticated
      )
    }).pipe(
      Effect.catchTag("InvoiceStoreError", () => Effect.succeed(internal()))
    )
  })
)

const addNewRoute = HttpRouter.get(
  `${dashboardBasePath}/invoices/new`,
  protectedUiHandler((authenticated, request) =>
    renderForm(authenticated, request, {
      id: 0,
      contactId: 0,
      invoiceDate: "",
      dueDate: "",
      taxAmount: 0,
      notes: "",
      lines: [{
        description: "",
        quantity: 100,
        unitPrice: 0,
        accountId: 0
      }]
    }, {}, false)
  )
)

const addCreateRoute = HttpRouter.post(
  `${dashboardBasePath}/invoices`,
  protectedUiHandler((authenticated, request, form) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    const values = valuesFromForm(form)
    const errors = validation(values)
    if (Object.keys(errors).length > 0) {
      return renderForm(authenticated, request, values, errors, false)
    }
    return Effect.gen(function*() {
      const invoices = yield* Invoices
      const created = yield* invoices.create({
        contactId: values.contactId,
        invoiceDate: values.invoiceDate,
        dueDate: values.dueDate,
        taxAmount: values.taxAmount,
        notes: values.notes,
        lines: domainLines(values.lines)
      }, authenticated.user.id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "invoice.create",
        actor: actor(authenticated),
        targetType: "invoice",
        targetId: created.id,
        targetLabel: created.invoice_number,
        metadata: {
          after: {
            contact_id: values.contactId,
            invoice_date: values.invoiceDate,
            due_date: values.dueDate,
            tax_amount: values.taxAmount,
            total: Number(created.total),
            line_count: values.lines.length
          }
        }
      })
      return uiRedirect(`${dashboardBasePath}/invoices/${created.id}`, {
        "set-cookie": uiFlashCookie("Invoice created successfully")
      })
    }).pipe(
      Effect.catchTag("InvoiceStoreError", (error) =>
        renderForm(
          authenticated,
          request,
          values,
          { general: String(error.cause) },
          false
        )
      )
    )
  })
)

const addViewRoute = HttpRouter.get(
  `${dashboardBasePath}/invoices/:id`,
  protectedUiHandler((authenticated, request) =>
    Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const invoices = yield* Invoices
      const accounts = yield* Accounts
      const [invoice, assets] = yield* Effect.all([
        invoices.getDetail(id),
        accounts.list({ type: "asset", search: "" }).pipe(
          Effect.orElseSucceed(() => [])
        )
      ])
      return renderUiPage(
        request,
        "invoices/view",
        `Invoice ${invoice.invoice_number}`,
        {
          Invoice: invoiceData(invoice),
          AssetAccounts: assets
            .filter((account) => account.is_active)
            .map(accountData),
          CreditNotes: (invoice.credit_notes ?? []).map(creditNoteData)
        },
        authenticated
      )
    }).pipe(
      Effect.catchTags({
        InvoiceNotFound: () => Effect.succeed(notFound()),
        InvoiceStoreError: () => Effect.succeed(notFound())
      })
    )
  )
)

const addEditRoute = HttpRouter.get(
  `${dashboardBasePath}/invoices/:id/edit`,
  protectedUiHandler((authenticated, request) =>
    Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const invoices = yield* Invoices
      const invoice = yield* invoices.get(id)
      if (invoice.status !== "draft") {
        return uiRedirect(`${dashboardBasePath}/invoices/${id}`, {
          "set-cookie": uiFlashCookie("Can only edit draft invoices")
        })
      }
      return yield* renderForm(
        authenticated,
        request,
        valuesFromInvoice(invoice),
        {},
        true
      )
    }).pipe(
      Effect.catchTags({
        InvoiceNotFound: () => Effect.succeed(notFound()),
        InvoiceStoreError: () => Effect.succeed(notFound())
      })
    )
  )
)

const addUpdateRoute = HttpRouter.post(
  `${dashboardBasePath}/invoices/:id`,
  protectedUiHandler((authenticated, request, form) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    return Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const invoices = yield* Invoices
      const existing = yield* invoices.get(id)
      const values = valuesFromForm(form, id)
      const errors = validation(values)
      if (Object.keys(errors).length > 0) {
        return yield* renderForm(
          authenticated,
          request,
          values,
          errors,
          true
        )
      }
      const result = yield* invoices.update(id, {
        contactId: values.contactId,
        invoiceDate: values.invoiceDate,
        dueDate: values.dueDate,
        taxAmount: values.taxAmount,
        notes: values.notes,
        lines: domainLines(values.lines)
      })
      const metadata = auditDiff(
        {
          contact_id: existing.contact_id,
          invoice_date: existing.invoice_date,
          due_date: existing.due_date,
          tax_amount: Number(existing.tax_amount),
          notes: existing.notes,
          total: Number(existing.total)
        },
        {
          contact_id: values.contactId,
          invoice_date: values.invoiceDate,
          due_date: values.dueDate,
          tax_amount: values.taxAmount,
          notes: values.notes,
          total: Number(result.after.total)
        },
        [
          "contact_id",
          "invoice_date",
          "due_date",
          "tax_amount",
          "notes",
          "total"
        ]
      )
      if (metadata !== undefined) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "invoice.update",
          actor: actor(authenticated),
          targetType: "invoice",
          targetId: id,
          targetLabel: existing.invoice_number,
          metadata
        })
      }
      return uiRedirect(`${dashboardBasePath}/invoices/${id}`, {
        "set-cookie": uiFlashCookie("Invoice updated successfully")
      })
    }).pipe(
      Effect.catchTags({
        InvoiceNotFound: () => Effect.succeed(notFound()),
        InvoiceConflict: (error) =>
          renderForm(
            authenticated,
            request,
            valuesFromForm(form, 0),
            { general: error.message },
            true
          ),
        InvoiceStoreError: (error) =>
          renderForm(
            authenticated,
            request,
            valuesFromForm(form, 0),
            { general: String(error.cause) },
            true
          )
      })
    )
  })
)

const addSendRoute = HttpRouter.post(
  `${dashboardBasePath}/invoices/:id/send`,
  protectedUiHandler((authenticated, request) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    return Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const invoices = yield* Invoices
      const sent = yield* invoices.send(id, authenticated.user.id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "invoice.send",
        actor: actor(authenticated),
        targetType: "invoice",
        targetId: id,
        targetLabel: sent.invoice_number,
        metadata: {
          after: { status: sent.status },
          journal_id: sent.journal_id
        }
      })
      return uiRedirect(`${dashboardBasePath}/invoices/${id}`, {
        "set-cookie": uiFlashCookie(
          "Invoice sent — journal entry created"
        )
      })
    }).pipe(
      Effect.catchAll((error) =>
        Effect.gen(function*() {
          const id = parseId((yield* HttpRouter.params).id) ?? 0
          const message = "message" in Object(error)
            ? String((error as { readonly message: unknown }).message)
            : String(error)
          return uiRedirect(`${dashboardBasePath}/invoices/${id}`, {
            "set-cookie": uiFlashCookie(`Error: ${message}`)
          })
        })
      )
    )
  })
)

const addPaymentRoute = HttpRouter.post(
  `${dashboardBasePath}/invoices/:id/payment`,
  protectedUiHandler((authenticated, request, form) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    return Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const amount = parseIdr(form.get("amount"))
      const paymentDate = form.get("payment_date") ?? ""
      const paymentAccount = parseIntOrZero(form.get("payment_account"))
      if (amount <= 0 || paymentDate === "" || paymentAccount === 0) {
        return uiRedirect(`${dashboardBasePath}/invoices/${id}`, {
          "set-cookie": uiFlashCookie(
            "Error: all payment fields are required"
          )
        })
      }
      const invoices = yield* Invoices
      const updated = yield* invoices.recordPayment(
        id,
        amount,
        paymentDate,
        paymentAccount,
        authenticated.user.id
      )
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "invoice.payment",
        actor: actor(authenticated),
        targetType: "invoice",
        targetId: id,
        targetLabel: updated.invoice_number,
        metadata: {
          amount,
          payment_date: paymentDate,
          payment_account_id: paymentAccount,
          status_after: updated.status
        }
      })
      return uiRedirect(`${dashboardBasePath}/invoices/${id}`, {
        "set-cookie": uiFlashCookie("Payment recorded successfully")
      })
    }).pipe(
      Effect.catchAll((error) =>
        Effect.gen(function*() {
          const id = parseId((yield* HttpRouter.params).id) ?? 0
          const message = "message" in Object(error)
            ? String((error as { readonly message: unknown }).message)
            : String(error)
          return uiRedirect(`${dashboardBasePath}/invoices/${id}`, {
            "set-cookie": uiFlashCookie(`Error: ${message}`)
          })
        })
      )
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  `${dashboardBasePath}/invoices/:id`,
  protectedUiHandler((authenticated, request) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    return Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const invoices = yield* Invoices
      const removed = yield* invoices.remove(id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "invoice.delete",
        actor: actor(authenticated),
        targetType: "invoice",
        targetId: id,
        targetLabel: removed.invoice_number,
        metadata: {
          before: {
            contact_id: removed.contact_id,
            invoice_date: removed.invoice_date,
            status: removed.status,
            total: Number(removed.total)
          }
        }
      })
      if (request.headers["hx-request"] === "true") {
        return HttpServerResponse.empty({
          status: 200,
          headers: { "hx-redirect": "/invoices" }
        })
      }
      return uiRedirect(`${dashboardBasePath}/invoices`, {
        "set-cookie": uiFlashCookie("Invoice deleted")
      })
    }).pipe(
      Effect.catchAll((error) =>
        Effect.gen(function*() {
          const id = parseId((yield* HttpRouter.params).id) ?? 0
          const message = "message" in Object(error)
            ? String((error as { readonly message: unknown }).message)
            : String(error)
          return uiRedirect(`${dashboardBasePath}/invoices/${id}`, {
            "set-cookie": uiFlashCookie(`Error: ${message}`)
          })
        })
      )
    )
  })
)

const selectedIds = (form: URLSearchParams) =>
  form.getAll("ids")
    .map((value) => parseId(value))
    .filter((value): value is number => value !== undefined)

const addBulkDeleteRoute = HttpRouter.post(
  `${dashboardBasePath}/invoices/bulk-delete`,
  protectedUiHandler((authenticated, request, form) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    const ids = selectedIds(form)
    if (ids.length === 0) {
      return Effect.succeed(uiRedirect(`${dashboardBasePath}/invoices`, {
        "set-cookie": uiFlashCookie("No invoices selected")
      }))
    }
    return Effect.gen(function*() {
      const invoices = yield* Invoices
      const result = yield* invoices.bulkDelete(ids)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "invoice.bulk_delete",
        actor: actor(authenticated),
        targetType: "invoice",
        metadata: {
          deleted: result.deleted,
          skipped: result.skipped.length
        }
      })
      let message = `Deleted ${result.deleted.length} draft invoice(s).`
      if (result.skipped.length > 0) {
        message += ` Skipped ${result.skipped.length} (not draft).`
      }
      return uiRedirect(`${dashboardBasePath}/invoices`, {
        "set-cookie": uiFlashCookie(message)
      })
    }).pipe(
      Effect.catchAll((error) =>
        Effect.succeed(uiRedirect(`${dashboardBasePath}/invoices`, {
          "set-cookie": uiFlashCookie(
            `Error deleting invoices: ${String(error)}`
          )
        }))
      )
    )
  })
)

const addBulkSendRoute = HttpRouter.post(
  `${dashboardBasePath}/invoices/bulk-send`,
  protectedUiHandler((authenticated, request, form) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    const ids = selectedIds(form)
    if (ids.length === 0) {
      return Effect.succeed(uiRedirect(`${dashboardBasePath}/invoices`, {
        "set-cookie": uiFlashCookie("No invoices selected")
      }))
    }
    return Effect.gen(function*() {
      const invoices = yield* Invoices
      const result = yield* invoices.bulkSend(ids, authenticated.user.id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "invoice.bulk_send",
        actor: actor(authenticated),
        targetType: "invoice",
        metadata: {
          sent: result.sent,
          skipped: result.skipped,
          failed: result.failed
        }
      })
      let message =
        `Marked ${result.sent.length} invoice(s) as sent ` +
        "(journal entries posted)."
      if (result.skipped.length > 0) {
        message += ` Skipped ${result.skipped.length} (not draft).`
      }
      if (result.failed.length > 0) {
        message += ` Failed ${result.failed.length}.`
      }
      return uiRedirect(`${dashboardBasePath}/invoices`, {
        "set-cookie": uiFlashCookie(message)
      })
    }).pipe(
      Effect.catchAll((error) =>
        Effect.succeed(uiRedirect(`${dashboardBasePath}/invoices`, {
          "set-cookie": uiFlashCookie(
            `Error sending invoices: ${String(error)}`
          )
        }))
      )
    )
  })
)

const localDate = (date: Date) => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, "0")
  const day = String(date.getDate()).padStart(2, "0")
  return `${year}-${month}-${day}`
}

const addGenerateRecurringRoute = HttpRouter.post(
  `${dashboardBasePath}/invoices/generate-recurring`,
  protectedUiHandler((authenticated, request) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    const now = new Date()
    const due = new Date(now)
    due.setDate(due.getDate() + 10)
    const invoiceDate = localDate(now)
    const dueDate = localDate(due)
    return Effect.gen(function*() {
      const invoices = yield* Invoices
      const result = yield* invoices.generateRecurring(
        invoiceDate,
        dueDate,
        authenticated.user.id
      )
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "invoice.generate_recurring",
        actor: actor(authenticated),
        targetType: "invoice",
        metadata: {
          invoice_date: invoiceDate,
          due_date: dueDate,
          effective_days: result.effective_days,
          multiplier_percent: result.multiplier_percent,
          created: result.created,
          skipped: result.skipped,
          failed: result.failed,
          created_invoices: result.items
            .filter((item) => item.invoice_number !== undefined)
            .map((item) => item.invoice_number)
        }
      })
      let message =
        `Generated ${result.created} draft invoice(s). ` +
        `Skipped ${result.skipped} customer(s).`
      if (result.failed > 0) {
        message += ` Failed ${result.failed}.`
      }
      return uiRedirect(`${dashboardBasePath}/invoices`, {
        "set-cookie": uiFlashCookie(message)
      })
    }).pipe(
      Effect.catchAll((error) =>
        Effect.succeed(uiRedirect(`${dashboardBasePath}/invoices`, {
          "set-cookie": uiFlashCookie(
            `Error generating invoices: ${
              "message" in Object(error)
                ? String((error as { readonly message: unknown }).message)
                : String(error)
            }`
          )
        }))
      )
    )
  })
)

const addPrintRoute = HttpRouter.get(
  `${dashboardBasePath}/invoices/:id/print`,
  protectedUiHandler((_authenticated, request) =>
    Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const invoices = yield* Invoices
      const profiles = yield* CompanyProfiles
      const [invoice, company] = yield* Effect.all([
        invoices.get(id),
        profiles.get
      ])
      const html = pageTemplate("invoices/print").render("print.html", {
        Title: `Invoice ${invoice.invoice_number}`,
        BasePath: dashboardBasePath,
        Data: {
          Invoice: invoiceData(invoice),
          Company: companyData(company)
        }
      })
      return HttpServerResponse.text(html, {
        headers: { "content-type": "text/html; charset=utf-8" }
      })
    }).pipe(
      Effect.catchTags({
        InvoiceNotFound: () => Effect.succeed(notFound()),
        InvoiceStoreError: () => Effect.succeed(notFound()),
        CompanyProfileStoreError: () => Effect.succeed(internal())
      })
    )
  )
)

const addPdfRoute = HttpRouter.get(
  `${dashboardBasePath}/invoices/:id/pdf`,
  protectedUiHandler(() =>
    Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const invoices = yield* Invoices
      const profiles = yield* CompanyProfiles
      const [invoice, company] = yield* Effect.all([
        invoices.get(id),
        profiles.get
      ])
      const body = renderInvoicePdf(invoice, company)
      return HttpServerResponse.uint8Array(body, {
        headers: {
          "content-type": "application/pdf",
          "content-disposition":
            `inline; filename="${invoice.invoice_number}.pdf"`,
          "content-length": String(body.byteLength)
        }
      })
    }).pipe(
      Effect.catchTags({
        InvoiceNotFound: () => Effect.succeed(notFound()),
        InvoiceStoreError: () => Effect.succeed(notFound()),
        CompanyProfileStoreError: () => Effect.succeed(internal())
      })
    )
  )
)

const normalizePhone = (phone: string) => {
  const digits = [...phone]
    .filter((character) => character >= "0" && character <= "9")
    .join("")
  return digits.startsWith("62")
    ? digits
    : digits.startsWith("0")
    ? `62${digits.slice(1)}`
    : digits
}

const randomPortalCode = (name: string) => {
  const first = name.trim().split(" ")[0] ?? ""
  const prefix = [...first.toLowerCase()]
    .filter((character) => character >= "a" && character <= "z")
    .join("")
    .slice(0, 12) || "lts"
  const values = new Uint32Array(1)
  crypto.getRandomValues(values)
  return `${prefix}-${String((values[0] ?? 0) % 1000).padStart(3, "0")}`
}

const portalCode = (contactId: number, name: string) =>
  Effect.gen(function*() {
    const sql = yield* SqlClient.SqlClient
    const rows = yield* sql<{ readonly portal_code: string }>`
      SELECT COALESCE(portal_code, '') AS portal_code
      FROM contacts
      WHERE id = ${contactId}
    `
    if ((rows[0]?.portal_code ?? "") !== "") {
      return rows[0]?.portal_code ?? ""
    }
    for (let attempt = 0; attempt < 10; attempt += 1) {
      const code = randomPortalCode(name)
      const result = yield* sql`
        UPDATE contacts
        SET portal_code = ${code}
        WHERE id = ${contactId}
      `.pipe(Effect.either)
      if (result._tag === "Right") {
        return code
      }
    }
    return yield* Effect.fail(new Error("could not generate portal code"))
  })

const addWhatsAppRoute = (development: boolean) =>
  HttpRouter.get(
    `${dashboardBasePath}/invoices/:id/whatsapp`,
    protectedUiHandler((authenticated, request) => {
      if (!hasManage(authenticated)) {
        return Effect.succeed(uiPlainError(403, "Forbidden"))
      }
      return Effect.gen(function*() {
        const id = parseId((yield* HttpRouter.params).id)
        if (id === undefined) {
          return notFound()
        }
        const invoices = yield* Invoices
        const invoice = yield* invoices.get(id)
        if (invoice.status === "draft") {
          return uiRedirect(`${dashboardBasePath}/invoices/${id}`, {
            "set-cookie": uiFlashCookie(
              "Kirim invoice ini dulu (Mark as Sent) sebelum " +
              "membagikan link ke orang tua."
            )
          })
        }
        const contacts = yield* Contacts
        const contact = yield* contacts.get(invoice.contact_id)
        if (contact.phone === "") {
          return uiRedirect(`${dashboardBasePath}/invoices/${id}`, {
            "set-cookie": uiFlashCookie(
              "Nomor telepon kontak belum diisi."
            )
          })
        }
        const code = yield* portalCode(contact.id, contact.name)
        const url = new URL(request.url, "http://localhost")
        const origin = `${development ? "http" : "https"}://${url.host}`
        const portalUrl = `${origin}/p/${code}`
        const message =
          "Assalamualaikum Wr. Wb., kami dari Antar Jemput Latasya. " +
          `Berikut link invoice Ananda ${contact.name} ` +
          `(${invoice.invoice_number}):\n${portalUrl}\n\n` +
          "Link berisi daftar invoice dan akan terus aktif sesuai masa " +
          "keikutsertaan antar jemput, Terima kasih"
        const query = new URLSearchParams({ text: message })
        return HttpServerResponse.empty({
          status: 302,
          headers: {
            location:
              `https://wa.me/${normalizePhone(contact.phone)}?${query}`
          }
        })
      }).pipe(Effect.catchAll((error) =>
        Effect.succeed(
          "_tag" in Object(error) &&
              ((error as { readonly _tag?: string })._tag ===
                "InvoiceNotFound" ||
                (error as { readonly _tag?: string })._tag ===
                  "InvoiceStoreError")
            ? notFound()
            : internal()
        )
      ))
    })
  )

export const addUiInvoiceRoutes = (development: boolean) =>
  <E, R>(router: HttpRouter.HttpRouter<E, R>) =>
    router.pipe(
      addListRoute,
      addNewRoute,
      addCreateRoute,
      addGenerateRecurringRoute,
      addBulkDeleteRoute,
      addBulkSendRoute,
      addViewRoute,
      addEditRoute,
      addUpdateRoute,
      addDeleteRoute,
      addSendRoute,
      addPaymentRoute,
      addPrintRoute,
      addPdfRoute,
      addWhatsAppRoute(development)
    )
