import { HttpRouter } from "@effect/platform"
import { Effect } from "effect"
import { AuditLog } from "../../domain/audit/audit-log.ts"
import { apiError, jsonResponse } from "./api-response.ts"
import { protectedApiHandler } from "./auth-api.ts"
import { parsePage } from "./pagination.ts"

const parseDate = (
  value: string,
  endOfDay: boolean
): string | undefined => {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (match === null) {
    return undefined
  }
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const date = new Date(Date.UTC(
    year,
    month - 1,
    day,
    endOfDay ? 23 : 0,
    endOfDay ? 59 : 0,
    endOfDay ? 59 : 0,
    endOfDay ? 999 : 0
  ))
  if (
    date.getUTCFullYear() !== year ||
    date.getUTCMonth() !== month - 1 ||
    date.getUTCDate() !== day
  ) {
    return undefined
  }
  return date.toISOString()
}

const addListRoute = HttpRouter.get(
  "/api/v1/audit",
  protectedApiHandler((authentication, request) => {
    if (
      !authentication.effectiveCapabilities.includes("audit.view")
    ) {
      return Effect.succeed(
        apiError(403, "forbidden", "insufficient permissions")
      )
    }
    return Effect.gen(function*() {
      const query = new URL(request.url, "http://localhost").searchParams
      const page = parsePage(request)
      const from = parseDate((query.get("from") ?? "").trim(), false)
      const to = parseDate((query.get("to") ?? "").trim(), true)
      const auditLog = yield* AuditLog
      const result = yield* auditLog.list({
        actorUsername: (query.get("actor") ?? "").trim(),
        actionPrefix: (query.get("action") ?? "").trim(),
        ...(from === undefined ? {} : { from }),
        ...(to === undefined ? {} : { to }),
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
        "AuditLogStoreError",
        () => Effect.succeed(
          apiError(
            500,
            "internal_error",
            "failed to list audit log"
          )
        )
      )
    )
  })
)

export const addAuditApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(addListRoute)
