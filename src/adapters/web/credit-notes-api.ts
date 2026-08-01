import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import {
  CreditNotes,
  validateCreditNote,
  type CreditNoteInputLine
} from "../../domain/accounting/credit-notes.ts"
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

type CreditNoteInput = {
  readonly contactId: number
  readonly invoiceId?: number
  readonly cnDate: string
  readonly reason: string
  readonly taxAmount: string
  readonly notes: string
  readonly lines: ReadonlyArray<CreditNoteInputLine>
}

const creditNoteFields = [
  "contact_id",
  "invoice_id",
  "cn_date",
  "reason",
  "tax_amount",
  "notes",
  "lines"
] as const

const lineFields = [
  "description",
  "quantity",
  "unit_price",
  "account_id"
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

const optionalIntegerValue = (
  input: Readonly<Record<string, unknown>>,
  field: string
) => {
  const value = input[field]
  if (value === undefined || value === null) {
    return undefined
  }
  if (typeof value !== "number" || !Number.isSafeInteger(value)) {
    throw new Error(`invalid ${field}`)
  }
  return value
}

const creditNoteFromObject = (
  input: Readonly<Record<string, unknown>>
): Effect.Effect<CreditNoteInput, InvalidJsonBody> =>
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
          throw new Error("invalid credit note line")
        }
        const line = rawLine as Readonly<Record<string, unknown>>
        if (
          Object.keys(line).some((key) => !lineFields.includes(
            key as typeof lineFields[number]
          ))
        ) {
          throw new Error("unknown credit note line field")
        }
        return {
          description: stringValue(line, "description"),
          quantity: stringValue(line, "quantity"),
          unitPrice: stringValue(line, "unit_price"),
          accountId: integerValue(line, "account_id")
        }
      })
      const invoiceId = optionalIntegerValue(input, "invoice_id")
      return {
        contactId: integerValue(input, "contact_id"),
        ...(invoiceId === undefined ? {} : { invoiceId }),
        cnDate: stringValue(input, "cn_date"),
        reason: stringValue(input, "reason"),
        taxAmount: stringValue(input, "tax_amount"),
        notes: stringValue(input, "notes"),
        lines
      }
    },
    catch: () => new InvalidJsonBody()
  })

const parseCreditNote = (
  request: HttpServerRequest.HttpServerRequest
) => readJsonObject(request, creditNoteFields).pipe(
  Effect.flatMap(creditNoteFromObject)
)

const parseCreditNoteText = (body: string) =>
  parseJsonObject(body, creditNoteFields).pipe(
    Effect.flatMap(creditNoteFromObject)
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

const storeMessage = (error: { readonly cause: unknown }) =>
  error.cause instanceof Error
    ? error.cause.message
    : String(error.cause)

const addListRoute = HttpRouter.get(
  "/api/v1/credit-notes",
  protectedApiHandler((_authentication, request) =>
    Effect.gen(function*() {
      const query = new URL(request.url, "http://localhost").searchParams
      const page = parsePage(request)
      const creditNotes = yield* CreditNotes
      const result = yield* creditNotes.list({
        status: query.get("status") ?? "",
        search: query.get("search") ?? "",
        limit: page.perPage,
        offset: (page.page - 1) * page.perPage
      })
      return jsonResponse({
        data: result.creditNotes,
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
        "CreditNoteStoreError",
        () => Effect.succeed(
          apiError(
            500,
            "internal_error",
            "failed to list credit notes"
          )
        )
      )
    )
  )
)

const addGetRoute = HttpRouter.get(
  "/api/v1/credit-notes/:id",
  protectedApiHandler(() =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "credit note not found")
      }
      const creditNotes = yield* CreditNotes
      return jsonResponse(yield* creditNotes.get(id))
    }).pipe(
      Effect.catchTags({
        CreditNoteNotFound: () =>
          Effect.succeed(
            apiError(404, "not_found", "credit note not found")
          ),
        CreditNoteStoreError: () =>
          Effect.succeed(
            apiError(404, "not_found", "credit note not found")
          )
      })
    )
  )
)

const createCreditNote = (
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest,
  input: Effect.Effect<CreditNoteInput, InvalidJsonBody>
) => {
  if (!canManage(authentication)) {
    return Effect.succeed(forbidden())
  }
  return Effect.gen(function*() {
    const values = yield* input
    const validated = validateCreditNote(values)
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
    const creditNotes = yield* CreditNotes
    const created = yield* creditNotes.create({
      ...values,
      taxAmount: validated.taxAmount,
      lines: validated.lines
    }, authentication.user.id)
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "credit_note.create",
      actor: actor(authentication),
      targetType: "credit_note",
      targetId: created.id,
      targetLabel: created.cn_number,
      metadata: {
        after: {
          contact_id: created.contact_id,
          invoice_id: created.invoice_id ?? null,
          cn_date: created.cn_date,
          reason: created.reason,
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
      CreditNoteStoreError: (error) =>
        Effect.succeed(
          apiError(422, "validation_failed", storeMessage(error))
        )
    })
  )
}

const addCreateRoute = HttpRouter.post(
  "/api/v1/credit-notes",
  protectedApiHandler((authentication, request) => {
    const key = request.headers["idempotency-key"] ?? ""
    if (key === "") {
      return createCreditNote(
        authentication,
        request,
        parseCreditNote(request)
      )
    }
    return readBodyText(request).pipe(
      Effect.flatMap((body) =>
        runIdempotently(
          authentication,
          request,
          body,
          createCreditNote(
            authentication,
            request,
            parseCreditNoteText(body)
          )
        )
      ),
      Effect.catchTag(
        "InvalidJsonBody",
        () => Effect.succeed(
          apiError(
            400,
            "invalid_request",
            "failed to read request body"
          )
        )
      )
    )
  })
)

const addUpdateRoute = HttpRouter.put(
  "/api/v1/credit-notes/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "credit note not found")
      }
      const creditNotes = yield* CreditNotes
      const existing = yield* creditNotes.get(id)
      if (existing.status !== "draft") {
        return apiError(
          409,
          "conflict",
          `can only edit draft credit notes (current: ${existing.status})`
        )
      }
      const values = yield* parseCreditNote(request)
      const validated = validateCreditNote(values)
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
      const result = yield* creditNotes.update(id, {
        ...values,
        taxAmount: validated.taxAmount,
        lines: validated.lines
      })
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "credit_note.update",
        actor: actor(authentication),
        targetType: "credit_note",
        targetId: id,
        targetLabel: result.after.cn_number,
        metadata: {
          before: {
            contact_id: result.before.contact_id,
            reason: result.before.reason,
            total: result.before.total
          },
          after: {
            contact_id: result.after.contact_id,
            reason: result.after.reason,
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
        CreditNoteNotFound: () =>
          Effect.succeed(
            apiError(404, "not_found", "credit note not found")
          ),
        CreditNoteConflict: (error) =>
          Effect.succeed(apiError(409, "conflict", error.message)),
        CreditNoteStoreError: (error) =>
          Effect.succeed(
            apiError(422, "validation_failed", storeMessage(error))
          )
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/api/v1/credit-notes/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "credit note not found")
      }
      const creditNotes = yield* CreditNotes
      const existing = yield* creditNotes.get(id)
      yield* creditNotes.remove(id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "credit_note.delete",
        actor: actor(authentication),
        targetType: "credit_note",
        targetId: id,
        targetLabel: existing.cn_number,
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
        CreditNoteNotFound: () =>
          Effect.succeed(
            apiError(404, "not_found", "credit note not found")
          ),
        CreditNoteConflict: (error) =>
          Effect.succeed(apiError(409, "conflict", error.message)),
        CreditNoteStoreError: (error) =>
          Effect.succeed(
            apiError(409, "conflict", storeMessage(error))
          )
      })
    )
  })
)

const lifecycleOperation = (
  action: "issue" | "void",
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest,
  id: number | undefined
) => {
  if (!canManage(authentication)) {
    return Effect.succeed(forbidden())
  }
  if (id === undefined) {
    return Effect.succeed(
      apiError(404, "not_found", "credit note not found")
    )
  }
  return Effect.gen(function*() {
    const creditNotes = yield* CreditNotes
    yield* creditNotes.get(id)
    const updated = action === "issue"
      ? yield* creditNotes.issue(id, authentication.user.id)
      : yield* creditNotes.void(id, authentication.user.id)
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: `credit_note.${action}`,
      actor: actor(authentication),
      targetType: "credit_note",
      targetId: id,
      targetLabel: updated.cn_number,
      metadata: action === "issue"
        ? {
          after: { status: updated.status },
          journal_id: updated.journal_id,
          invoice_id: updated.invoice_id
        }
        : {
          after: { status: updated.status },
          invoice_id: updated.invoice_id
        }
    })
    return jsonResponse(updated)
  }).pipe(
    Effect.catchTags({
      CreditNoteNotFound: () =>
        Effect.succeed(
          apiError(404, "not_found", "credit note not found")
        ),
      CreditNoteConflict: (error) =>
        Effect.succeed(apiError(409, "conflict", error.message)),
      CreditNoteStoreError: (error) =>
        Effect.succeed(
          apiError(409, "conflict", storeMessage(error))
        )
    })
  )
}

const lifecycleRoute = (action: "issue" | "void") =>
  HttpRouter.post(
    `/api/v1/credit-notes/:id/${action}`,
    protectedApiHandler((authentication, request) =>
      Effect.gen(function*() {
        const params = yield* HttpRouter.params
        const operation = lifecycleOperation(
          action,
          authentication,
          request,
          parseId(params.id)
        )
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
            apiError(
              400,
              "invalid_request",
              "failed to read request body"
            )
          )
        )
      )
    )
  )

const addIssueRoute = lifecycleRoute("issue")
const addVoidRoute = lifecycleRoute("void")

export const addCreditNoteApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addListRoute,
  addGetRoute,
  addCreateRoute,
  addUpdateRoute,
  addDeleteRoute,
  addIssueRoute,
  addVoidRoute
)
