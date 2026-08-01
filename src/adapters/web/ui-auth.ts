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
  type AuthUser,
  type CookieAuthentication,
  InvalidCredentials,
  InvalidSession,
  PasswordValidationFailed
} from "../../domain/auth/authentication.ts"
import { checkCsrf } from "../../domain/auth/csrf.ts"
import {
  RateLimiter,
  retryAfterSeconds
} from "../../domain/security/rate-limiter.ts"
import { apiError } from "./api-response.ts"
import {
  requestMetadata,
  unspoofableClientIp
} from "./request-metadata.ts"
import { pageTemplate } from "./template-assets.ts"

export const dashboardBasePath = "/dashboard"
const basePath = dashboardBasePath
const loginPath = `${basePath}/login`
const passwordPath = `${basePath}/password/change`

const sessionCookie = (sessionId: string, development: boolean) =>
  [
    `session_id=${sessionId}`,
    "Path=/",
    "Max-Age=172800",
    "HttpOnly",
    ...(development ? [] : ["Secure"]),
    "SameSite=Lax"
  ].join("; ")

const clearedSessionCookie = "session_id=; Path=/; Max-Age=0"
const clearedFlashCookie = "flash=; Path=/; Max-Age=0"

export const uiRedirect = (
  location: string,
  headers: Readonly<Record<string, string>> = {}
) =>
  HttpServerResponse.text(`<a href="${location}">See Other</a>.\n\n`, {
    status: 303,
    headers: {
      "content-type": "text/html; charset=utf-8",
      location,
      ...headers
    }
  })

export const uiPlainError = (status: number, message: string) =>
  HttpServerResponse.text(`${message}\n`, {
    status,
    headers: { "content-type": "text/plain; charset=utf-8" }
  })

export const uiFlashCookie = (message: string) =>
  `flash="${[...message].map((character) => {
    const code = character.codePointAt(0) ?? 0
    return code >= 0x20 && code <= 0x7e && character !== "%"
      ? character
      : encodeURIComponent(character)
  }).join("").replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"; ` +
  "Path=/; HttpOnly; SameSite=Lax"

const templateUser = (user: AuthUser) => ({
  ID: user.id,
  Username: user.username,
  FullName: user.fullName,
  Role: user.role,
  IsActive: user.isActive,
  MustChangePassword: user.mustChangePassword,
  CreatedAt: user.createdAt,
  UpdatedAt: user.updatedAt,
  Capabilities: user.capabilities
})

const cookieValue = (
  request: HttpServerRequest.HttpServerRequest,
  name: string
) => {
  const value = request.cookies[name] ?? ""
  const unquoted = value.startsWith('"') && value.endsWith('"')
    ? value.slice(1, -1)
    : value
  if (name === "flash") {
    try {
      return decodeURIComponent(unquoted)
    } catch {
      return unquoted
    }
  }
  return unquoted
}

export const renderUiPage = (
  request: HttpServerRequest.HttpServerRequest,
  page: string,
  title: string,
  data: unknown,
  authenticated?: CookieAuthentication,
  extraTemplates: ReadonlyArray<string> = [],
  displayFlashOverride?: string
) => {
  const flash = cookieValue(request, "flash")
  const path = new URL(request.url, "http://localhost").pathname.slice(
    basePath.length
  ) || "/"
  const html = pageTemplate(page, extraTemplates).render("base", {
    User: authenticated === undefined
      ? null
      : templateUser(authenticated.user),
    Title: title,
    Flash: displayFlashOverride ?? flash,
    Path: path,
    CSRFToken: authenticated?.csrfToken ?? "",
    BasePath: basePath,
    Data: data
  })
  return HttpServerResponse.text(html, {
    headers: {
      "content-type": "text/html; charset=utf-8",
      ...(flash === "" ? {} : { "set-cookie": clearedFlashCookie })
    }
  })
}

const readForm = (
  request: HttpServerRequest.HttpServerRequest
): Effect.Effect<URLSearchParams> =>
  request.text.pipe(
    Effect.map((body) => new URLSearchParams(body)),
    Effect.orElseSucceed(() => new URLSearchParams())
  )

const authenticateUi = (
  request: HttpServerRequest.HttpServerRequest
): Effect.Effect<
  CookieAuthentication,
  HttpServerResponse.HttpServerResponse,
  Authentication
> => {
  const sessionId = request.cookies.session_id
  if (sessionId === undefined) {
    return Effect.fail(uiRedirect(loginPath))
  }
  return Effect.gen(function*() {
    const authentication = yield* Authentication
    return yield* authentication.authenticateSession(sessionId)
  }).pipe(
    Effect.mapError((error) =>
      error instanceof InvalidSession
        ? uiRedirect(
          loginPath,
          error.reason === "invalid_or_expired"
            ? { "set-cookie": clearedSessionCookie }
            : {}
        )
        : uiPlainError(500, "Internal Server Error")
    )
  )
}

export const protectedUiHandler = <R>(
  handler: (
    authentication: CookieAuthentication,
    request: HttpServerRequest.HttpServerRequest,
    form: URLSearchParams
  ) => Effect.Effect<HttpServerResponse.HttpServerResponse, never, R>
) =>
  Effect.gen(function*() {
    const request = yield* HttpServerRequest.HttpServerRequest
    const authenticated = yield* authenticateUi(request)
    const form = request.method === "GET" || request.method === "HEAD"
      ? new URLSearchParams()
      : yield* readForm(request)
    const csrf = checkCsrf({
      method: request.method,
      bearer: false,
      expected: authenticated.csrfToken,
      ...(request.headers["x-csrf-token"] === undefined
        ? {}
        : { headerToken: request.headers["x-csrf-token"] }),
      ...(form.get("csrf_token") === null
        ? {}
        : { formToken: form.get("csrf_token") ?? "" })
    })
    if (csrf !== "allowed") {
      return uiPlainError(
        403,
        csrf === "missing"
          ? "Forbidden: missing CSRF token"
          : "Forbidden: invalid CSRF token"
      )
    }
    const pathname = new URL(request.url, "http://localhost").pathname
    if (
      authenticated.user.mustChangePassword &&
      pathname !== passwordPath
    ) {
      return uiRedirect(passwordPath)
    }
    return yield* handler(authenticated, request, form)
  }).pipe(Effect.catchAll(Effect.succeed))

const addLoginPageRoute = HttpRouter.get(
  loginPath,
  Effect.gen(function*() {
    const request = yield* HttpServerRequest.HttpServerRequest
    const sessionId = request.cookies.session_id
    if (sessionId !== undefined) {
      const authentication = yield* Authentication
      const authenticated = yield* authentication.authenticateSession(sessionId)
        .pipe(Effect.option)
      if (authenticated._tag === "Some") {
        return uiRedirect(`${basePath}/`)
      }
    }
    return renderUiPage(request, "auth/login", "Login", null)
  })
)

const addLoginRoute = (development: boolean) =>
  HttpRouter.post(
    loginPath,
    Effect.gen(function*() {
      const request = yield* HttpServerRequest.HttpServerRequest
      const form = yield* readForm(request)
      const username = form.get("username") ?? ""
      const password = form.get("password") ?? ""
      const limiter = yield* RateLimiter
      const rateKey = `${unspoofableClientIp(request)}:${username || "unknown"}`
      if (!(yield* limiter.take("login", rateKey))) {
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

      if (username === "" || password === "") {
        return renderUiPage(request, "auth/login", "Login", {
          Error: "Username and password are required",
          Username: username
        })
      }

      const authentication = yield* Authentication
      const audit = yield* Audit
      const metadata = requestMetadata(request)
      const session = yield* authentication.login(username, password).pipe(
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
        ),
        Effect.catchTags({
          InvalidCredentials: (error) =>
            Effect.succeed(error.reason === "inactive"
              ? "Account is disabled"
              : "Invalid username or password"),
          AuthenticationStoreError: () =>
            Effect.succeed(uiPlainError(500, "Internal Server Error")),
          PasswordHashError: () =>
            Effect.succeed(uiPlainError(500, "Internal Server Error")),
          InvalidSession: () =>
            Effect.succeed(uiPlainError(500, "Internal Server Error")),
          InvalidToken: () =>
            Effect.succeed(uiPlainError(500, "Internal Server Error")),
          PasswordValidationFailed: () =>
            Effect.succeed(uiPlainError(500, "Internal Server Error"))
        })
      )

      if (typeof session === "string") {
        return renderUiPage(request, "auth/login", "Login", {
          Error: session,
          Username: username
        })
      }
      if ("status" in session) {
        return session
      }

      yield* audit.log(metadata, {
        action: "auth.login",
        actor: { id: session.user.id, username: session.user.username },
        targetType: "user",
        targetId: session.user.id,
        targetLabel: session.user.username
      })
      return uiRedirect(
        session.user.mustChangePassword ? passwordPath : `${basePath}/`,
        { "set-cookie": sessionCookie(session.sessionId, development) }
      )
    })
  )

const addPasswordPageRoute = HttpRouter.get(
  passwordPath,
  protectedUiHandler((authenticated, request) =>
    Effect.succeed(renderUiPage(
      request,
      "auth/password_change",
      "Change Password",
      { Forced: authenticated.user.mustChangePassword, Error: "" },
      authenticated
    ))
  )
)

const passwordMessage = (
  error: PasswordValidationFailed
): string => {
  if (error.fields.current_password !== undefined) {
    return "Current password is incorrect"
  }
  if (error.fields.new_password === "must be at least 8 characters") {
    return "New password must be at least 8 characters"
  }
  if (error.fields.confirm_password !== undefined) {
    return "New password and confirmation do not match"
  }
  return "New password must be different from current password"
}

const addPasswordChangeRoute = HttpRouter.post(
  passwordPath,
  protectedUiHandler((authenticated, request, form) =>
    Effect.gen(function*() {
      const authentication = yield* Authentication
      const audit = yield* Audit
      yield* authentication.changePassword(
        authenticated.user,
        form.get("current_password") ?? "",
        form.get("new_password") ?? "",
        form.get("confirm_password") ?? ""
      )
      yield* audit.log(requestMetadata(request), {
        action: "auth.password_change",
        actor: {
          id: authenticated.user.id,
          username: authenticated.user.username
        },
        targetType: "user",
        targetId: authenticated.user.id,
        targetLabel: authenticated.user.username,
        metadata: { forced: authenticated.user.mustChangePassword }
      })
      return uiRedirect(`${basePath}/`, {
        "set-cookie": uiFlashCookie("Password updated successfully")
      })
    }).pipe(
      Effect.catchTag(
        "PasswordValidationFailed",
        (error) => Effect.succeed(renderUiPage(
          request,
          "auth/password_change",
          "Change Password",
          {
            Forced: authenticated.user.mustChangePassword,
            Error: passwordMessage(error)
          },
          authenticated
        ))
      ),
      Effect.catchAll(() =>
        Effect.succeed(uiPlainError(500, "Internal Server Error"))
      )
    )
  )
)

const addLogoutRoute = HttpRouter.post(
  `${basePath}/logout`,
  protectedUiHandler((authenticated, request) =>
    Effect.gen(function*() {
      const audit = yield* Audit
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
      const authentication = yield* Authentication
      yield* authentication.logoutSession(authenticated.sessionId).pipe(
        Effect.ignore
      )
      return uiRedirect(loginPath, { "set-cookie": clearedSessionCookie })
    })
  )
)

export const addUiAuthRoutes = (development: boolean) =>
  <E, R>(router: HttpRouter.HttpRouter<E, R>) =>
    router.pipe(
      addLoginPageRoute,
      addLoginRoute(development),
      addPasswordPageRoute,
      addPasswordChangeRoute,
      addLogoutRoute
    )
