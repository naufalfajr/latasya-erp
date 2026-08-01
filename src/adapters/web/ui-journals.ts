import { HttpRouter, HttpServerResponse } from "@effect/platform"
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

const pageSize = 50

const hasManage = (authenticated: CookieAuthentication) =>
  authenticated.user.role === "admin" ||
  authenticated.effectiveCapabilities.includes("journals.manage")

const parseId = (value: string | undefined) =>
  value !== undefined && /^[+-]?\d+$/.test(value)
    ? Number(value)
    : undefined

const parsePage = (value: string | null) => {
  const page = Number.parseInt(value ?? "", 10)
  return Number.isNaN(page) || page < 1 ? 1 : page
}

const parseIdr = (value: string) => {
  const normalized = value
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

const lineData = (line: {
  readonly accountId: number
  readonly debit: number
  readonly credit: number
  readonly memo: string
  readonly accountCode?: string
  readonly accountName?: string
}) => ({
  AccountID: line.accountId,
  Debit: line.debit,
  Credit: line.credit,
  Memo: line.memo,
  AccountCode: line.accountCode ?? "",
  AccountName: line.accountName ?? ""
})

const entryData = (entry: JournalEntry) => ({
  ID: entry.id,
  EntryDate: entry.entry_date,
  Reference: entry.reference,
  Description: entry.description,
  SourceType: entry.source_type,
  SourceID: entry.source_id,
  IsPosted: entry.is_posted,
  CreatedBy: entry.created_by,
  CreatedByName: entry.created_by_name ?? "",
  CreatedAt: entry.created_at,
  UpdatedAt: entry.updated_at,
  TotalDebit: Number(entry.total_debit),
  TotalCredit: Number(entry.total_credit),
  Lines: (entry.lines ?? []).map((line) =>
    lineData({
      accountId: line.account_id,
      debit: Number(line.debit),
      credit: Number(line.credit),
      memo: line.memo,
      ...(line.account_code === undefined
        ? {}
        : { accountCode: line.account_code }),
      ...(line.account_name === undefined
        ? {}
        : { accountName: line.account_name })
    })
  )
})

const listEntryData = (entry: JournalEntry) => ({
  ID: entry.id,
  EntryDate: entry.entry_date,
  Reference: entry.reference,
  Description: entry.description,
  SourceType: entry.source_type,
  TotalDebit: Number(entry.total_debit),
  TotalCredit: Number(entry.total_credit)
})

const readLines = (form: URLSearchParams): ReadonlyArray<JournalLineValues> => {
  const accounts = form.getAll("line_account_id")
  const debits = form.getAll("line_debit")
  const credits = form.getAll("line_credit")
  const memos = form.getAll("line_memo")
  const lines: Array<JournalLineValues> = []
  accounts.forEach((account, index) => {
    const accountId = Number.parseInt(account, 10) || 0
    const debit = parseIdr(debits[index] ?? "")
    const credit = parseIdr(credits[index] ?? "")
    if (accountId === 0 && debit === 0 && credit === 0) {
      return
    }
    lines.push({
      accountId,
      debit,
      credit,
      memo: memos[index] ?? ""
    })
  })
  return lines
}

const validate = (
  entryDate: string,
  description: string,
  lines: ReadonlyArray<JournalLineValues>
) => {
  const errors: Record<string, string> = {}
  if (entryDate === "") {
    errors.entry_date = "Date is required"
  }
  if (description === "") {
    errors.description = "Description is required"
  }
  if (lines.length < 2) {
    errors.lines = "At least 2 lines are required"
  }
  let totalDebit = 0
  let totalCredit = 0
  lines.forEach((line, index) => {
    if (line.accountId === 0) {
      errors[`line_${index}_account`] = "Account is required"
    }
    if (line.debit === 0 && line.credit === 0) {
      errors[`line_${index}_amount`] = "Debit or credit amount is required"
    }
    if (line.debit > 0 && line.credit > 0) {
      errors[`line_${index}_amount`] =
        "Line cannot have both debit and credit"
    }
    totalDebit += line.debit
    totalCredit += line.credit
  })
  if (lines.length >= 2 && totalDebit !== totalCredit) {
    errors.balance = `Debits (${totalDebit}) must equal credits (${totalCredit})`
  }
  return errors
}

const activeAccounts = Effect.gen(function*() {
  const accounts = yield* Accounts
  return (yield* accounts.list({ type: "", search: "" }).pipe(
    Effect.orElseSucceed(() => [])
  )).filter((account) => account.is_active)
})

const renderForm = (
  authenticated: CookieAuthentication,
  request: Parameters<typeof renderUiPage>[0],
  input: {
    readonly id: number
    readonly entryDate: string
    readonly description: string
    readonly lines: ReadonlyArray<JournalLineValues>
    readonly errors: Readonly<Record<string, string>>
    readonly isEdit: boolean
  }
) =>
  Effect.gen(function*() {
    const accounts = yield* activeAccounts
    return renderUiPage(
      request,
      "journals/form",
      input.isEdit ? "Edit Journal Entry" : "New Journal Entry",
      {
        Entry: {
          ID: input.id,
          EntryDate: input.entryDate,
          Description: input.description
        },
        Lines: input.lines.map(lineData),
        Accounts: accounts.map(accountData),
        Errors: input.errors,
        IsEdit: input.isEdit
      },
      authenticated,
      ["journals/line_partial"]
    )
  })

const notFound = () => uiPlainError(404, "404 page not found")
const internal = () => uiPlainError(500, "Internal Server Error")

const addListRoute = HttpRouter.get(
  "/dashboard/journals",
  protectedUiHandler((authenticated, request) => {
    const query = new URL(request.url, "http://localhost").searchParams
    const filter = {
      dateFrom: query.get("from") ?? "",
      dateTo: query.get("to") ?? "",
      sourceType: query.get("source") ?? "",
      search: query.get("search") ?? ""
    }
    const requestedPage = parsePage(query.get("page"))
    return Effect.gen(function*() {
      const journals = yield* Journals
      let result = yield* journals.list({
        ...filter,
        limit: pageSize,
        offset: (requestedPage - 1) * pageSize
      })
      const totalPages = Math.max(1, Math.ceil(result.total / pageSize))
      const page = Math.min(requestedPage, totalPages)
      if (page !== requestedPage) {
        result = yield* journals.list({
          ...filter,
          limit: pageSize,
          offset: (page - 1) * pageSize
        })
      }
      return renderUiPage(
        request,
        "journals/index",
        "Journal Entries",
        {
          Entries: result.entries.map(listEntryData),
          Filter: {
            DateFrom: filter.dateFrom,
            DateTo: filter.dateTo,
            SourceType: filter.sourceType,
            Search: filter.search
          },
          Pagination: {
            Page: page,
            PageSize: pageSize,
            Total: result.total,
            TotalPages: totalPages,
            HasPrev: page > 1,
            HasNext: page < totalPages,
            PrevPage: page - 1,
            NextPage: page + 1
          }
        },
        authenticated
      )
    }).pipe(Effect.catchTag("JournalStoreError", () => Effect.succeed(internal())))
  })
)

const addNewRoute = HttpRouter.get(
  "/dashboard/journals/new",
  protectedUiHandler((authenticated, request) =>
    renderForm(authenticated, request, {
      id: 0,
      entryDate: "",
      description: "",
      lines: [
        { accountId: 0, debit: 0, credit: 0, memo: "" },
        { accountId: 0, debit: 0, credit: 0, memo: "" }
      ],
      errors: {},
      isEdit: false
    })
  )
)

const addCreateRoute = HttpRouter.post(
  "/dashboard/journals",
  protectedUiHandler((authenticated, request, form) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    const entryDate = form.get("entry_date") ?? ""
    const description = form.get("description") ?? ""
    const lines = readLines(form)
    const errors = validate(entryDate, description, lines)
    if (Object.keys(errors).length > 0) {
      return renderForm(authenticated, request, {
        id: 0,
        entryDate,
        description,
        lines,
        errors,
        isEdit: false
      })
    }
    return Effect.gen(function*() {
      const journals = yield* Journals
      const created = yield* journals.create({
        entryDate,
        description,
        sourceType: "manual",
        isPosted: true,
        createdBy: authenticated.user.id,
        lines
      })
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "journal.create",
        actor: {
          id: authenticated.user.id,
          username: authenticated.user.username
        },
        targetType: "journal",
        targetId: created.id,
        targetLabel: description,
        metadata: {
          after: {
            entry_date: entryDate,
            description,
            line_count: lines.length,
            total: lines.reduce((sum, line) => sum + line.debit, 0)
          }
        }
      })
      return uiRedirect(`${dashboardBasePath}/journals/${created.id}`, {
        "set-cookie": uiFlashCookie("Journal entry created successfully")
      })
    }).pipe(
      Effect.catchTags({
        JournalValidationError: (error) =>
          renderForm(authenticated, request, {
            id: 0,
            entryDate,
            description,
            lines,
            errors: { general: error.message },
            isEdit: false
          }),
        JournalStoreError: (error) =>
          renderForm(authenticated, request, {
            id: 0,
            entryDate,
            description,
            lines,
            errors: { general: String(error.cause) },
            isEdit: false
          })
      })
    )
  })
)

const addViewRoute = HttpRouter.get(
  "/dashboard/journals/:id",
  protectedUiHandler((authenticated, request) =>
    Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const journals = yield* Journals
      const entry = yield* journals.get(id)
      return renderUiPage(
        request,
        "journals/view",
        `Journal Entry ${entry.reference}`,
        entryData(entry),
        authenticated
      )
    }).pipe(
      Effect.catchTags({
        JournalNotFound: () => Effect.succeed(notFound()),
        JournalStoreError: () => Effect.succeed(notFound())
      })
    )
  )
)

const addEditRoute = HttpRouter.get(
  "/dashboard/journals/:id/edit",
  protectedUiHandler((authenticated, request) =>
    Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const journals = yield* Journals
      const entry = yield* journals.get(id)
      if (entry.source_type !== "" && entry.source_type !== "manual") {
        return uiRedirect(`${dashboardBasePath}/journals/${id}`, {
          "set-cookie": uiFlashCookie(
            "Cannot edit auto-generated journal entries"
          )
        })
      }
      return yield* renderForm(authenticated, request, {
        id,
        entryDate: entry.entry_date,
        description: entry.description,
        lines: (entry.lines ?? []).map((line) => ({
          accountId: line.account_id,
          debit: Number(line.debit),
          credit: Number(line.credit),
          memo: line.memo
        })),
        errors: {},
        isEdit: true
      })
    }).pipe(
      Effect.catchTags({
        JournalNotFound: () => Effect.succeed(notFound()),
        JournalStoreError: () => Effect.succeed(notFound())
      })
    )
  )
)

const addUpdateRoute = HttpRouter.post(
  "/dashboard/journals/:id",
  protectedUiHandler((authenticated, request, form) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    return Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const journals = yield* Journals
      const existing = yield* journals.get(id)
      const entryDate = form.get("entry_date") ?? ""
      const description = form.get("description") ?? ""
      const lines = readLines(form)
      const errors = validate(entryDate, description, lines)
      if (Object.keys(errors).length > 0) {
        return yield* renderForm(authenticated, request, {
          id,
          entryDate,
          description,
          lines,
          errors,
          isEdit: true
        })
      }
      const updated = yield* journals.updateManual(
        id,
        entryDate,
        description,
        lines
      )
      const metadata = auditDiff(
        {
          entry_date: existing.entry_date,
          description: existing.description,
          total: Number(existing.total_debit)
        },
        {
          entry_date: updated.entry_date,
          description: updated.description,
          total: Number(updated.total_debit)
        },
        ["entry_date", "description", "total"]
      )
      if (metadata !== undefined) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "journal.update",
          actor: {
            id: authenticated.user.id,
            username: authenticated.user.username
          },
          targetType: "journal",
          targetId: id,
          targetLabel: description,
          metadata
        })
      }
      return uiRedirect(`${dashboardBasePath}/journals/${id}`, {
        "set-cookie": uiFlashCookie("Journal entry updated successfully")
      })
    }).pipe(
      Effect.catchTags({
        JournalNotFound: () => Effect.succeed(notFound()),
        JournalConflict: (error) =>
          Effect.succeed(uiPlainError(500, error.message)),
        JournalValidationError: (error) =>
          Effect.succeed(uiPlainError(500, error.message)),
        JournalStoreError: () => Effect.succeed(internal())
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/dashboard/journals/:id",
  protectedUiHandler((authenticated, request) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    return Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const journals = yield* Journals
      const existing = yield* journals.get(id).pipe(Effect.option)
      const removed = yield* journals.removeManual(id).pipe(
        Effect.map((entry) => ({ entry })),
        Effect.catchAll((error) =>
          Effect.succeed({ error })
        )
      )
      if ("error" in removed) {
        const message = "message" in removed.error
          ? String(removed.error.message)
          : String(removed.error)
        return uiRedirect(`${dashboardBasePath}/journals/${id}`, {
          "set-cookie": uiFlashCookie(`Error: ${message}`)
        })
      }
      if (existing._tag === "Some") {
        const entry = existing.value
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "journal.delete",
          actor: {
            id: authenticated.user.id,
            username: authenticated.user.username
          },
          targetType: "journal",
          targetId: id,
          targetLabel: entry.description,
          metadata: {
            before: {
              entry_date: entry.entry_date,
              description: entry.description,
              total: Number(entry.total_debit),
              source_type: entry.source_type
            }
          }
        })
      }
      if (request.headers["hx-request"] === "true") {
        return HttpServerResponse.empty({ status: 200 })
      }
      return uiRedirect(`${dashboardBasePath}/journals`, {
        "set-cookie": uiFlashCookie("Journal entry deleted successfully")
      })
    })
  })
)

export const addUiJournalRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) =>
  router.pipe(
    addListRoute,
    addNewRoute,
    addCreateRoute,
    addEditRoute,
    addUpdateRoute,
    addDeleteRoute,
    addViewRoute
  )
