import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import {
  ExpenseNotFound,
  Expenses,
  validateExpense
} from "../../domain/accounting/expenses.ts"
import { JournalStoreError } from "../../domain/accounting/journals.ts"
import { Audit, auditDiff } from "../../domain/audit/audit.ts"
import type { Authenticated } from "../../domain/auth/authentication.ts"
import { apiError, jsonResponse } from "./api-response.ts"
import { protectedApiHandler } from "./auth-api.ts"
import { runIdempotently } from "./idempotent-response.ts"
import {
  InvalidJsonBody,
  parseJsonObject,
  readBodyText,
  readJsonObject
} from "./json-body.ts"
import { parsePage } from "./pagination.ts"
import { requestMetadata } from "./request-metadata.ts"

type ExpenseInput = {
  readonly entryDate: string
  readonly description: string
  readonly amount: string
  readonly expenseAccount: number
  readonly paymentAccount: number
}

const fields = [
  "entry_date",
  "description",
  "amount",
  "expense_account",
  "payment_account"
] as const

const stringValue = (
  input: Readonly<Record<string, unknown>>,
  field: string
) => {
  const value = input[field]
  if (value === undefined || value === null) {
    return ""
  }
  if (typeof value !== "string") {
    throw new Error(`invalid ${field}`)
  }
  return value
}

const integerValue = (
  input: Readonly<Record<string, unknown>>,
  field: string
) => {
  const value = input[field]
  if (value === undefined || value === null) {
    return 0
  }
  if (typeof value !== "number" || !Number.isSafeInteger(value)) {
    throw new Error(`invalid ${field}`)
  }
  return value
}

const fromObject = (
  input: Readonly<Record<string, unknown>>
): Effect.Effect<ExpenseInput, InvalidJsonBody> =>
  Effect.try({
    try: () => ({
      entryDate: stringValue(input, "entry_date"),
      description: stringValue(input, "description"),
      amount: stringValue(input, "amount"),
      expenseAccount: integerValue(input, "expense_account"),
      paymentAccount: integerValue(input, "payment_account")
    }),
    catch: () => new InvalidJsonBody()
  })

const parseInput = (request: HttpServerRequest.HttpServerRequest) =>
  readJsonObject(request, fields).pipe(Effect.flatMap(fromObject))

const parseInputText = (body: string) =>
  parseJsonObject(body, fields).pipe(Effect.flatMap(fromObject))

const parseId = (value: string | undefined) => {
  if (value === undefined || !/^[+-]?\d+$/.test(value)) {
    return undefined
  }
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : undefined
}

const canManage = (authentication: Authenticated) =>
  authentication.effectiveCapabilities.includes("expenses.manage")

const actor = (authentication: Authenticated) => ({
  id: authentication.user.id,
  username: authentication.user.username,
  ...(authentication.method === "bearer"
    ? { tokenId: authentication.tokenId }
    : {})
})

const errorMessage = (error: JournalStoreError) =>
  error.cause instanceof Error
    ? error.cause.message
    : String(error.cause)

const addListRoute = HttpRouter.get(
  "/api/v1/expenses",
  protectedApiHandler((_authentication, request) =>
    Effect.gen(function*() {
      const query = new URL(request.url, "http://localhost").searchParams
      const page = parsePage(request)
      const expenses = yield* Expenses
      const result = yield* expenses.list({
        dateFrom: query.get("from") ?? "",
        dateTo: query.get("to") ?? "",
        search: query.get("search") ?? "",
        limit: page.perPage,
        offset: (page.page - 1) * page.perPage
      })
      return jsonResponse({
        data: result.entries,
        meta: {
          page: page.page,
          per_page: page.perPage,
          total: result.total,
          total_pages: result.total === 0
            ? 0
            : Math.ceil(result.total / page.perPage)
        }
      })
    }).pipe(
      Effect.catchTag(
        "JournalStoreError",
        () => Effect.succeed(
          apiError(
            500,
            "internal_error",
            "failed to list expense entries"
          )
        )
      )
    )
  )
)

const addGetRoute = HttpRouter.get(
  "/api/v1/expenses/:id",
  protectedApiHandler(() =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "expense entry not found")
      }
      const expenses = yield* Expenses
      const entry = yield* expenses.get(id)
      return jsonResponse({ data: entry })
    }).pipe(
      Effect.catchTags({
        ExpenseNotFound: () =>
          Effect.succeed(
            apiError(404, "not_found", "expense entry not found")
          ),
        JournalStoreError: () =>
          Effect.succeed(
            apiError(404, "not_found", "expense entry not found")
          )
      })
    )
  )
)

const createExpense = (
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest,
  input: Effect.Effect<ExpenseInput, InvalidJsonBody>
) => {
  if (!canManage(authentication)) {
    return Effect.succeed(
      apiError(
        403,
        "forbidden",
        "expenses.manage capability required"
      )
    )
  }
  return Effect.gen(function*() {
    const values = yield* input
    const validated = validateExpense(values)
    if (validated.amount === undefined) {
      return apiError(
        422,
        "validation_failed",
        "validation failed",
        validated.fields
      )
    }
    const expenses = yield* Expenses
    const created = yield* expenses.create(
      { ...values, amount: validated.amount },
      authentication.user.id
    )
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "expense.create",
      actor: actor(authentication),
      targetType: "expense",
      targetId: created.id,
      targetLabel: values.description,
      metadata: {
        after: {
          entry_date: values.entryDate,
          description: values.description,
          amount: validated.amount,
          expense_account: values.expenseAccount,
          payment_account: values.paymentAccount
        }
      }
    })
    return jsonResponse({ data: created }, 201)
  }).pipe(
    Effect.catchTags({
      InvalidJsonBody: () =>
        Effect.succeed(
          apiError(400, "invalid_request", "invalid request body")
        ),
      JournalValidationError: () =>
        Effect.succeed(
          apiError(
            500,
            "internal_error",
            "failed to create expense entry"
          )
        ),
      JournalStoreError: () =>
        Effect.succeed(
          apiError(
            500,
            "internal_error",
            "failed to create expense entry"
          )
        )
    })
  )
}

const addCreateRoute = HttpRouter.post(
  "/api/v1/expenses",
  protectedApiHandler((authentication, request) => {
    const key = request.headers["idempotency-key"] ?? ""
    if (key === "") {
      return createExpense(authentication, request, parseInput(request))
    }
    return readBodyText(request).pipe(
      Effect.flatMap((body) =>
        runIdempotently(
          authentication,
          request,
          body,
          createExpense(
            authentication,
            request,
            parseInputText(body)
          )
        )
      ),
      Effect.catchTag(
        "InvalidJsonBody",
        () => Effect.succeed(
          apiError(400, "invalid_request", "failed to read request body")
        )
      )
    )
  })
)

const addUpdateRoute = HttpRouter.put(
  "/api/v1/expenses/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(
        apiError(
          403,
          "forbidden",
          "expenses.manage capability required"
        )
      )
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "expense entry not found")
      }
      const expenses = yield* Expenses
      yield* expenses.get(id)
      const values = yield* parseInput(request)
      const validated = validateExpense(values)
      if (validated.amount === undefined) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          validated.fields
        )
      }
      const { before, after } = yield* expenses.update(id, {
        ...values,
        amount: validated.amount
      })
      const metadata = auditDiff(
        {
          entry_date: before.entry_date,
          description: before.description,
          amount: Number(before.amount),
          expense_account: before.expense_account?.id ?? 0,
          payment_account: before.payment_account?.id ?? 0
        },
        {
          entry_date: values.entryDate,
          description: values.description,
          amount: validated.amount,
          expense_account: values.expenseAccount,
          payment_account: values.paymentAccount
        },
        [
          "entry_date",
          "description",
          "amount",
          "expense_account",
          "payment_account"
        ]
      )
      if (metadata !== undefined) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "expense.update",
          actor: actor(authentication),
          targetType: "expense",
          targetId: id,
          targetLabel: values.description,
          metadata
        })
      }
      return jsonResponse({ data: after })
    }).pipe(
      Effect.catchTags({
        InvalidJsonBody: () =>
          Effect.succeed(
            apiError(400, "invalid_request", "invalid request body")
          ),
        ExpenseNotFound: () =>
          Effect.succeed(
            apiError(404, "not_found", "expense entry not found")
          ),
        JournalConflict: () =>
          Effect.succeed(
            apiError(404, "not_found", "expense entry not found")
          ),
        JournalValidationError: () =>
          Effect.succeed(
            apiError(
              500,
              "internal_error",
              "failed to update expense entry"
            )
          ),
        JournalStoreError: () =>
          Effect.succeed(
            apiError(
              500,
              "internal_error",
              "failed to update expense entry"
            )
          )
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/api/v1/expenses/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(
        apiError(
          403,
          "forbidden",
          "expenses.manage capability required"
        )
      )
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "expense entry not found")
      }
      const expenses = yield* Expenses
      const existing = yield* expenses.remove(id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "expense.delete",
        actor: actor(authentication),
        targetType: "expense",
        targetId: id,
        targetLabel: existing.description,
        metadata: {
          before: {
            entry_date: existing.entry_date,
            description: existing.description,
            amount: Number(existing.amount),
            expense_account: existing.expense_account?.id ?? 0,
            payment_account: existing.payment_account?.id ?? 0
          }
        }
      })
      return HttpServerResponse.empty({ status: 204 })
    }).pipe(
      Effect.catchTags({
        ExpenseNotFound: () =>
          Effect.succeed(
            apiError(404, "not_found", "expense entry not found")
          ),
        JournalConflict: (error) =>
          Effect.succeed(
            apiError(409, "conflict", error.message)
          ),
        JournalStoreError: (error) =>
          Effect.succeed(
            apiError(409, "conflict", errorMessage(error))
          )
      })
    )
  })
)

export const addExpenseApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addListRoute,
  addGetRoute,
  addCreateRoute,
  addUpdateRoute,
  addDeleteRoute
)
