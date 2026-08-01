import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import {
  Invoices,
  validateInvoice,
  validateInvoicePayment,
  type InvoiceInputLine
} from "../../domain/accounting/invoices.ts"
import { Audit } from "../../domain/audit/audit.ts"
import type { Authenticated } from "../../domain/auth/authentication.ts"
import { CompanyProfiles } from "../../domain/company/profile.ts"
import { renderInvoicePdf } from "../../domain/documents/invoice-pdf.ts"
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

type InvoiceInput = {
  readonly contactId: number
  readonly invoiceDate: string
  readonly dueDate: string
  readonly taxAmount: string
  readonly notes: string
  readonly lines: ReadonlyArray<InvoiceInputLine>
}

type PaymentInput = {
  readonly amount: string
  readonly paymentDate: string
  readonly paymentAccount: number
}

const invoiceFields = [
  "contact_id",
  "invoice_date",
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

const invoiceFromObject = (
  input: Readonly<Record<string, unknown>>
): Effect.Effect<InvoiceInput, InvalidJsonBody> =>
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
          throw new Error("invalid invoice line")
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
          throw new Error("unknown invoice line field")
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
        invoiceDate: stringValue(input, "invoice_date"),
        dueDate: stringValue(input, "due_date"),
        taxAmount: stringValue(input, "tax_amount"),
        notes: stringValue(input, "notes"),
        lines
      }
    },
    catch: () => new InvalidJsonBody()
  })

const parseInvoice = (request: HttpServerRequest.HttpServerRequest) =>
  readJsonObject(request, invoiceFields).pipe(
    Effect.flatMap(invoiceFromObject)
  )

const parseInvoiceText = (body: string) =>
  parseJsonObject(body, invoiceFields).pipe(
    Effect.flatMap(invoiceFromObject)
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

const parseIds = (
  request: HttpServerRequest.HttpServerRequest
) =>
  readJsonObject(request, ["ids"]).pipe(
    Effect.flatMap((input) =>
      Effect.try({
        try: () => {
          const rawIds = input.ids
          if (
            rawIds !== undefined &&
            rawIds !== null &&
            !Array.isArray(rawIds)
          ) {
            throw new Error("invalid ids")
          }
          return (rawIds ?? []).map((value) => {
            if (value === null) {
              return 0
            }
            if (
              typeof value !== "number" ||
              !Number.isSafeInteger(value)
            ) {
              throw new Error("invalid id")
            }
            return value
          })
        },
        catch: () => new InvalidJsonBody()
      })
    )
  )

const parseId = (value: string | undefined) => {
  if (value === undefined || !/^[+-]?\d+$/.test(value)) {
    return undefined
  }
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : undefined
}

const canManage = (authentication: Authenticated) =>
  authentication.effectiveCapabilities.includes("invoices.manage")

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
    "invoices.manage capability required"
  )

const addListRoute = HttpRouter.get(
  "/api/v1/invoices",
  protectedApiHandler((_authentication, request) =>
    Effect.gen(function*() {
      const query = new URL(request.url, "http://localhost").searchParams
      const page = parsePage(request)
      const invoices = yield* Invoices
      const result = yield* invoices.list({
        status: query.get("status") ?? "",
        search: query.get("search") ?? "",
        limit: page.perPage,
        offset: (page.page - 1) * page.perPage
      })
      return jsonResponse({
        data: result.invoices,
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
        "InvoiceStoreError",
        () => Effect.succeed(
          apiError(500, "internal_error", "failed to list invoices")
        )
      )
    )
  )
)

const addGetRoute = HttpRouter.get(
  "/api/v1/invoices/:id",
  protectedApiHandler(() =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "invoice not found")
      }
      const invoices = yield* Invoices
      return jsonResponse({ data: yield* invoices.getDetail(id) })
    }).pipe(
      Effect.catchTags({
        InvoiceNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "invoice not found")),
        InvoiceStoreError: () =>
          Effect.succeed(apiError(404, "not_found", "invoice not found"))
      })
    )
  )
)

const addPdfRoute = HttpRouter.get(
  "/api/v1/invoices/:id/pdf",
  protectedApiHandler(() =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "invoice not found")
      }
      const invoices = yield* Invoices
      const invoice = yield* invoices.get(id)
      const profiles = yield* CompanyProfiles
      const company = yield* profiles.get
      const body = renderInvoicePdf(invoice, company)
      return HttpServerResponse.uint8Array(body, {
        status: 200,
        headers: {
          "content-type": "application/pdf",
          "content-disposition":
            `attachment; filename="${invoice.invoice_number}.pdf"`,
          "content-length": String(body.byteLength)
        }
      })
    }).pipe(
      Effect.catchTags({
        InvoiceNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "invoice not found")),
        InvoiceStoreError: () =>
          Effect.succeed(apiError(404, "not_found", "invoice not found")),
        CompanyProfileStoreError: () =>
          Effect.succeed(
            apiError(
              500,
              "internal_error",
              "failed to load company profile"
            )
          )
      })
    )
  )
)

const createInvoice = (
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest,
  input: Effect.Effect<InvoiceInput, InvalidJsonBody>
) => {
  if (!canManage(authentication)) {
    return Effect.succeed(forbidden())
  }
  return Effect.gen(function*() {
    const values = yield* input
    const validated = validateInvoice(values)
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
    const invoices = yield* Invoices
    const created = yield* invoices.create({
      ...values,
      taxAmount: validated.taxAmount,
      lines: validated.lines
    }, authentication.user.id)
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "invoice.create",
      actor: actor(authentication),
      targetType: "invoice",
      targetId: created.id,
      targetLabel: created.invoice_number,
      metadata: {
        after: {
          contact_id: created.contact_id,
          invoice_date: created.invoice_date,
          due_date: created.due_date,
          total: Number(created.total),
          line_count: created.lines?.length ?? 0
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
      InvoiceStoreError: () =>
        Effect.succeed(
          apiError(500, "internal_error", "failed to create invoice")
        )
    })
  )
}

const addCreateRoute = HttpRouter.post(
  "/api/v1/invoices",
  protectedApiHandler((authentication, request) => {
    const key = request.headers["idempotency-key"] ?? ""
    if (key === "") {
      return createInvoice(authentication, request, parseInvoice(request))
    }
    return readBodyText(request).pipe(
      Effect.flatMap((body) =>
        runIdempotently(
          authentication,
          request,
          body,
          createInvoice(
            authentication,
            request,
            parseInvoiceText(body)
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
  "/api/v1/invoices/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "invoice not found")
      }
      const invoices = yield* Invoices
      const existing = yield* invoices.get(id)
      if (existing.status !== "draft") {
        return apiError(
          409,
          "conflict",
          `can only edit draft invoices (current: ${existing.status})`
        )
      }
      const values = yield* parseInvoice(request)
      const validated = validateInvoice(values)
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
      const result = yield* invoices.update(id, {
        ...values,
        taxAmount: validated.taxAmount,
        lines: validated.lines
      })
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "invoice.update",
        actor: actor(authentication),
        targetType: "invoice",
        targetId: id,
        targetLabel: result.after.invoice_number,
        metadata: {
          before: { total: Number(result.before.total) },
          after: { total: Number(result.after.total) }
        }
      })
      return jsonResponse({ data: result.after })
    }).pipe(
      Effect.catchTags({
        InvalidJsonBody: () =>
          Effect.succeed(
            apiError(400, "invalid_request", "invalid request body")
          ),
        InvoiceNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "invoice not found")),
        InvoiceConflict: (error) =>
          Effect.succeed(apiError(409, "conflict", error.message)),
        InvoiceStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to update invoice")
          )
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/api/v1/invoices/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "invoice not found")
      }
      const invoices = yield* Invoices
      const existing = yield* invoices.get(id)
      if (existing.status !== "draft") {
        return apiError(
          409,
          "conflict",
          `can only delete draft invoices (current: ${existing.status})`
        )
      }
      yield* invoices.remove(id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "invoice.delete",
        actor: actor(authentication),
        targetType: "invoice",
        targetId: id,
        targetLabel: existing.invoice_number,
        metadata: {
          before: {
            contact_id: existing.contact_id,
            invoice_date: existing.invoice_date,
            total: Number(existing.total)
          }
        }
      })
      return HttpServerResponse.empty({ status: 204 })
    }).pipe(
      Effect.catchTags({
        InvoiceNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "invoice not found")),
        InvoiceConflict: (error) =>
          Effect.succeed(apiError(409, "conflict", error.message)),
        InvoiceStoreError: (error) =>
          Effect.succeed(
            apiError(
              409,
              "conflict",
              error.cause instanceof Error
                ? error.cause.message
                : String(error.cause)
            )
          )
      })
    )
  })
)

const sendInvoice = (
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest,
  id: number | undefined
) => {
  if (!canManage(authentication)) {
    return Effect.succeed(forbidden())
  }
  if (id === undefined) {
    return Effect.succeed(
      apiError(404, "not_found", "invoice not found")
    )
  }
  return Effect.gen(function*() {
    const invoices = yield* Invoices
    const existing = yield* invoices.get(id)
    if (existing.status !== "draft") {
      return apiError(
        409,
        "conflict",
        `can only send draft invoices (current: ${existing.status})`
      )
    }
    const updated = yield* invoices.send(id, authentication.user.id)
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "invoice.send",
      actor: actor(authentication),
      targetType: "invoice",
      targetId: id,
      targetLabel: updated.invoice_number,
      metadata: {
        after: { status: updated.status },
        journal_id: updated.journal_id
      }
    })
    return jsonResponse({ data: updated })
  }).pipe(
    Effect.catchTags({
      InvoiceNotFound: () =>
        Effect.succeed(apiError(404, "not_found", "invoice not found")),
      InvoiceConflict: (error) =>
        Effect.succeed(apiError(409, "conflict", error.message)),
      InvoiceStoreError: () =>
        Effect.succeed(
          apiError(500, "internal_error", "failed to send invoice")
        )
    })
  )
}

const addSendRoute = HttpRouter.post(
  "/api/v1/invoices/:id/send",
  protectedApiHandler((authentication, request) =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      const operation = sendInvoice(authentication, request, id)
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
    return Effect.succeed(
      apiError(404, "not_found", "invoice not found")
    )
  }
  return Effect.gen(function*() {
    const invoices = yield* Invoices
    const existing = yield* invoices.get(id)
    const values = yield* input
    const validated = validateInvoicePayment(values)
    if (validated.amount === undefined) {
      return apiError(
        422,
        "validation_failed",
        "validation failed",
        validated.fields
      )
    }
    if (
      existing.status === "draft" ||
      existing.status === "cancelled" ||
      existing.status === "paid"
    ) {
      return apiError(
        409,
        "conflict",
        `cannot record payment for ${existing.status} invoice`
      )
    }
    const updated = yield* invoices.recordPayment(
      id,
      validated.amount,
      values.paymentDate,
      values.paymentAccount,
      authentication.user.id
    )
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "invoice.payment",
      actor: actor(authentication),
      targetType: "invoice",
      targetId: id,
      targetLabel: updated.invoice_number,
      metadata: {
        amount: validated.amount,
        payment_date: values.paymentDate,
        payment_account_id: values.paymentAccount,
        status_after: updated.status
      }
    })
    return jsonResponse({ data: updated })
  }).pipe(
    Effect.catchTags({
      InvalidJsonBody: () =>
        Effect.succeed(
          apiError(400, "invalid_request", "invalid request body")
        ),
      InvoiceNotFound: () =>
        Effect.succeed(apiError(404, "not_found", "invoice not found")),
      InvoiceConflict: (error) =>
        Effect.succeed(apiError(409, "conflict", error.message)),
      InvoiceOverpayment: (error) =>
        Effect.succeed(
          apiError(
            422,
            "validation_failed",
            error.message,
            { amount: "exceeds remaining balance" }
          )
        ),
      InvoiceStoreError: () =>
        Effect.succeed(
          apiError(500, "internal_error", "failed to record payment")
        )
    })
  )
}

const addPaymentRoute = HttpRouter.post(
  "/api/v1/invoices/:id/payment",
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

const addBulkDeleteRoute = HttpRouter.post(
  "/api/v1/invoices/bulk-delete",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const ids = yield* parseIds(request)
      if (ids.length === 0) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          { ids: "at least one id required" }
        )
      }
      const invoices = yield* Invoices
      const result = yield* invoices.bulkDelete(ids)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "invoice.bulk_delete",
        actor: actor(authentication),
        targetType: "invoice",
        metadata: result
      })
      return jsonResponse({
        data: {
          deleted: result.deleted.length,
          deleted_invoices: result.deleted,
          skipped: result.skipped
        }
      })
    }).pipe(
      Effect.catchTags({
        InvalidJsonBody: () =>
          Effect.succeed(
            apiError(400, "invalid_request", "invalid request body")
          ),
        InvoiceStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to delete invoices")
          )
      })
    )
  })
)

const addBulkSendRoute = HttpRouter.post(
  "/api/v1/invoices/bulk-send",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const ids = yield* parseIds(request)
      if (ids.length === 0) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          { ids: "at least one id required" }
        )
      }
      const invoices = yield* Invoices
      const result = yield* invoices.bulkSend(
        ids,
        authentication.user.id
      )
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "invoice.bulk_send",
        actor: actor(authentication),
        targetType: "invoice",
        metadata: result
      })
      return jsonResponse({
        data: {
          sent: result.sent.length,
          sent_invoices: result.sent,
          skipped: result.skipped,
          failed: result.failed
        }
      })
    }).pipe(
      Effect.catchTags({
        InvalidJsonBody: () =>
          Effect.succeed(
            apiError(400, "invalid_request", "invalid request body")
          ),
        InvoiceStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to send invoices")
          )
      })
    )
  })
)

const localDate = (date: Date) =>
  `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${
    String(date.getDate()).padStart(2, "0")
  }`

const generateRecurring = (
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest
) => {
  if (!canManage(authentication)) {
    return Effect.succeed(forbidden())
  }
  return Effect.gen(function*() {
    const now = new Date()
    const due = new Date(now)
    due.setDate(due.getDate() + 10)
    const invoiceDate = localDate(now)
    const dueDate = localDate(due)
    const invoices = yield* Invoices
    const result = yield* invoices.generateRecurring(
      invoiceDate,
      dueDate,
      authentication.user.id
    )
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "invoice.generate_recurring",
      actor: actor(authentication),
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
          .filter((item) => item.result === "created")
          .map((item) => item.invoice_number ?? "")
      }
    })
    return jsonResponse({ data: result })
  }).pipe(
    Effect.catchTags({
      NoDefaultRevenueAccount: (error) =>
        Effect.succeed(
          apiError(422, "validation_failed", error.message)
        ),
      InvoiceStoreError: () =>
        Effect.succeed(
          apiError(
            500,
            "internal_error",
            "failed to generate recurring invoices"
          )
        )
    })
  )
}

const addGenerateRecurringRoute = HttpRouter.post(
  "/api/v1/invoices/generate-recurring",
  protectedApiHandler((authentication, request) =>
    Effect.gen(function*() {
      const operation = generateRecurring(authentication, request)
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

export const addInvoiceApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addListRoute,
  addBulkDeleteRoute,
  addBulkSendRoute,
  addGenerateRecurringRoute,
  addPdfRoute,
  addGetRoute,
  addCreateRoute,
  addUpdateRoute,
  addDeleteRoute,
  addSendRoute,
  addPaymentRoute
)
