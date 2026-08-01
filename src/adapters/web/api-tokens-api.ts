import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import { ApiTokens } from "../../domain/auth/api-tokens.ts"
import type { Authenticated } from "../../domain/auth/authentication.ts"
import { Audit } from "../../domain/audit/audit.ts"
import { apiError, jsonResponse } from "./api-response.ts"
import { protectedApiHandler } from "./auth-api.ts"
import { runIdempotently } from "./idempotent-response.ts"
import {
  InvalidJsonBody,
  parseJsonObject,
  readBodyText,
  readJsonObject
} from "./json-body.ts"
import { requestMetadata } from "./request-metadata.ts"

type CreateTokenInput = {
  readonly name: string
  readonly scopes?: ReadonlyArray<string>
  readonly expiresAt?: string
}

const tokenFields = ["name", "scopes", "expires_at"] as const

const rfc3339 =
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/

const tokenFromObject = (
  input: Readonly<Record<string, unknown>>
): Effect.Effect<CreateTokenInput, InvalidJsonBody> =>
  Effect.try({
    try: () => {
      const nameValue = input.name
      if (
        nameValue !== undefined &&
        nameValue !== null &&
        typeof nameValue !== "string"
      ) {
        throw new Error("invalid name")
      }

      const scopesValue = input.scopes
      let scopes: ReadonlyArray<string> | undefined
      if (scopesValue !== undefined && scopesValue !== null) {
        if (
          !Array.isArray(scopesValue) ||
          !scopesValue.every((scope) => typeof scope === "string")
        ) {
          throw new Error("invalid scopes")
        }
        scopes = scopesValue
      }

      const expiresValue = input.expires_at
      let expiresAt: string | undefined
      if (expiresValue !== undefined && expiresValue !== null) {
        if (
          typeof expiresValue !== "string" ||
          !rfc3339.test(expiresValue) ||
          Number.isNaN(Date.parse(expiresValue))
        ) {
          throw new Error("invalid expires_at")
        }
        expiresAt = expiresValue
      }

      return {
        name: typeof nameValue === "string" ? nameValue : "",
        ...(scopes === undefined ? {} : { scopes }),
        ...(expiresAt === undefined ? {} : { expiresAt })
      }
    },
    catch: () => new InvalidJsonBody()
  })

const parseToken = (request: HttpServerRequest.HttpServerRequest) =>
  readJsonObject(request, tokenFields).pipe(
    Effect.flatMap(tokenFromObject)
  )

const parseTokenText = (body: string) =>
  parseJsonObject(body, tokenFields).pipe(
    Effect.flatMap(tokenFromObject)
  )

const parseId = (value: string | undefined) => {
  if (value === undefined || !/^[+-]?\d+$/.test(value)) {
    return undefined
  }
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : undefined
}

const actor = (authentication: Authenticated) => ({
  id: authentication.user.id,
  username: authentication.user.username,
  ...(authentication.method === "bearer"
    ? { tokenId: authentication.tokenId }
    : {})
})

const bearerMutationForbidden = () =>
  apiError(
    403,
    "forbidden",
    "api token cannot create or revoke api tokens"
  )

const createToken = (
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest,
  input: Effect.Effect<CreateTokenInput, InvalidJsonBody>
) => {
  if (authentication.method === "bearer") {
    return Effect.succeed(bearerMutationForbidden())
  }
  return Effect.gen(function*() {
    const values = yield* input
    const fields: Record<string, string> = {}
    const name = values.name.trim()
    if (name === "") {
      fields.name = "required"
    }
    if (values.scopes === undefined) {
      fields.scopes = "required"
    }
    if (
      values.expiresAt !== undefined &&
      Date.parse(values.expiresAt) <= Date.now()
    ) {
      fields.expires_at = "must be in the future"
    }
    if (Object.keys(fields).length > 0) {
      return apiError(
        422,
        "validation_failed",
        "validation failed",
        fields
      )
    }

    const scopes = values.scopes ?? []
    if (authentication.user.role !== "admin") {
      const capabilities = new Set(authentication.user.capabilities)
      const invalidScope = scopes.find((scope) => !capabilities.has(
        scope as typeof authentication.user.capabilities[number]
      ))
      if (invalidScope !== undefined) {
        return apiError(
          422,
          "validation_failed",
          "scope is not in your capabilities",
          {
            scopes:
              `scope ${JSON.stringify(invalidScope)} is not in your capabilities`
          }
        )
      }
    }

    const apiTokens = yield* ApiTokens
    const token = yield* apiTokens.create(
      authentication.user.id,
      name,
      scopes,
      values.expiresAt
    )
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "api_token.create",
      actor: actor(authentication),
      targetType: "api_token",
      targetId: token.id,
      targetLabel: token.name,
      metadata: {
        name: token.name,
        scopes: token.scopes,
        expires_at: token.expires_at
      }
    })
    return jsonResponse({ data: token }, 201)
  }).pipe(
    Effect.catchTags({
      InvalidJsonBody: () =>
        Effect.succeed(
          apiError(400, "invalid_request", "invalid request body")
        ),
      ApiTokenStoreError: () =>
        Effect.succeed(
          apiError(
            500,
            "internal_error",
            "failed to create api token"
          )
        )
    })
  )
}

const addCreateRoute = HttpRouter.post(
  "/api/v1/api-tokens",
  protectedApiHandler((authentication, request) => {
    const key = request.headers["idempotency-key"] ?? ""
    if (key === "") {
      return createToken(authentication, request, parseToken(request))
    }
    return readBodyText(request).pipe(
      Effect.flatMap((body) =>
        runIdempotently(
          authentication,
          request,
          body,
          createToken(
            authentication,
            request,
            parseTokenText(body)
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

const addListRoute = HttpRouter.get(
  "/api/v1/api-tokens",
  protectedApiHandler((authentication) =>
    Effect.gen(function*() {
      const apiTokens = yield* ApiTokens
      return jsonResponse({
        data: yield* apiTokens.list(authentication.user.id)
      })
    }).pipe(
      Effect.catchTag(
        "ApiTokenStoreError",
        () => Effect.succeed(
          apiError(
            500,
            "internal_error",
            "failed to list api tokens"
          )
        )
      )
    )
  )
)

const addRevokeRoute = HttpRouter.del(
  "/api/v1/api-tokens/:id",
  protectedApiHandler((authentication, request) => {
    if (authentication.method === "bearer") {
      return Effect.succeed(bearerMutationForbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(
          400,
          "invalid_request",
          "invalid token id"
        )
      }
      const apiTokens = yield* ApiTokens
      const revoked = yield* apiTokens.revoke(
        authentication.user.id,
        id
      )
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "api_token.revoke",
        actor: actor(authentication),
        targetType: "api_token",
        targetId: id,
        targetLabel: revoked.name,
        metadata: {
          token_id: id,
          name: revoked.name
        }
      })
      return HttpServerResponse.empty({ status: 204 })
    }).pipe(
      Effect.catchTags({
        ApiTokenNotFound: () =>
          Effect.succeed(
            apiError(404, "not_found", "api token not found")
          ),
        ApiTokenStoreError: () =>
          Effect.succeed(
            apiError(
              500,
              "internal_error",
              "failed to revoke api token"
            )
          )
      })
    )
  })
)

export const addApiTokenApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(addListRoute, addCreateRoute, addRevokeRoute)
