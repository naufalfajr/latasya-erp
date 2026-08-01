import { HttpRouter, HttpServerResponse } from "@effect/platform"
import { Effect } from "effect"
import { Accounts, type Account } from "../../domain/accounting/accounts.ts"
import {
  type Bill,
  type BillLineValues,
  Bills
} from "../../domain/accounting/bills.ts"
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
  readonly billDate: string
  readonly dueDate: string
  readonly taxAmount: number
  readonly notes: string
  readonly lines: ReadonlyArray<BillLineValues>
}

const size = 50
const manage = (auth: CookieAuthentication) =>
  auth.user.role === "admin" ||
  auth.effectiveCapabilities.includes("bills.manage")
const actor = (auth: CookieAuthentication) => ({
  id: auth.user.id,
  username: auth.user.username
})
const id = (value: string | undefined) => {
  const parsed = Number(value)
  return value !== undefined && /^[+-]?\d+$/.test(value) &&
      Number.isSafeInteger(parsed)
    ? parsed
    : undefined
}
const integer = (value: string | null) => {
  const parsed = Number.parseInt(value ?? "", 10)
  return Number.isNaN(parsed) ? 0 : parsed
}
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
const lines = (form: URLSearchParams) => {
  const descriptions = form.getAll("line_description")
  const quantities = form.getAll("line_quantity")
  const prices = form.getAll("line_unit_price")
  const accounts = form.getAll("line_account_id")
  const result: Array<BillLineValues> = []
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
const fromForm = (form: URLSearchParams, billId = 0): Values => ({
  id: billId,
  contactId: integer(form.get("contact_id")),
  billDate: form.get("bill_date") ?? "",
  dueDate: form.get("due_date") ?? "",
  taxAmount: idr(form.get("tax_amount")),
  notes: form.get("notes") ?? "",
  lines: lines(form)
})
const validate = (values: Values) => {
  const errors: Record<string, string> = {}
  if (values.contactId === 0) errors.contact_id = "Supplier is required"
  if (values.billDate === "") errors.bill_date = "Bill date is required"
  if (values.dueDate === "") errors.due_date = "Due date is required"
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
const fromBill = (bill: Bill): Values => ({
  id: bill.id,
  contactId: bill.contact_id,
  billDate: bill.bill_date,
  dueDate: bill.due_date,
  taxAmount: bill.tax_amount,
  notes: bill.notes,
  lines: (bill.lines ?? []).map((line) => ({
    description: line.description,
    quantity: line.quantity,
    unitPrice: line.unit_price,
    accountId: line.account_id
  }))
})
const accountData = (account: Account) => ({
  ID: account.id,
  Code: account.code,
  Name: account.name
})
const billData = (bill: Bill) => ({
  ID: bill.id,
  BillNumber: bill.bill_number,
  ContactID: bill.contact_id,
  ContactName: bill.contact_name ?? "",
  BillDate: bill.bill_date,
  DueDate: bill.due_date,
  Status: bill.status,
  Subtotal: bill.subtotal,
  TaxAmount: bill.tax_amount,
  Total: bill.total,
  AmountPaid: bill.amount_paid,
  AmountDue: bill.total - bill.amount_paid,
  Notes: bill.notes,
  JournalID: bill.journal_id ?? 0,
  Lines: (bill.lines ?? []).map((line) => ({
    Description: line.description,
    Quantity: line.quantity,
    UnitPrice: line.unit_price,
    Amount: line.amount,
    AccountID: line.account_id,
    AccountCode: line.account_code ?? "",
    AccountName: line.account_name ?? ""
  }))
})
const formData = (values: Values) => ({
  ID: values.id,
  ContactID: values.contactId,
  BillDate: values.billDate,
  DueDate: values.dueDate,
  TaxAmount: values.taxAmount,
  Notes: values.notes
})
const options = Effect.gen(function*() {
  const contacts = yield* Contacts
  const accounts = yield* Accounts
  const [suppliers, expenses, assets] = yield* Effect.all([
    contacts.list({ type: "supplier", search: "" }).pipe(
      Effect.orElseSucceed(() => [])
    ),
    accounts.list({ type: "expense", search: "" }).pipe(
      Effect.orElseSucceed(() => [])
    ),
    accounts.list({ type: "asset", search: "" }).pipe(
      Effect.orElseSucceed(() => [])
    )
  ])
  return {
    suppliers: suppliers.filter((item) => item.is_active),
    expenses: expenses.filter((item) => item.is_active),
    assets: assets.filter((item) => item.is_active)
  }
})
const renderForm = (
  auth: CookieAuthentication,
  request: Parameters<typeof renderUiPage>[0],
  values: Values,
  errors: Readonly<Record<string, string>>,
  isEdit: boolean
) => Effect.gen(function*() {
  const data = yield* options
  return renderUiPage(request, "bills/form", isEdit ? "Edit Bill" : "New Bill", {
    Bill: formData(values),
    Lines: values.lines.map((line) => ({
      Description: line.description,
      Quantity: line.quantity,
      UnitPrice: line.unitPrice,
      Amount: Math.round(line.quantity * line.unitPrice / 100),
      AccountID: line.accountId
    })),
    Contacts: data.suppliers.map((contact) => ({
      ID: contact.id,
      Name: contact.name
    })),
    ExpenseAccounts: data.expenses.map(accountData),
    AssetAccounts: data.assets.map(accountData),
    Errors: errors,
    IsEdit: isEdit
  }, auth, ["bills/line_partial"])
})
const notFound = () => uiPlainError(404, "404 page not found")
const internal = () => uiPlainError(500, "Internal Server Error")
const pageUrl = (page: number, status: string, search: string) => {
  const query = new URLSearchParams()
  if (status) query.set("status", status)
  if (search) query.set("search", search)
  query.set("page", String(page))
  return `${dashboardBasePath}/bills?${query}`
}
const errorMessage = (error: unknown) =>
  typeof error === "object" && error !== null && "message" in error
    ? String(error.message)
    : String(error)

const listRoute = HttpRouter.get(`${dashboardBasePath}/bills`,
  protectedUiHandler((auth, request) => {
    const query = new URL(request.url, "http://localhost").searchParams
    const status = query.get("status") ?? ""
    const search = query.get("search") ?? ""
    const requested = Math.max(1, integer(query.get("page")))
    return Effect.gen(function*() {
      const bills = yield* Bills
      let result = yield* bills.list({
        status,
        search,
        limit: size,
        offset: (requested - 1) * size
      })
      const totalPages = Math.max(1, Math.ceil(result.total / size))
      const page = Math.min(requested, totalPages)
      if (page !== requested) {
        result = yield* bills.list({
          status,
          search,
          limit: size,
          offset: (page - 1) * size
        })
      }
      return renderUiPage(request, "bills/index", "Bills", {
        Bills: result.bills.map(billData),
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

const newRoute = HttpRouter.get(`${dashboardBasePath}/bills/new`,
  protectedUiHandler((auth, request) => renderForm(auth, request, {
    id: 0,
    contactId: 0,
    billDate: "",
    dueDate: "",
    taxAmount: 0,
    notes: "",
    lines: [
      { description: "", quantity: 100, unitPrice: 0, accountId: 0 },
      { description: "", quantity: 100, unitPrice: 0, accountId: 0 }
    ]
  }, {}, false)))

const createRoute = HttpRouter.post(`${dashboardBasePath}/bills`,
  protectedUiHandler((auth, request, form) => {
    if (!manage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    const values = fromForm(form)
    const errors = validate(values)
    if (Object.keys(errors).length) {
      return renderForm(auth, request, values, errors, false)
    }
    return Effect.gen(function*() {
      const bills = yield* Bills
      const created = yield* bills.create({
        contactId: values.contactId,
        billDate: values.billDate,
        dueDate: values.dueDate,
        taxAmount: values.taxAmount,
        notes: values.notes,
        lines: values.lines
      }, auth.user.id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "bill.create",
        actor: actor(auth),
        targetType: "bill",
        targetId: created.id,
        targetLabel: created.bill_number,
        metadata: { after: {
          contact_id: values.contactId,
          bill_date: values.billDate,
          due_date: values.dueDate,
          tax_amount: values.taxAmount,
          total: created.total,
          line_count: values.lines.length
        } }
      })
      return uiRedirect(`${dashboardBasePath}/bills/${created.id}`, {
        "set-cookie": uiFlashCookie("Bill created successfully")
      })
    }).pipe(Effect.catchAll((error) =>
      renderForm(auth, request, values, { general: errorMessage(error) }, false)
    ))
  }))

const viewRoute = HttpRouter.get(`${dashboardBasePath}/bills/:id`,
  protectedUiHandler((auth, request) => Effect.gen(function*() {
    const billId = id((yield* HttpRouter.params).id)
    if (billId === undefined) return notFound()
    const bills = yield* Bills
    const accounts = yield* Accounts
    const [bill, assets] = yield* Effect.all([
      bills.get(billId),
      accounts.list({ type: "asset", search: "" }).pipe(
        Effect.orElseSucceed(() => [])
      )
    ])
    return renderUiPage(request, "bills/view", `Bill ${bill.bill_number}`, {
      Bill: billData(bill),
      AssetAccounts: assets.filter((item) => item.is_active).map(accountData)
    }, auth)
  }).pipe(Effect.catchAll(() => Effect.succeed(notFound())))))

const editRoute = HttpRouter.get(`${dashboardBasePath}/bills/:id/edit`,
  protectedUiHandler((auth, request) => Effect.gen(function*() {
    const billId = id((yield* HttpRouter.params).id)
    if (billId === undefined) return notFound()
    const bills = yield* Bills
    const bill = yield* bills.get(billId)
    if (bill.status !== "draft") {
      return uiRedirect(`${dashboardBasePath}/bills/${billId}`, {
        "set-cookie": uiFlashCookie("Can only edit draft bills")
      })
    }
    return yield* renderForm(auth, request, fromBill(bill), {}, true)
  }).pipe(Effect.catchAll(() => Effect.succeed(notFound())))))

const updateRoute = HttpRouter.post(`${dashboardBasePath}/bills/:id`,
  protectedUiHandler((auth, request, form) => {
    if (!manage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const billId = id((yield* HttpRouter.params).id)
      if (billId === undefined) return notFound()
      const values = fromForm(form, billId)
      const errors = validate(values)
      if (Object.keys(errors).length) {
        return yield* renderForm(auth, request, values, errors, true)
      }
      const bills = yield* Bills
      const result = yield* bills.update(billId, {
        contactId: values.contactId,
        billDate: values.billDate,
        dueDate: values.dueDate,
        taxAmount: values.taxAmount,
        notes: values.notes,
        lines: values.lines
      })
      const metadata = auditDiff(
        {
          contact_id: result.before.contact_id,
          bill_date: result.before.bill_date,
          due_date: result.before.due_date,
          tax_amount: result.before.tax_amount,
          notes: result.before.notes,
          total: result.before.total
        },
        {
          contact_id: values.contactId,
          bill_date: values.billDate,
          due_date: values.dueDate,
          tax_amount: values.taxAmount,
          notes: values.notes,
          total: result.after.total
        },
        ["contact_id", "bill_date", "due_date", "tax_amount", "notes", "total"]
      )
      if (metadata) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "bill.update",
          actor: actor(auth),
          targetType: "bill",
          targetId: billId,
          targetLabel: result.before.bill_number,
          metadata
        })
      }
      return uiRedirect(`${dashboardBasePath}/bills/${billId}`, {
        "set-cookie": uiFlashCookie("Bill updated successfully")
      })
    }).pipe(Effect.catchAll((error) =>
      Effect.gen(function*() {
        const billId = id((yield* HttpRouter.params).id) ?? 0
        return yield* renderForm(
          auth,
          request,
          fromForm(form, billId),
          { general: errorMessage(error) },
          true
        )
      })
    ))
  }))

const actionRoute = (
  action: "receive" | "payment",
  flash: string
) => HttpRouter.post(`${dashboardBasePath}/bills/:id/${action}`,
  protectedUiHandler((auth, request, form) => {
    if (!manage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const billId = id((yield* HttpRouter.params).id)
      if (billId === undefined) return notFound()
      const bills = yield* Bills
      let updated: Bill
      let metadata: Record<string, unknown>
      if (action === "receive") {
        updated = yield* bills.receive(billId, auth.user.id)
        metadata = {
          after: { status: updated.status },
          journal_id: updated.journal_id
        }
      } else {
        const amount = idr(form.get("amount"))
        const paymentDate = form.get("payment_date") ?? ""
        const paymentAccount = integer(form.get("payment_account"))
        if (amount <= 0 || paymentDate === "" || paymentAccount === 0) {
          return uiRedirect(`${dashboardBasePath}/bills/${billId}`, {
            "set-cookie": uiFlashCookie(
              "Error: all payment fields are required"
            )
          })
        }
        updated = yield* bills.recordPayment(
          billId,
          amount,
          paymentDate,
          paymentAccount,
          auth.user.id
        )
        metadata = {
          amount,
          payment_date: paymentDate,
          payment_account_id: paymentAccount,
          status_after: updated.status
        }
      }
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: `bill.${action}`,
        actor: actor(auth),
        targetType: "bill",
        targetId: billId,
        targetLabel: updated.bill_number,
        metadata
      })
      return uiRedirect(`${dashboardBasePath}/bills/${billId}`, {
        "set-cookie": uiFlashCookie(flash)
      })
    }).pipe(Effect.catchAll((error) =>
      Effect.gen(function*() {
        const billId = id((yield* HttpRouter.params).id) ?? 0
        return uiRedirect(`${dashboardBasePath}/bills/${billId}`, {
          "set-cookie": uiFlashCookie(`Error: ${errorMessage(error)}`)
        })
      })
    ))
  }))

const deleteRoute = HttpRouter.del(`${dashboardBasePath}/bills/:id`,
  protectedUiHandler((auth, request) => {
    if (!manage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const billId = id((yield* HttpRouter.params).id)
      if (billId === undefined) return notFound()
      const bills = yield* Bills
      const removed = yield* bills.remove(billId)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "bill.delete",
        actor: actor(auth),
        targetType: "bill",
        targetId: billId,
        targetLabel: removed.bill_number,
        metadata: { before: {
          contact_id: removed.contact_id,
          bill_date: removed.bill_date,
          status: removed.status,
          total: removed.total
        } }
      })
      return request.headers["hx-request"] === "true"
        ? HttpServerResponse.empty({ status: 200 })
        : uiRedirect(`${dashboardBasePath}/bills`, {
          "set-cookie": uiFlashCookie("Bill deleted")
        })
    }).pipe(Effect.catchAll((error) =>
      Effect.gen(function*() {
        const billId = id((yield* HttpRouter.params).id) ?? 0
        return uiRedirect(`${dashboardBasePath}/bills/${billId}`, {
          "set-cookie": uiFlashCookie(`Error: ${errorMessage(error)}`)
        })
      })
    ))
  }))

export const addUiBillRoutes = <E, R>(router: HttpRouter.HttpRouter<E, R>) =>
  router.pipe(
    listRoute,
    newRoute,
    createRoute,
    viewRoute,
    editRoute,
    updateRoute,
    deleteRoute,
    actionRoute("receive", "Bill received — journal entry created"),
    actionRoute("payment", "Payment recorded successfully")
  )
