import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import {
  AccountConflict,
  AccountNotFound,
  Accounts,
  AccountStoreError,
  validateAccount
} from "../../domain/accounting/accounts.ts"
import { Audit, auditDiff } from "../../domain/audit/audit.ts"
import type { Authenticated } from "../../domain/auth/authentication.ts"
import { apiError, jsonResponse } from "./api-response.ts"
import { protectedApiHandler } from "./auth-api.ts"
import {
  InvalidJsonBody,
  readJsonObject
} from "./json-body.ts"
import { paginate, parsePage } from "./pagination.ts"
import { requestMetadata } from "./request-metadata.ts"

type AccountInput = {
  readonly code: string
  readonly name: string
  readonly accountType: string
  readonly normalBalance: string
  readonly description: string
  readonly isActive: boolean | undefined
  readonly isCash: boolean | undefined
}

const parseAccountInput = (
  request: HttpServerRequest.HttpServerRequest
): Effect.Effect<AccountInput, InvalidJsonBody> =>
  readJsonObject(request, [
    "code",
    "name",
    "account_type",
    "normal_balance",
    "description",
    "is_active",
    "is_cash"
  ]).pipe(
    Effect.flatMap((input) =>
      Effect.try({
        try: () => {
          for (const field of [
            "code",
            "name",
            "account_type",
            "normal_balance",
            "description"
          ]) {
            const value = input[field]
            if (
              value !== undefined &&
              value !== null &&
              typeof value !== "string"
            ) {
              throw new Error(`invalid ${field}`)
            }
          }
          for (const field of ["is_active", "is_cash"]) {
            const value = input[field]
            if (
              value !== undefined &&
              value !== null &&
              typeof value !== "boolean"
            ) {
              throw new Error(`invalid ${field}`)
            }
          }
          return {
            code: typeof input.code === "string" ? input.code : "",
            name: typeof input.name === "string" ? input.name : "",
            accountType: typeof input.account_type === "string"
              ? input.account_type
              : "",
            normalBalance: typeof input.normal_balance === "string"
              ? input.normal_balance
              : "",
            description: typeof input.description === "string"
              ? input.description
              : "",
            isActive: typeof input.is_active === "boolean"
              ? input.is_active
              : undefined,
            isCash: typeof input.is_cash === "boolean"
              ? input.is_cash
              : undefined
          }
        },
        catch: () => new InvalidJsonBody()
      })
    )
  )

const parseId = (value: string | undefined) =>
  value !== undefined && /^[+-]?\d+$/.test(value)
    ? Number(value)
    : undefined

const canManage = (authentication: Authenticated) =>
  authentication.effectiveCapabilities.includes("accounts.manage")

const actor = (authentication: Authenticated) => ({
  id: authentication.user.id,
  username: authentication.user.username,
  ...(authentication.method === "bearer"
    ? { tokenId: authentication.tokenId }
    : {})
})

const addListRoute = HttpRouter.get(
  "/api/v1/accounts",
  protectedApiHandler((_authentication, request) =>
    Effect.gen(function*() {
      const query = new URL(request.url, "http://localhost").searchParams
      const accounts = yield* Accounts
      const values = yield* accounts.list({
        type: query.get("type") ?? "",
        search: query.get("search") ?? ""
      })
      return jsonResponse(paginate(values, parsePage(request)))
    }).pipe(
      Effect.catchTag(
        "AccountStoreError",
        () => Effect.succeed(
          apiError(500, "internal_error", "failed to list accounts")
        )
      )
    )
  )
)

const addGetRoute = HttpRouter.get(
  "/api/v1/accounts/:id",
  protectedApiHandler(() =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(400, "invalid_request", "invalid account id")
      }
      const accounts = yield* Accounts
      const account = yield* accounts.get(id)
      return jsonResponse({ data: account })
    }).pipe(
      Effect.catchTags({
        AccountNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "account not found")),
        AccountStoreError: () =>
          Effect.succeed(apiError(404, "not_found", "account not found"))
      })
    )
  )
)

const addCreateRoute = HttpRouter.post(
  "/api/v1/accounts",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(
        apiError(403, "forbidden", "insufficient permissions")
      )
    }
    return Effect.gen(function*() {
      const input = yield* parseAccountInput(request)
      const fields = validateAccount(input)
      if (Object.keys(fields).length > 0) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          fields
        )
      }
      const accounts = yield* Accounts
      const created = yield* accounts.create({
        code: input.code,
        name: input.name,
        accountType: input.accountType,
        normalBalance: input.normalBalance,
        isActive: input.isActive ?? true,
        isCash: input.isCash ?? false,
        description: input.description
      })
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "account.create",
        actor: actor(authentication),
        targetType: "account",
        targetId: created.id,
        targetLabel: created.code,
        metadata: {
          after: {
            code: created.code,
            name: created.name,
            account_type: created.account_type,
            normal_balance: created.normal_balance,
            is_active: created.is_active,
            is_cash: created.is_cash
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
        AccountConflict: () =>
          Effect.succeed(
            apiError(409, "conflict", "account code already exists")
          ),
        AccountStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to create account")
          )
      })
    )
  })
)

const addUpdateRoute = HttpRouter.put(
  "/api/v1/accounts/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(
        apiError(403, "forbidden", "insufficient permissions")
      )
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(400, "invalid_request", "invalid account id")
      }
      const accounts = yield* Accounts
      const existing = yield* accounts.get(id)
      const input = yield* parseAccountInput(request)
      const fields = validateAccount(input)
      if (Object.keys(fields).length > 0) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          fields
        )
      }
      const isCash = input.isCash ?? existing.is_cash
      if (
        isCash &&
        (input.accountType !== "asset" || input.normalBalance !== "debit")
      ) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          { is_cash: "cash accounts must be debit-normal assets" }
        )
      }
      const updated = yield* accounts.update(id, {
        code: input.code,
        name: input.name,
        accountType: input.accountType,
        normalBalance: input.normalBalance,
        isActive: input.isActive ?? existing.is_active,
        isCash,
        description: input.description
      })
      const metadata = auditDiff(
        {
          code: existing.code,
          name: existing.name,
          account_type: existing.account_type,
          normal_balance: existing.normal_balance,
          description: existing.description,
          is_active: existing.is_active,
          is_cash: existing.is_cash
        },
        {
          code: updated.code,
          name: updated.name,
          account_type: updated.account_type,
          normal_balance: updated.normal_balance,
          description: updated.description,
          is_active: updated.is_active,
          is_cash: updated.is_cash
        },
        [
          "code",
          "name",
          "account_type",
          "normal_balance",
          "description",
          "is_active",
          "is_cash"
        ]
      )
      if (metadata !== undefined) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "account.update",
          actor: actor(authentication),
          targetType: "account",
          targetId: id,
          targetLabel: existing.code,
          metadata
        })
      }
      return jsonResponse({ data: updated })
    }).pipe(
      Effect.catchTags({
        InvalidJsonBody: () =>
          Effect.succeed(
            apiError(400, "invalid_request", "invalid request body")
          ),
        AccountNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "account not found")),
        AccountStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to update account")
          )
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/api/v1/accounts/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(
        apiError(403, "forbidden", "insufficient permissions")
      )
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(400, "invalid_request", "invalid account id")
      }
      const accounts = yield* Accounts
      const removed = yield* accounts.remove(id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "account.delete",
        actor: actor(authentication),
        targetType: "account",
        targetId: id,
        targetLabel: removed.code,
        metadata: {
          before: {
            code: removed.code,
            name: removed.name,
            account_type: removed.account_type,
            normal_balance: removed.normal_balance,
            is_cash: removed.is_cash
          }
        }
      })
      return HttpServerResponse.empty({ status: 204 })
    }).pipe(
      Effect.catchTags({
        AccountNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "account not found")),
        AccountConflict: (error) =>
          Effect.succeed(
            error.reason === "system"
              ? apiError(
                409,
                "conflict",
                "cannot delete system account"
              )
              : apiError(
                409,
                "conflict",
                "account has linked transactions and cannot be deleted"
              )
          ),
        AccountStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to delete account")
          )
      })
    )
  })
)

export const addAccountApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addListRoute,
  addGetRoute,
  addCreateRoute,
  addUpdateRoute,
  addDeleteRoute
)
