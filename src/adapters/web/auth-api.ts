import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import { Audit } from "../../domain/audit/audit.ts"
import {
  Authentication,
  AuthenticationStoreError,
  type Authenticated,
  type AuthUser,
  InvalidCredentials,
  InvalidSession,
  InvalidToken,
  PasswordValidationFailed
} from "../../domain/auth/authentication.ts"
import {
  RateLimiter,
  retryAfterSeconds
} from "../../domain/security/rate-limiter.ts"
import { apiError, jsonResponse } from "./api-response.ts"
import { InvalidJsonBody, readStringRecord } from "./json-body.ts"
import {
  requestMetadata,
  unspoofableClientIp
} from "./request-metadata.ts"

const sessionCookie = (
  sessionId: string,
  development: boolean
) => [
  `session_id=${sessionId}`,
  "Path=/",
  "Max-Age=172800",
  "HttpOnly",
  ...(development ? [] : ["Secure"]),
  "SameSite=Lax"
].join("; ")

const clearedSessionCookie = "session_id=; Path=/; Max-Age=0"

const publicUser = (user: AuthUser) => ({
  id: user.id,
  username: user.username,
  full_name: user.fullName,
  role: user.role,
  capabilities: user.capabilities,
  must_change_password: user.mustChangePassword
})

const redirectToLogin = (clearCookie: boolean) =>
  HttpServerResponse.text('<a href="/login">See Other</a>.\n\n', {
    status: 303,
    headers: {
      "content-type": "text/html; charset=utf-8",
      location: "/login",
      ...(clearCookie ? { "set-cookie": clearedSessionCookie } : {})
    }
  })

const authenticateRequest = (
  request: HttpServerRequest.HttpServerRequest
): Effect.Effect<
  Authenticated,
  HttpServerResponse.HttpServerResponse,
  Authentication
> => Effect.gen(function*() {
  const authentication = yield* Authentication
  const authorization = request.headers.authorization ?? ""
  if (authorization.startsWith("Bearer ")) {
    return yield* authentication.authenticateBearer(
      authorization.slice("Bearer ".length)
    ).pipe(
      Effect.mapError((error) => {
        if (error instanceof InvalidToken) {
          const message = error.reason === "invalid_or_expired"
            ? "invalid or expired token"
            : "token user not found or inactive"
          return apiError(401, "invalid_token", message)
        }
        return apiError(500, "internal_error", "internal server error")
      })
    )
  }

  const sessionId = request.cookies.session_id
  if (sessionId === undefined) {
    return yield* Effect.fail(
      apiError(401, "unauthorized", "authentication required")
    )
  }
  return yield* authentication.authenticateSession(sessionId).pipe(
    Effect.mapError((error) =>
      error instanceof InvalidSession
        ? redirectToLogin(error.reason === "invalid_or_expired")
        : redirectToLogin(false)
    )
  )
})

export const protectedApiHandler = <R>(
  handler: (
    authentication: Authenticated,
    request: HttpServerRequest.HttpServerRequest
  ) => Effect.Effect<
    HttpServerResponse.HttpServerResponse,
    never,
    R
  >
) => Effect.gen(function*() {
  const request = yield* HttpServerRequest.HttpServerRequest
  const authenticated = yield* authenticateRequest(request)
  return yield* handler(authenticated, request)
}).pipe(
  Effect.catchAll(Effect.succeed)
)

const withLoginRateLimit = <E, R>(
  handler: Effect.Effect<HttpServerResponse.HttpServerResponse, E, R>
) => Effect.gen(function*() {
  const request = yield* HttpServerRequest.HttpServerRequest
  const limiter = yield* RateLimiter
  const key = `${unspoofableClientIp(request)}:unknown`
  if (!(yield* limiter.take("login", key))) {
    return HttpServerResponse.setHeader(
      apiError(
        429,
        "rate_limited",
        "too many login attempts, please try again later"
      ),
      "retry-after",
      String(retryAfterSeconds("login"))
    )
  }

  const response = yield* handler
  if (response.status >= 200 && response.status < 300) {
    yield* limiter.refund("login", key)
  }
  return response
})

const addLoginRoute = (development: boolean) =>
  HttpRouter.post(
    "/api/v1/auth/login",
    withLoginRateLimit(Effect.gen(function*() {
      const request = yield* HttpServerRequest.HttpServerRequest
      const metadata = requestMetadata(request)
      return yield* Effect.gen(function*() {
        const input = yield* readStringRecord(request, [
          "username",
          "password"
        ])
        const fields: Record<string, string> = {}
        if (input.username === "") {
          fields.username = "required"
        }
        if (input.password === "") {
          fields.password = "required"
        }
        if (Object.keys(fields).length > 0) {
          return apiError(
            422,
            "validation_failed",
            "username and password are required",
            fields,
            metadata.requestId
          )
        }

        const authentication = yield* Authentication
        const audit = yield* Audit
        const username = input.username ?? ""
        const session = yield* authentication.login(
          username,
          input.password ?? ""
        ).pipe(
          Effect.tapError((error) =>
            error instanceof InvalidCredentials
              ? audit.log(metadata, {
                action: "auth.login_failed",
                actor: {
                  ...(error.userId === undefined ? {} : { id: error.userId }),
                  username
                },
                result: "fail",
                metadata: { reason: error.reason }
              })
              : Effect.void
          )
        )
        yield* audit.log(metadata, {
          action: "auth.login",
          actor: {
            id: session.user.id,
            username: session.user.username
          },
          targetType: "user",
          targetId: session.user.id,
          targetLabel: session.user.username
        })
        return jsonResponse({
          data: {
            user: publicUser(session.user),
            csrf_token: session.csrfToken
          }
        }, 200, {
          "set-cookie": sessionCookie(session.sessionId, development)
        })
      }).pipe(
        Effect.catchTags({
          InvalidJsonBody: () =>
            Effect.succeed(
              apiError(
                400,
                "invalid_request",
                "invalid JSON body",
                undefined,
                metadata.requestId
              )
            ),
          InvalidCredentials: (error) =>
            Effect.succeed(apiError(
              401,
              "invalid_credentials",
              error.reason === "inactive"
                ? "account is disabled"
                : "invalid username or password",
              undefined,
              metadata.requestId
            )),
          AuthenticationStoreError: () =>
            Effect.succeed(apiError(
              500,
              "internal_error",
              "failed to create session",
              undefined,
              metadata.requestId
            )),
          PasswordHashError: () =>
            Effect.succeed(apiError(
              500,
              "internal_error",
              "failed to create session",
              undefined,
              metadata.requestId
            )),
          InvalidSession: () =>
            Effect.succeed(apiError(
              500,
              "internal_error",
              "internal server error",
              undefined,
              metadata.requestId
            )),
          InvalidToken: () =>
            Effect.succeed(apiError(
              500,
              "internal_error",
              "internal server error",
              undefined,
              metadata.requestId
            )),
          PasswordValidationFailed: () =>
            Effect.succeed(apiError(
              500,
              "internal_error",
              "internal server error",
              undefined,
              metadata.requestId
            ))
        })
      )
    }))
  )

const addLogoutRoute = HttpRouter.post(
  "/api/v1/auth/logout",
  protectedApiHandler((authenticated, request) =>
    Effect.gen(function*() {
      const audit = yield* Audit
      if (authenticated.method === "cookie") {
        const authentication = yield* Authentication
        yield* audit.log(requestMetadata(request), {
          action: "auth.logout",
          actor: {
            id: authenticated.user.id,
            username: authenticated.user.username
          },
          targetType: "user",
          targetId: authenticated.user.id,
          targetLabel: authenticated.user.username
        })
        yield* authentication.logoutSession(authenticated.sessionId).pipe(
          Effect.ignore
        )
      } else {
        yield* audit.log(requestMetadata(request), {
          action: "auth.logout",
          actor: {
            id: authenticated.user.id,
            username: authenticated.user.username,
            tokenId: authenticated.tokenId
          },
          targetType: "user",
          targetId: authenticated.user.id,
          targetLabel: authenticated.user.username,
          metadata: { auth_method: "bearer" }
        })
      }
      return jsonResponse({ success: true }, 200, {
        "set-cookie": clearedSessionCookie
      })
    })
  )
)

const addMeRoute = HttpRouter.get(
  "/api/v1/auth/me",
  protectedApiHandler((authenticated) =>
    Effect.succeed(jsonResponse({
      data: {
        ...publicUser(authenticated.user),
        auth_method: authenticated.method,
        token_id: authenticated.method === "bearer"
          ? authenticated.tokenId
          : null
      }
    }))
  )
)

const addCsrfRoute = HttpRouter.get(
  "/api/v1/auth/csrf",
  protectedApiHandler((authenticated) =>
    Effect.succeed(
      authenticated.method === "bearer"
        ? apiError(
          400,
          "invalid_request",
          "csrf tokens are not used with bearer authentication"
        )
        : jsonResponse({ csrf_token: authenticated.csrfToken })
    )
  )
)

const addPasswordChangeRoute = HttpRouter.post(
  "/api/v1/auth/password/change",
  protectedApiHandler((authenticated, request) =>
    Effect.gen(function*() {
      const input = yield* readStringRecord(request, [
        "current_password",
        "new_password",
        "confirm_password"
      ])
      const authentication = yield* Authentication
      yield* authentication.changePassword(
        authenticated.user,
        input.current_password ?? "",
        input.new_password ?? "",
        input.confirm_password ?? ""
      )
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "auth.password_change",
        actor: {
          id: authenticated.user.id,
          username: authenticated.user.username,
          ...(authenticated.method === "bearer"
            ? { tokenId: authenticated.tokenId }
            : {})
        },
        targetType: "user",
        targetId: authenticated.user.id,
        targetLabel: authenticated.user.username,
        metadata: {
          forced: authenticated.user.mustChangePassword,
          auth_method: authenticated.method
        }
      })
      return jsonResponse({ success: true })
    }).pipe(
      Effect.catchTags({
        InvalidJsonBody: () =>
          Effect.succeed(
            apiError(400, "invalid_request", "invalid JSON body")
          ),
        PasswordValidationFailed: (error) =>
          Effect.succeed(apiError(
            422,
            "validation_failed",
            "validation failed",
            error.fields
          )),
        PasswordHashError: () =>
          Effect.succeed(apiError(
            500,
            "internal_error",
            "failed to hash password"
          )),
        AuthenticationStoreError: () =>
          Effect.succeed(apiError(
            500,
            "internal_error",
            "failed to update password"
          )),
        InvalidCredentials: () =>
          Effect.succeed(apiError(500, "internal_error", "internal server error")),
        InvalidSession: () =>
          Effect.succeed(apiError(500, "internal_error", "internal server error")),
        InvalidToken: () =>
          Effect.succeed(apiError(500, "internal_error", "internal server error"))
      })
    )
  )
)

export const addAuthApiRoutes = (development: boolean) =>
  <E, R>(router: HttpRouter.HttpRouter<E, R>) =>
    router.pipe(
      addLoginRoute(development),
      addLogoutRoute,
      addMeRoute,
      addCsrfRoute,
      addPasswordChangeRoute
    )
