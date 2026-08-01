import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import {
  Bills,
  validateBill,
  validateBillPayment,
  type BillInputLine
} from "../../domain/accounting/bills.ts"
import { Audit } from "../../domain/audit/audit.ts"
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

type BillInput = {
  readonly contactId: number
  readonly billDate: string
  readonly dueDate: string
  readonly taxAmount: string
  readonly notes: string
  readonly lines: ReadonlyArray<BillInputLine>
}

type PaymentInput = {
  readonly amount: string
  readonly paymentDate: string
  readonly paymentAccount: number
}

const billFields = [
  "contact_id",
  "bill_date",
  "due_date",
  "tax_amount",
  "notes",
  "lines"
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

const billFromObject = (
  input: Readonly<Record<string, unknown>>
): Effect.Effect<BillInput, InvalidJsonBody> =>
  Effect.try({
    try: () => {
      const rawLines = input.lines
      if (
        rawLines !== undefined &&
        rawLines !== null &&
        !Array.isArray(rawLines)
      ) {
        throw new Error("invalid lines")
      }
      const lines = (rawLines ?? []).map((rawLine) => {
        if (
          typeof rawLine !== "object" ||
          rawLine === null ||
          Array.isArray(rawLine)
        ) {
          throw new Error("invalid bill line")
        }
        const line = rawLine as Readonly<Record<string, unknown>>
        if (
          Object.keys(line).some((key) =>
            ![
              "description",
              "quantity",
              "unit_price",
              "account_id"
            ].includes(key)
          )
        ) {
          throw new Error("unknown bill line field")
        }
        return {
          description: stringValue(line, "description"),
          quantity: stringValue(line, "quantity"),
          unitPrice: stringValue(line, "unit_price"),
          accountId: integerValue(line, "account_id")
        }
      })
      return {
        contactId: integerValue(input, "contact_id"),
        billDate: stringValue(input, "bill_date"),
        dueDate: stringValue(input, "due_date"),
        taxAmount: stringValue(input, "tax_amount"),
        notes: stringValue(input, "notes"),
        lines
      }
    },
    catch: () => new InvalidJsonBody()
  })

const parseBill = (request: HttpServerRequest.HttpServerRequest) =>
  readJsonObject(request, billFields).pipe(
    Effect.flatMap(billFromObject)
  )

const parseBillText = (body: string) =>
  parseJsonObject(body, billFields).pipe(
    Effect.flatMap(billFromObject)
  )

const paymentFromObject = (
  input: Readonly<Record<string, unknown>>
): Effect.Effect<PaymentInput, InvalidJsonBody> =>
  Effect.try({
    try: () => ({
      amount: stringValue(input, "amount"),
      paymentDate: stringValue(input, "payment_date"),
      paymentAccount: integerValue(input, "payment_account")
    }),
    catch: () => new InvalidJsonBody()
  })

const parsePayment = (request: HttpServerRequest.HttpServerRequest) =>
  readJsonObject(request, [
    "amount",
    "payment_date",
    "payment_account"
  ]).pipe(Effect.flatMap(paymentFromObject))

const parsePaymentText = (body: string) =>
  parseJsonObject(body, [
    "amount",
    "payment_date",
    "payment_account"
  ]).pipe(Effect.flatMap(paymentFromObject))

const parseId = (value: string | undefined) => {
  if (value === undefined || !/^[+-]?\d+$/.test(value)) {
    return undefined
  }
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : undefined
}

const canManage = (authentication: Authenticated) =>
  authentication.effectiveCapabilities.includes("bills.manage")

const actor = (authentication: Authenticated) => ({
  id: authentication.user.id,
  username: authentication.user.username,
  ...(authentication.method === "bearer"
    ? { tokenId: authentication.tokenId }
    : {})
})

const forbidden = () =>
  apiError(
    403,
    "forbidden",
    "bills.manage capability required"
  )

const storeMessage = (error: { readonly cause: unknown }) =>
  error.cause instanceof Error
    ? error.cause.message
    : String(error.cause)

const addListRoute = HttpRouter.get(
  "/api/v1/bills",
  protectedApiHandler((_authentication, request) =>
    Effect.gen(function*() {
      const query = new URL(request.url, "http://localhost").searchParams
      const page = parsePage(request)
      const bills = yield* Bills
      const result = yield* bills.list({
        status: query.get("status") ?? "",
        search: query.get("search") ?? "",
        limit: page.perPage,
        offset: (page.page - 1) * page.perPage
      })
      return jsonResponse({
        data: result.bills,
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
        "BillStoreError",
        () => Effect.succeed(
          apiError(500, "internal_error", "failed to list bills")
        )
      )
    )
  )
)

const addGetRoute = HttpRouter.get(
  "/api/v1/bills/:id",
  protectedApiHandler(() =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "bill not found")
      }
      const bills = yield* Bills
      return jsonResponse(yield* bills.get(id))
    }).pipe(
      Effect.catchTags({
        BillNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "bill not found")),
        BillStoreError: () =>
          Effect.succeed(apiError(404, "not_found", "bill not found"))
      })
    )
  )
)

const createBill = (
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest,
  input: Effect.Effect<BillInput, InvalidJsonBody>
) => {
  if (!canManage(authentication)) {
    return Effect.succeed(forbidden())
  }
  return Effect.gen(function*() {
    const values = yield* input
    const validated = validateBill(values)
    if (
      validated.lines === undefined ||
      validated.taxAmount === undefined
    ) {
      return apiError(
        422,
        "validation_failed",
        "validation failed",
        validated.fields
      )
    }
    const bills = yield* Bills
    const created = yield* bills.create({
      ...values,
      taxAmount: validated.taxAmount,
      lines: validated.lines
    }, authentication.user.id)
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "bill.create",
      actor: actor(authentication),
      targetType: "bill",
      targetId: created.id,
      targetLabel: created.bill_number,
      metadata: {
        after: {
          contact_id: created.contact_id,
          bill_date: created.bill_date,
          due_date: created.due_date,
          tax_amount: created.tax_amount,
          total: created.total,
          line_count: created.lines?.length ?? 0
        }
      }
    })
    return jsonResponse(created, 201)
  }).pipe(
    Effect.catchTags({
      InvalidJsonBody: () =>
        Effect.succeed(
          apiError(400, "invalid_request", "invalid request body")
        ),
      BillStoreError: (error) =>
        Effect.succeed(
          apiError(422, "validation_failed", storeMessage(error))
        )
    })
  )
}

const addCreateRoute = HttpRouter.post(
  "/api/v1/bills",
  protectedApiHandler((authentication, request) => {
    const key = request.headers["idempotency-key"] ?? ""
    if (key === "") {
      return createBill(authentication, request, parseBill(request))
    }
    return readBodyText(request).pipe(
      Effect.flatMap((body) =>
        runIdempotently(
          authentication,
          request,
          body,
          createBill(
            authentication,
            request,
            parseBillText(body)
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
  "/api/v1/bills/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "bill not found")
      }
      const bills = yield* Bills
      const existing = yield* bills.get(id)
      if (existing.status !== "draft") {
        return apiError(
          409,
          "conflict",
          `can only edit draft bills (current: ${existing.status})`
        )
      }
      const values = yield* parseBill(request)
      const validated = validateBill(values)
      if (
        validated.lines === undefined ||
        validated.taxAmount === undefined
      ) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          validated.fields
        )
      }
      const result = yield* bills.update(id, {
        ...values,
        taxAmount: validated.taxAmount,
        lines: validated.lines
      })
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "bill.update",
        actor: actor(authentication),
        targetType: "bill",
        targetId: id,
        targetLabel: result.after.bill_number,
        metadata: {
          before: {
            contact_id: result.before.contact_id,
            bill_date: result.before.bill_date,
            due_date: result.before.due_date,
            tax_amount: result.before.tax_amount,
            total: result.before.total
          },
          after: {
            contact_id: result.after.contact_id,
            bill_date: result.after.bill_date,
            due_date: result.after.due_date,
            tax_amount: result.after.tax_amount,
            total: result.after.total
          }
        }
      })
      return jsonResponse(result.after)
    }).pipe(
      Effect.catchTags({
        InvalidJsonBody: () =>
          Effect.succeed(
            apiError(400, "invalid_request", "invalid request body")
          ),
        BillNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "bill not found")),
        BillConflict: (error) =>
          Effect.succeed(apiError(409, "conflict", error.message)),
        BillStoreError: (error) =>
          Effect.succeed(
            apiError(422, "validation_failed", storeMessage(error))
          )
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/api/v1/bills/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "bill not found")
      }
      const bills = yield* Bills
      const existing = yield* bills.get(id)
      yield* bills.remove(id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "bill.delete",
        actor: actor(authentication),
        targetType: "bill",
        targetId: id,
        targetLabel: existing.bill_number,
        metadata: {
          before: {
            contact_id: existing.contact_id,
            status: existing.status,
            total: existing.total
          }
        }
      })
      return HttpServerResponse.empty({ status: 204 })
    }).pipe(
      Effect.catchTags({
        BillNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "bill not found")),
        BillConflict: (error) =>
          Effect.succeed(apiError(409, "conflict", error.message)),
        BillStoreError: (error) =>
          Effect.succeed(
            apiError(409, "conflict", storeMessage(error))
          )
      })
    )
  })
)

const receiveBill = (
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest,
  id: number | undefined
) => {
  if (!canManage(authentication)) {
    return Effect.succeed(forbidden())
  }
  if (id === undefined) {
    return Effect.succeed(apiError(404, "not_found", "bill not found"))
  }
  return Effect.gen(function*() {
    const bills = yield* Bills
    yield* bills.get(id)
    const updated = yield* bills.receive(id, authentication.user.id)
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "bill.receive",
      actor: actor(authentication),
      targetType: "bill",
      targetId: id,
      targetLabel: updated.bill_number,
      metadata: {
        after: { status: updated.status },
        journal_id: updated.journal_id
      }
    })
    return jsonResponse(updated)
  }).pipe(
    Effect.catchTags({
      BillNotFound: () =>
        Effect.succeed(apiError(404, "not_found", "bill not found")),
      BillConflict: (error) =>
        Effect.succeed(apiError(409, "conflict", error.message)),
      BillStoreError: (error) =>
        Effect.succeed(
          apiError(409, "conflict", storeMessage(error))
        )
    })
  )
}

const addReceiveRoute = HttpRouter.post(
  "/api/v1/bills/:id/receive",
  protectedApiHandler((authentication, request) =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      const operation = receiveBill(authentication, request, id)
      const key = request.headers["idempotency-key"] ?? ""
      if (key === "") {
        return yield* operation
      }
      const body = yield* readBodyText(request)
      return yield* runIdempotently(
        authentication,
        request,
        body,
        operation
      )
    }).pipe(
      Effect.catchTag(
        "InvalidJsonBody",
        () => Effect.succeed(
          apiError(400, "invalid_request", "failed to read request body")
        )
      )
    )
  )
)

const paymentOperation = (
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest,
  id: number | undefined,
  input: Effect.Effect<PaymentInput, InvalidJsonBody>
) => {
  if (!canManage(authentication)) {
    return Effect.succeed(forbidden())
  }
  if (id === undefined) {
    return Effect.succeed(apiError(404, "not_found", "bill not found"))
  }
  return Effect.gen(function*() {
    const bills = yield* Bills
    yield* bills.get(id)
    const values = yield* input
    const validated = validateBillPayment(values)
    if (validated.amount === undefined) {
      return apiError(
        422,
        "validation_failed",
        "validation failed",
        validated.fields
      )
    }
    const updated = yield* bills.recordPayment(
      id,
      validated.amount,
      values.paymentDate,
      values.paymentAccount,
      authentication.user.id
    )
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "bill.payment",
      actor: actor(authentication),
      targetType: "bill",
      targetId: id,
      targetLabel: updated.bill_number,
      metadata: {
        amount: validated.amount,
        payment_date: values.paymentDate,
        payment_account_id: values.paymentAccount,
        status_after: updated.status
      }
    })
    return jsonResponse(updated)
  }).pipe(
    Effect.catchTags({
      InvalidJsonBody: () =>
        Effect.succeed(
          apiError(400, "invalid_request", "invalid request body")
        ),
      BillNotFound: () =>
        Effect.succeed(apiError(404, "not_found", "bill not found")),
      BillConflict: (error) =>
        Effect.succeed(apiError(409, "conflict", error.message)),
      BillStoreError: (error) =>
        Effect.succeed(
          apiError(409, "conflict", storeMessage(error))
        )
    })
  )
}

const addPaymentRoute = HttpRouter.post(
  "/api/v1/bills/:id/payment",
  protectedApiHandler((authentication, request) =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      const key = request.headers["idempotency-key"] ?? ""
      if (key === "") {
        return yield* paymentOperation(
          authentication,
          request,
          id,
          parsePayment(request)
        )
      }
      const body = yield* readBodyText(request)
      return yield* runIdempotently(
        authentication,
        request,
        body,
        paymentOperation(
          authentication,
          request,
          id,
          parsePaymentText(body)
        )
      )
    }).pipe(
      Effect.catchTag(
        "InvalidJsonBody",
        () => Effect.succeed(
          apiError(400, "invalid_request", "failed to read request body")
        )
      )
    )
  )
)

export const addBillApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addListRoute,
  addGetRoute,
  addCreateRoute,
  addUpdateRoute,
  addDeleteRoute,
  addReceiveRoute,
  addPaymentRoute
)
