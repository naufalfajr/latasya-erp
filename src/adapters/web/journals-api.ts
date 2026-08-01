import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import {
  JournalConflict,
  JournalNotFound,
  Journals,
  JournalStoreError,
  JournalValidationError,
  validateJournal,
  type JournalInputLine
} from "../../domain/accounting/journals.ts"
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

type JournalInput = {
  readonly entryDate: string
  readonly description: string
  readonly lines: ReadonlyArray<JournalInputLine>
}

const stringField = (
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

const journalInputFromObject = (
  input: Readonly<Record<string, unknown>>
): Effect.Effect<JournalInput, InvalidJsonBody> =>
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
          throw new Error("invalid journal line")
        }
        const line = rawLine as Readonly<Record<string, unknown>>
        if (
          Object.keys(line).some((key) =>
            !["account_id", "debit", "credit", "memo"].includes(key)
          )
        ) {
          throw new Error("unknown journal line field")
        }
        const rawAccountId = line.account_id
        const accountId = rawAccountId === undefined || rawAccountId === null
          ? 0
          : rawAccountId
        if (
          typeof accountId !== "number" ||
          !Number.isSafeInteger(accountId)
        ) {
          throw new Error("invalid account_id")
        }
        return {
          accountId,
          debit: stringField(line, "debit"),
          credit: stringField(line, "credit"),
          memo: stringField(line, "memo")
        }
      })
      return {
        entryDate: stringField(input, "entry_date"),
        description: stringField(input, "description"),
        lines
      }
    },
    catch: () => new InvalidJsonBody()
  })

const parseJournalInput = (
  request: HttpServerRequest.HttpServerRequest
) =>
  readJsonObject(request, ["entry_date", "description", "lines"]).pipe(
    Effect.flatMap(journalInputFromObject)
  )

const parseJournalText = (body: string) =>
  parseJsonObject(body, ["entry_date", "description", "lines"]).pipe(
    Effect.flatMap(journalInputFromObject)
  )

const parseId = (value: string | undefined) => {
  if (value === undefined || !/^[+-]?\d+$/.test(value)) {
    return undefined
  }
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : undefined
}

const canManage = (authentication: Authenticated) =>
  authentication.effectiveCapabilities.includes("journals.manage")

const actor = (authentication: Authenticated) => ({
  id: authentication.user.id,
  username: authentication.user.username,
  ...(authentication.method === "bearer"
    ? { tokenId: authentication.tokenId }
    : {})
})

const errorMessage = (error: JournalStoreError) => {
  const cause = error.cause
  return cause instanceof Error ? cause.message : String(cause)
}

const addListRoute = HttpRouter.get(
  "/api/v1/journals",
  protectedApiHandler((_authentication, request) =>
    Effect.gen(function*() {
      const query = new URL(request.url, "http://localhost").searchParams
      const page = parsePage(request)
      const journals = yield* Journals
      const result = yield* journals.list({
        dateFrom: query.get("from") ?? "",
        dateTo: query.get("to") ?? "",
        sourceType: query.get("source") ?? "",
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
          apiError(500, "internal_error", "failed to list journals")
        )
      )
    )
  )
)

const addGetRoute = HttpRouter.get(
  "/api/v1/journals/:id",
  protectedApiHandler(() =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "journal entry not found")
      }
      const journals = yield* Journals
      const entry = yield* journals.get(id)
      return jsonResponse({ data: entry })
    }).pipe(
      Effect.catchTags({
        JournalNotFound: () =>
          Effect.succeed(
            apiError(404, "not_found", "journal entry not found")
          ),
        JournalStoreError: () =>
          Effect.succeed(
            apiError(404, "not_found", "journal entry not found")
          )
      })
    )
  )
)

const createJournal = (
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest,
  input: Effect.Effect<JournalInput, InvalidJsonBody>
) => {
  if (!canManage(authentication)) {
    return Effect.succeed(
      apiError(
        403,
        "forbidden",
        "journals.manage capability required"
      )
    )
  }
  return Effect.gen(function*() {
    const values = yield* input
    const validated = validateJournal(values)
    if (validated.lines === undefined) {
      return apiError(
        422,
        "validation_failed",
        "validation failed",
        validated.fields
      )
    }
    const journals = yield* Journals
    const created = yield* journals.create({
      entryDate: values.entryDate,
      description: values.description,
      sourceType: "manual",
      isPosted: true,
      createdBy: authentication.user.id,
      lines: validated.lines
    })
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "journal.create",
      actor: actor(authentication),
      targetType: "journal_entry",
      targetId: created.id,
      targetLabel: created.reference,
      metadata: {
        after: {
          reference: created.reference,
          entry_date: created.entry_date,
          description: created.description,
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
      JournalValidationError: (error) =>
        Effect.succeed(
          apiError(422, "validation_failed", error.message)
        ),
      JournalStoreError: (error) =>
        Effect.succeed(
          apiError(422, "validation_failed", errorMessage(error))
        )
    })
  )
}

const addCreateRoute = HttpRouter.post(
  "/api/v1/journals",
  protectedApiHandler((authentication, request) => {
    const key = request.headers["idempotency-key"] ?? ""
    if (key === "") {
      return createJournal(
        authentication,
        request,
        parseJournalInput(request)
      )
    }
    return readBodyText(request).pipe(
      Effect.flatMap((body) =>
        runIdempotently(
          authentication,
          request,
          body,
          createJournal(
            authentication,
            request,
            parseJournalText(body)
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
  "/api/v1/journals/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(
        apiError(
          403,
          "forbidden",
          "journals.manage capability required"
        )
      )
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "journal entry not found")
      }
      const journals = yield* Journals
      const existing = yield* journals.get(id)
      if (
        existing.source_type !== "" &&
        existing.source_type !== "manual"
      ) {
        return apiError(
          409,
          "conflict",
          "cannot edit auto-generated journal entry"
        )
      }
      const values = yield* parseJournalInput(request)
      const validated = validateJournal(values)
      if (validated.lines === undefined) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          validated.fields
        )
      }
      const updated = yield* journals.updateManual(
        id,
        values.entryDate,
        values.description,
        validated.lines
      )
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "journal.update",
        actor: actor(authentication),
        targetType: "journal_entry",
        targetId: id,
        targetLabel: updated.reference,
        metadata: {
          before: {
            entry_date: existing.entry_date,
            description: existing.description
          },
          after: {
            entry_date: updated.entry_date,
            description: updated.description
          }
        }
      })
      return jsonResponse({ data: updated })
    }).pipe(
      Effect.catchTags({
        InvalidJsonBody: () =>
          Effect.succeed(
            apiError(400, "invalid_request", "invalid request body")
          ),
        JournalNotFound: () =>
          Effect.succeed(
            apiError(404, "not_found", "journal entry not found")
          ),
        JournalConflict: (error) =>
          Effect.succeed(apiError(409, "conflict", error.message)),
        JournalValidationError: (error) =>
          Effect.succeed(
            apiError(422, "validation_failed", error.message)
          ),
        JournalStoreError: (error) =>
          Effect.succeed(
            apiError(422, "validation_failed", errorMessage(error))
          )
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/api/v1/journals/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(
        apiError(
          403,
          "forbidden",
          "journals.manage capability required"
        )
      )
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "journal entry not found")
      }
      const journals = yield* Journals
      const existing = yield* journals.get(id)
      yield* journals.removeManual(id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "journal.delete",
        actor: actor(authentication),
        targetType: "journal_entry",
        targetId: id,
        targetLabel: existing.reference,
        metadata: {
          before: {
            reference: existing.reference,
            entry_date: existing.entry_date,
            description: existing.description
          }
        }
      })
      return HttpServerResponse.empty({ status: 204 })
    }).pipe(
      Effect.catchTags({
        JournalNotFound: () =>
          Effect.succeed(
            apiError(404, "not_found", "journal entry not found")
          ),
        JournalConflict: (error) =>
          Effect.succeed(apiError(409, "conflict", error.message)),
        JournalStoreError: (error) =>
          Effect.succeed(
            apiError(409, "conflict", errorMessage(error))
          )
      })
    )
  })
)

export const addJournalApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addListRoute,
  addGetRoute,
  addCreateRoute,
  addUpdateRoute,
  addDeleteRoute
)
