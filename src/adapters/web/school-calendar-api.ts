import { HttpRouter, HttpServerRequest } from "@effect/platform"
import { Effect } from "effect"
import {
  monthlyPriceMultiplierPercent,
  SchoolCalendar,
  validSchoolMonth,
  validateSchoolClosure
} from "../../domain/school-calendar/school-calendar.ts"
import { Audit } from "../../domain/audit/audit.ts"
import type { Authenticated } from "../../domain/auth/authentication.ts"
import { apiError, jsonResponse } from "./api-response.ts"
import { protectedApiHandler } from "./auth-api.ts"
import {
  InvalidJsonBody,
  readJsonObject
} from "./json-body.ts"
import { requestMetadata } from "./request-metadata.ts"

type ClosureInput = {
  readonly title: string
  readonly startDate: string
  readonly endDate: string
}

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

const parseClosure = (request: HttpServerRequest.HttpServerRequest) =>
  readJsonObject(request, [
    "title",
    "start_date",
    "end_date"
  ]).pipe(
    Effect.flatMap((input) =>
      Effect.try({
        try: (): ClosureInput => ({
          title: stringValue(input, "title"),
          startDate: stringValue(input, "start_date"),
          endDate: stringValue(input, "end_date")
        }),
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
  "/api/v1/school-calendar/closures",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    const query = new URL(request.url, "http://localhost").searchParams
    const month = (query.get("month") ?? "").trim()
    if (month !== "" && !validSchoolMonth(month)) {
      return Effect.succeed(apiError(
        400,
        "invalid_request",
        "invalid month",
        { month: "must be YYYY-MM" }
      ))
    }
    return Effect.gen(function*() {
      const calendar = yield* SchoolCalendar
      return jsonResponse({ data: yield* calendar.list(month) })
    }).pipe(
      Effect.catchTag(
        "SchoolCalendarStoreError",
        () => Effect.succeed(apiError(
          500,
          "internal_error",
          "failed to list school closures"
        ))
      )
    )
  })
)

const addCreateRoute = HttpRouter.post(
  "/api/v1/school-calendar/closures",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const input = yield* parseClosure(request)
      const values = {
        title: input.title.trim(),
        startDate: input.startDate.trim(),
        endDate: input.endDate.trim()
      }
      const fields = validateSchoolClosure(values)
      if (Object.keys(fields).length > 0) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          fields
        )
      }
      const calendar = yield* SchoolCalendar
      const created = yield* calendar.createManual(
        values.title,
        values.startDate,
        values.endDate
      )
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "school_closure.create",
        actor: actor(authentication),
        targetType: "school_closure",
        targetId: created.id,
        targetLabel: created.title,
        metadata: {
          after: {
            source: created.source,
            title: created.title,
            start_date: created.start_date,
            end_date: created.end_date
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
        SchoolCalendarStoreError: () =>
          Effect.succeed(apiError(
            500,
            "internal_error",
            "failed to create school closure"
          ))
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/api/v1/school-calendar/closures/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(
          404,
          "not_found",
          "school closure not found"
        )
      }
      const calendar = yield* SchoolCalendar
      const existing = yield* calendar.remove(id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "school_closure.delete",
        actor: actor(authentication),
        targetType: "school_closure",
        targetId: id,
        targetLabel: existing.title,
        metadata: {
          before: {
            source: existing.source,
            title: existing.title,
            start_date: existing.start_date,
            end_date: existing.end_date
          }
        }
      })
      return jsonResponse({ data: { deleted: true } })
    }).pipe(
      Effect.catchTags({
        SchoolClosureNotFound: () =>
          Effect.succeed(apiError(
            404,
            "not_found",
            "school closure not found"
          )),
        SchoolCalendarStoreError: () =>
          Effect.succeed(apiError(
            500,
            "internal_error",
            "failed to delete school closure"
          ))
      })
    )
  })
)

const addEffectiveDaysRoute = HttpRouter.get(
  "/api/v1/school-calendar/effective-days",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    const query = new URL(request.url, "http://localhost").searchParams
    const month = (query.get("month") ?? "").trim()
    if (!validSchoolMonth(month)) {
      return Effect.succeed(apiError(
        400,
        "invalid_request",
        "invalid month",
        { month: "must be YYYY-MM" }
      ))
    }
    return Effect.gen(function*() {
      const calendar = yield* SchoolCalendar
      const effectiveDays = yield* calendar.effectiveDays(month)
      return jsonResponse({
        data: {
          month,
          effective_days: effectiveDays,
          multiplier_percent:
            monthlyPriceMultiplierPercent(effectiveDays)
        }
      })
    }).pipe(
      Effect.catchTag(
        "SchoolCalendarStoreError",
        () => Effect.succeed(apiError(
          500,
          "internal_error",
          "failed to calculate effective school days"
        ))
      )
    )
  })
)

const addSyncRoute = HttpRouter.post(
  "/api/v1/integrations/google-calendar/sync",
  protectedApiHandler((authentication, request) => {
    if (authentication.user.role !== "admin") {
      return Effect.succeed(
        apiError(403, "forbidden", "admin user required")
      )
    }
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const calendar = yield* SchoolCalendar
      const result = yield* calendar.sync
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "google_calendar.sync",
        actor: actor(authentication),
        targetType: "google_calendar_connection",
        targetId: 1,
        metadata: {
          fetched: result.fetched,
          stored: result.stored,
          window_start: result.window_start,
          window_end: result.window_end
        }
      })
      return jsonResponse({ data: result })
    }).pipe(
      Effect.catchTags({
        GoogleCalendarError: (error) =>
          Effect.succeed(
            apiError(400, "invalid_request", error.message)
          ),
        SchoolCalendarStoreError: (error) =>
          Effect.succeed(apiError(
            400,
            "invalid_request",
            error.cause instanceof Error
              ? error.cause.message
              : String(error.cause)
          ))
      })
    )
  })
)

export const addSchoolCalendarApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addListRoute,
  addCreateRoute,
  addDeleteRoute,
  addEffectiveDaysRoute,
  addSyncRoute
)
