import { HttpRouter, HttpServerResponse } from "@effect/platform"
import { SqlClient } from "@effect/sql"
import { Effect } from "effect"
import {
  Accounts,
  type Account
} from "../../domain/accounting/accounts.ts"
import {
  type CookieAuthentication
} from "../../domain/auth/authentication.ts"
import {
  type JournalEntry,
  type JournalLineValues,
  Journals
} from "../../domain/accounting/journals.ts"
import { Audit, auditDiff } from "../../domain/audit/audit.ts"
import {
  dashboardBasePath,
  protectedUiHandler,
  renderUiPage,
  uiFlashCookie,
  uiPlainError,
  uiRedirect
} from "./ui-auth.ts"
import { requestMetadata } from "./request-metadata.ts"

type TransactionKind = "income" | "expense"

type TransactionValues = {
  readonly entryDate: string
  readonly description: string
  readonly amount: number
  readonly primaryAccount: number
  readonly cashAccount: number
  readonly vehicleId: number
}

const pageSize = 50

const kindConfig = {
  income: {
    capability: "income.manage",
    primaryType: "revenue",
    pageTitle: "Income",
    newTitle: "Record Income",
    editTitle: "Edit Income",
    createFlash: "Income recorded successfully",
    updateFlash: "Income updated successfully",
    deleteFlash: "Income deleted successfully"
  },
  expense: {
    capability: "expenses.manage",
    primaryType: "expense",
    pageTitle: "Expenses",
    newTitle: "Record Expense",
    editTitle: "Edit Expense",
    createFlash: "Expense recorded successfully",
    updateFlash: "Expense updated successfully",
    deleteFlash: "Expense deleted successfully"
  }
} as const

const hasManage = (
  authenticated: CookieAuthentication,
  kind: TransactionKind
) =>
  authenticated.user.role === "admin" ||
  authenticated.effectiveCapabilities.includes(
    kindConfig[kind].capability
  )

const parseId = (value: string | undefined) =>
  value !== undefined && /^[+-]?\d+$/.test(value)
    ? Number(value)
    : undefined

const parseOptionalId = (value: string | null) => {
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

const accountData = (account: Account) => ({
  ID: account.id,
  Code: account.code,
  Name: account.name
})

const valuesFromForm = (
  form: URLSearchParams,
  kind: TransactionKind
): TransactionValues => ({
  entryDate: form.get("entry_date") ?? "",
  description: form.get("description") ?? "",
  amount: parseIdr(form.get("amount")),
  primaryAccount: parseOptionalId(
    form.get(kind === "income" ? "revenue_account" : "expense_account")
  ),
  cashAccount: parseOptionalId(
    form.get(kind === "income" ? "deposit_account" : "payment_account")
  ),
  vehicleId: kind === "expense"
    ? parseOptionalId(form.get("vehicle_id"))
    : 0
})

const validation = (values: TransactionValues, kind: TransactionKind) => {
  const errors: Record<string, string> = {}
  if (values.entryDate === "") {
    errors.entry_date = "Date is required"
  }
  if (values.description === "") {
    errors.description = "Description is required"
  }
  if (values.amount <= 0) {
    errors.amount = "Amount must be greater than 0"
  }
  if (values.primaryAccount === 0) {
    errors[kind === "income" ? "revenue_account" : "expense_account"] =
      kind === "income"
        ? "Revenue account is required"
        : "Expense account is required"
  }
  if (values.cashAccount === 0) {
    errors[kind === "income" ? "deposit_account" : "payment_account"] =
      kind === "income"
        ? "Deposit account is required"
        : "Payment account is required"
  }
  return errors
}

const linesFor = (
  values: TransactionValues,
  kind: TransactionKind
): ReadonlyArray<JournalLineValues> =>
  kind === "income"
    ? [
      {
        accountId: values.cashAccount,
        debit: values.amount,
        credit: 0,
        memo: ""
      },
      {
        accountId: values.primaryAccount,
        debit: 0,
        credit: values.amount,
        memo: ""
      }
    ]
    : [
      {
        accountId: values.primaryAccount,
        debit: values.amount,
        credit: 0,
        memo: ""
      },
      {
        accountId: values.cashAccount,
        debit: 0,
        credit: values.amount,
        memo: ""
      }
    ]

const shapeFromEntry = (
  entry: JournalEntry,
  kind: TransactionKind
): TransactionValues => {
  let primaryAccount = 0
  let cashAccount = 0
  let amount = 0
  for (const line of entry.lines ?? []) {
    if (kind === "income") {
      if (Number(line.debit) > 0) {
        cashAccount = line.account_id
        amount = Number(line.debit)
      }
      if (Number(line.credit) > 0) {
        primaryAccount = line.account_id
      }
    } else {
      if (Number(line.debit) > 0) {
        primaryAccount = line.account_id
        amount = Number(line.debit)
      }
      if (Number(line.credit) > 0) {
        cashAccount = line.account_id
      }
    }
  }
  return {
    entryDate: entry.entry_date,
    description: entry.description,
    amount,
    primaryAccount,
    cashAccount,
    vehicleId: entry.vehicle_id ?? 0
  }
}

const formOptions = (kind: TransactionKind) =>
  Effect.gen(function*() {
    const accounts = yield* Accounts
    const [primary, cash] = yield* Effect.all([
      accounts.list({
        type: kindConfig[kind].primaryType,
        search: ""
      }).pipe(Effect.orElseSucceed(() => [])),
      accounts.list({ type: "asset", search: "" }).pipe(
        Effect.orElseSucceed(() => [])
      )
    ])
    let vehicles: ReadonlyArray<{
      readonly id: number
      readonly code: string
    }> = []
    if (kind === "expense") {
      const sql = yield* SqlClient.SqlClient
      vehicles = yield* sql<{
        readonly id: number
        readonly code: string
      }>`
        SELECT id, code
        FROM vehicles
        WHERE is_active = 1
        ORDER BY code
      `.pipe(Effect.orElseSucceed(() => []))
    }
    return {
      primary: primary.filter((account) => account.is_active),
      cash: cash.filter((account) => account.is_active),
      vehicles
    }
  })

const renderForm = (
  authenticated: CookieAuthentication,
  request: Parameters<typeof renderUiPage>[0],
  kind: TransactionKind,
  id: number,
  values: TransactionValues,
  errors: Readonly<Record<string, string>>,
  isEdit: boolean
) =>
  Effect.gen(function*() {
    const options = yield* formOptions(kind)
    const common = {
      Entry: {
        ID: id,
        EntryDate: values.entryDate,
        Description: values.description
      },
      Amount: values.amount,
      Errors: errors,
      IsEdit: isEdit
    }
    const data = kind === "income"
      ? {
        ...common,
        RevenueAccount: values.primaryAccount,
        DepositAccount: values.cashAccount,
        RevenueAccounts: options.primary.map(accountData),
        DepositAccounts: options.cash.map(accountData)
      }
      : {
        ...common,
        ExpenseAccount: values.primaryAccount,
        PaymentAccount: values.cashAccount,
        VehicleID: values.vehicleId,
        ExpenseAccounts: options.primary.map(accountData),
        PaymentAccounts: options.cash.map(accountData),
        Vehicles: options.vehicles.map((vehicle) => ({
          ID: vehicle.id,
          Code: vehicle.code
        }))
      }
    return renderUiPage(
      request,
      `${kind === "income" ? "income" : "expenses"}/form`,
      isEdit ? kindConfig[kind].editTitle : kindConfig[kind].newTitle,
      data,
      authenticated
    )
  })

const emptyValues: TransactionValues = {
  entryDate: "",
  description: "",
  amount: 0,
  primaryAccount: 0,
  cashAccount: 0,
  vehicleId: 0
}

const pageFromQuery = (value: string | null) => {
  const page = Number.parseInt(value ?? "", 10)
  return Number.isNaN(page) || page < 1 ? 1 : page
}

const pageUrl = (
  page: number,
  filter: {
    readonly from: string
    readonly to: string
    readonly search: string
  }
) => {
  const entries = [
    ["from", filter.from],
    ["page", String(page)],
    ["search", filter.search],
    ["to", filter.to]
  ].filter(([, value]) => value !== "")
  return `?${new URLSearchParams(entries).toString()}`
}

const pagination = (
  page: number,
  total: number,
  filter: {
    readonly from: string
    readonly to: string
    readonly search: string
  }
) => {
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  return {
    Page: page,
    PageSize: pageSize,
    Total: total,
    TotalPages: totalPages,
    HasPrev: page > 1,
    HasNext: page < totalPages,
    PrevURL: pageUrl(page - 1, filter),
    NextURL: pageUrl(page + 1, filter)
  }
}

const listEntry = (entry: JournalEntry) => ({
  ID: entry.id,
  EntryDate: entry.entry_date,
  Reference: entry.reference,
  Description: entry.description,
  AccountSummary: entry.account_summary ?? "",
  VehicleCode: entry.vehicle_code ?? "",
  TotalDebit: Number(entry.total_debit)
})

const notFound = () => uiPlainError(404, "404 page not found")
const internal = () => uiPlainError(500, "Internal Server Error")

const listRoute = (kind: TransactionKind) =>
  HttpRouter.get(
    `${dashboardBasePath}/${kind === "income" ? "income" : "expenses"}`,
    protectedUiHandler((authenticated, request) => {
      const query = new URL(request.url, "http://localhost").searchParams
      const filter = {
        from: query.get("from") ?? "",
        to: query.get("to") ?? "",
        search: query.get("search") ?? ""
      }
      const requestedPage = pageFromQuery(query.get("page"))
      return Effect.gen(function*() {
        const journals = yield* Journals
        let result = yield* journals.list({
          sourceType: kind,
          dateFrom: filter.from,
          dateTo: filter.to,
          search: filter.search,
          limit: pageSize,
          offset: (requestedPage - 1) * pageSize
        })
        const totalPages = Math.max(1, Math.ceil(result.total / pageSize))
        const page = Math.min(requestedPage, totalPages)
        if (page !== requestedPage) {
          result = yield* journals.list({
            sourceType: kind,
            dateFrom: filter.from,
            dateTo: filter.to,
            search: filter.search,
            limit: pageSize,
            offset: (page - 1) * pageSize
          })
        }
        return renderUiPage(
          request,
          `${kind === "income" ? "income" : "expenses"}/index`,
          kindConfig[kind].pageTitle,
          {
            Entries: result.entries.map(listEntry),
            Pagination: pagination(page, result.total, filter)
          },
          authenticated
        )
      }).pipe(Effect.catchTag("JournalStoreError", () => Effect.succeed(internal())))
    })
  )

const newRoute = (kind: TransactionKind) =>
  HttpRouter.get(
    `${dashboardBasePath}/${kind === "income" ? "income" : "expenses"}/new`,
    protectedUiHandler((authenticated, request) =>
      renderForm(authenticated, request, kind, 0, emptyValues, {}, false)
    )
  )

const createRoute = (kind: TransactionKind) =>
  HttpRouter.post(
    `${dashboardBasePath}/${kind === "income" ? "income" : "expenses"}`,
    protectedUiHandler((authenticated, request, form) => {
      if (!hasManage(authenticated, kind)) {
        return Effect.succeed(uiPlainError(403, "Forbidden"))
      }
      const values = valuesFromForm(form, kind)
      const errors = validation(values, kind)
      if (Object.keys(errors).length > 0) {
        return renderForm(
          authenticated,
          request,
          kind,
          0,
          values,
          errors,
          false
        )
      }
      return Effect.gen(function*() {
        const journals = yield* Journals
        const created = yield* journals.create({
          entryDate: values.entryDate,
          description: values.description,
          sourceType: kind,
          isPosted: true,
          createdBy: authenticated.user.id,
          lines: linesFor(values, kind),
          ...(kind === "expense" && values.vehicleId !== 0
            ? { vehicleId: values.vehicleId }
            : {})
        })
        const audit = yield* Audit
        const after = {
          entry_date: values.entryDate,
          description: values.description,
          amount: values.amount,
          [kind === "income" ? "revenue_account" : "expense_account"]:
            values.primaryAccount,
          [kind === "income" ? "deposit_account" : "payment_account"]:
            values.cashAccount,
          ...(kind === "expense" ? { vehicle_id: values.vehicleId } : {})
        }
        yield* audit.log(requestMetadata(request), {
          action: `${kind}.create`,
          actor: {
            id: authenticated.user.id,
            username: authenticated.user.username
          },
          targetType: kind,
          targetId: created.id,
          targetLabel: values.description,
          metadata: { after }
        })
        return uiRedirect(`${dashboardBasePath}/journals/${created.id}`, {
          "set-cookie": uiFlashCookie(kindConfig[kind].createFlash)
        })
      }).pipe(
        Effect.catchTags({
          JournalValidationError: (error) =>
            renderForm(
              authenticated,
              request,
              kind,
              0,
              values,
              { general: error.message },
              false
            ),
          JournalStoreError: (error) =>
            renderForm(
              authenticated,
              request,
              kind,
              0,
              values,
              { general: String(error.cause) },
              false
            )
        })
      )
    })
  )

const editRoute = (kind: TransactionKind) =>
  HttpRouter.get(
    `${dashboardBasePath}/${kind === "income" ? "income" : "expenses"}/:id/edit`,
    protectedUiHandler((authenticated, request) =>
      Effect.gen(function*() {
        const id = parseId((yield* HttpRouter.params).id)
        if (id === undefined) {
          return notFound()
        }
        const journals = yield* Journals
        const entry = yield* journals.get(id)
        if (entry.source_type !== kind) {
          return notFound()
        }
        return yield* renderForm(
          authenticated,
          request,
          kind,
          id,
          shapeFromEntry(entry, kind),
          {},
          true
        )
      }).pipe(
        Effect.catchTags({
          JournalNotFound: () => Effect.succeed(notFound()),
          JournalStoreError: () => Effect.succeed(notFound())
        })
      )
    )
  )

const updateRoute = (kind: TransactionKind) =>
  HttpRouter.post(
    `${dashboardBasePath}/${kind === "income" ? "income" : "expenses"}/:id`,
    protectedUiHandler((authenticated, request, form) => {
      if (!hasManage(authenticated, kind)) {
        return Effect.succeed(uiPlainError(403, "Forbidden"))
      }
      return Effect.gen(function*() {
        const id = parseId((yield* HttpRouter.params).id)
        if (id === undefined) {
          return notFound()
        }
        const journals = yield* Journals
        const existing = yield* journals.get(id)
        if (existing.source_type !== kind) {
          return notFound()
        }
        const values = valuesFromForm(form, kind)
        const errors = validation(values, kind)
        if (Object.keys(errors).length > 0) {
          return yield* renderForm(
            authenticated,
            request,
            kind,
            id,
            values,
            errors,
            true
          )
        }
        const before = shapeFromEntry(existing, kind)
        yield* journals.updateBySource(
          id,
          kind,
          values.entryDate,
          values.description,
          linesFor(values, kind),
          kind === "expense" && values.vehicleId !== 0
            ? values.vehicleId
            : undefined
        )
        const fields = kind === "income"
          ? [
            "entry_date",
            "description",
            "amount",
            "revenue_account",
            "deposit_account"
          ]
          : [
            "entry_date",
            "description",
            "amount",
            "expense_account",
            "payment_account",
            "vehicle_id"
          ]
        const oldFields = {
          entry_date: before.entryDate,
          description: before.description,
          amount: before.amount,
          [kind === "income" ? "revenue_account" : "expense_account"]:
            before.primaryAccount,
          [kind === "income" ? "deposit_account" : "payment_account"]:
            before.cashAccount,
          ...(kind === "expense" ? { vehicle_id: before.vehicleId } : {})
        }
        const newFields = {
          entry_date: values.entryDate,
          description: values.description,
          amount: values.amount,
          [kind === "income" ? "revenue_account" : "expense_account"]:
            values.primaryAccount,
          [kind === "income" ? "deposit_account" : "payment_account"]:
            values.cashAccount,
          ...(kind === "expense" ? { vehicle_id: values.vehicleId } : {})
        }
        const metadata = auditDiff(oldFields, newFields, fields)
        if (metadata !== undefined) {
          const audit = yield* Audit
          yield* audit.log(requestMetadata(request), {
            action: `${kind}.update`,
            actor: {
              id: authenticated.user.id,
              username: authenticated.user.username
            },
            targetType: kind,
            targetId: id,
            targetLabel: values.description,
            metadata
          })
        }
        return uiRedirect(`${dashboardBasePath}/journals/${id}`, {
          "set-cookie": uiFlashCookie(kindConfig[kind].updateFlash)
        })
      }).pipe(
        Effect.catchTags({
          JournalNotFound: () => Effect.succeed(notFound()),
          JournalConflict: (error) => Effect.succeed(uiPlainError(
            500,
            error.message
          )),
          JournalValidationError: (error) => Effect.succeed(uiPlainError(
            500,
            error.message
          )),
          JournalStoreError: () => Effect.succeed(internal())
        })
      )
    })
  )

const deleteRoute = (kind: TransactionKind) =>
  HttpRouter.del(
    `${dashboardBasePath}/${kind === "income" ? "income" : "expenses"}/:id`,
    protectedUiHandler((authenticated, request) => {
      if (!hasManage(authenticated, kind)) {
        return Effect.succeed(uiPlainError(403, "Forbidden"))
      }
      return Effect.gen(function*() {
        const id = parseId((yield* HttpRouter.params).id)
        if (id === undefined) {
          return notFound()
        }
        const journals = yield* Journals
        const existing = yield* journals.get(id).pipe(Effect.option)
        const removed = yield* journals.removeBySource(id, kind).pipe(
          Effect.as({ ok: true as const }),
          Effect.catchAll((error) =>
            Effect.succeed({ ok: false as const, error })
          )
        )
        if (!removed.ok) {
          const message = "message" in removed.error
            ? String(removed.error.message)
            : String(removed.error)
          return uiRedirect(
            `${dashboardBasePath}/${kind === "income" ? "income" : "expenses"}`,
            { "set-cookie": uiFlashCookie(`Error: ${message}`) }
          )
        }
        if (existing._tag === "Some") {
          const entry = existing.value
          const values = shapeFromEntry(entry, kind)
          const audit = yield* Audit
          yield* audit.log(requestMetadata(request), {
            action: `${kind}.delete`,
            actor: {
              id: authenticated.user.id,
              username: authenticated.user.username
            },
            targetType: kind,
            targetId: id,
            targetLabel: entry.description,
            metadata: {
              before: {
                entry_date: values.entryDate,
                description: values.description,
                amount: values.amount,
                [kind === "income"
                  ? "revenue_account"
                  : "expense_account"]: values.primaryAccount,
                [kind === "income"
                  ? "deposit_account"
                  : "payment_account"]: values.cashAccount,
                ...(kind === "expense"
                  ? { vehicle_id: values.vehicleId }
                  : {})
              }
            }
          })
        }
        if (request.headers["hx-request"] === "true") {
          return HttpServerResponse.empty({ status: 200 })
        }
        return uiRedirect(
          `${dashboardBasePath}/${kind === "income" ? "income" : "expenses"}`,
          { "set-cookie": uiFlashCookie(kindConfig[kind].deleteFlash) }
        )
      })
    })
  )

const addIncomeRoutes = [
  listRoute("income"),
  newRoute("income"),
  createRoute("income"),
  editRoute("income"),
  updateRoute("income"),
  deleteRoute("income")
] as const

const addExpenseRoutes = [
  listRoute("expense"),
  newRoute("expense"),
  createRoute("expense"),
  editRoute("expense"),
  updateRoute("expense"),
  deleteRoute("expense")
] as const

export const addUiIncomeExpenseRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) =>
  router.pipe(...addIncomeRoutes).pipe(...addExpenseRoutes)
