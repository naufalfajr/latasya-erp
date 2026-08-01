import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import {
  UserConflict,
  UserNotFound,
  UserPasswordError,
  Users,
  UserStoreError,
  validateUser
} from "../../domain/access/users.ts"
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

type UserInput = {
  readonly username: string
  readonly fullName: string
  readonly role: string
  readonly password: string
  readonly isActive: boolean | undefined
}

const parseUserInput = (
  request: HttpServerRequest.HttpServerRequest
): Effect.Effect<UserInput, InvalidJsonBody> =>
  readJsonObject(request, [
    "username",
    "full_name",
    "role",
    "is_active",
    "password"
  ]).pipe(
    Effect.flatMap((input) =>
      Effect.try({
        try: () => {
          for (const field of [
            "username",
            "full_name",
            "role",
            "password"
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
          const isActive = input.is_active
          if (
            isActive !== undefined &&
            isActive !== null &&
            typeof isActive !== "boolean"
          ) {
            throw new Error("invalid is_active")
          }
          return {
            username: typeof input.username === "string" ? input.username : "",
            fullName: typeof input.full_name === "string"
              ? input.full_name
              : "",
            role: typeof input.role === "string" ? input.role : "",
            password: typeof input.password === "string"
              ? input.password
              : "",
            isActive: typeof isActive === "boolean" ? isActive : undefined
          }
        },
        catch: () => new InvalidJsonBody()
      })
    )
  )

const parseUserId = (value: string | undefined) =>
  value !== undefined && /^[+-]?\d+$/.test(value)
    ? Number(value)
    : undefined

const canManageUsers = (authentication: Authenticated) =>
  authentication.effectiveCapabilities.includes("users.manage")

const forbidden = () =>
  apiError(403, "forbidden", "insufficient permissions")

const auditActor = (authentication: Authenticated) => ({
  id: authentication.user.id,
  username: authentication.user.username,
  ...(authentication.method === "bearer"
    ? { tokenId: authentication.tokenId }
    : {})
})

const addListRoute = HttpRouter.get(
  "/api/v1/users",
  protectedApiHandler((authentication, request) => {
    if (!canManageUsers(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const users = yield* Users
      const values = yield* users.list
      return jsonResponse(paginate(values, parsePage(request)))
    }).pipe(
      Effect.catchTag(
        "UserStoreError",
        () => Effect.succeed(
          apiError(500, "internal_error", "failed to list users")
        )
      )
    )
  })
)

const addGetRoute = HttpRouter.get(
  "/api/v1/users/:id",
  protectedApiHandler((authentication) => {
    if (!canManageUsers(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseUserId(params.id)
      if (id === undefined) {
        return apiError(400, "invalid_request", "invalid user id")
      }
      const users = yield* Users
      const user = yield* users.get(id)
      return jsonResponse({ data: user })
    }).pipe(
      Effect.catchTags({
        UserNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "user not found")),
        UserStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "internal server error")
          )
      })
    )
  })
)

const addCreateRoute = HttpRouter.post(
  "/api/v1/users",
  protectedApiHandler((authentication, request) => {
    if (!canManageUsers(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const input = yield* parseUserInput(request)
      const users = yield* Users
      const roleValid = input.role === ""
        ? false
        : yield* users.roleExists(input.role)
      const fields = validateUser(input, false, roleValid)
      if (Object.keys(fields).length > 0) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          fields
        )
      }
      const created = yield* users.create({
        username: input.username.trim(),
        fullName: input.fullName.trim(),
        role: input.role,
        isActive: input.isActive ?? true,
        password: input.password
      })
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "user.create",
        actor: auditActor(authentication),
        targetType: "user",
        targetId: created.id,
        targetLabel: created.username,
        metadata: {
          after: {
            username: created.username,
            full_name: created.full_name,
            role: created.role,
            is_active: created.is_active
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
        UserConflict: (error) =>
          Effect.succeed(
            error.reason === "duplicate_username"
              ? apiError(409, "conflict", "username already exists")
              : apiError(500, "internal_error", "failed to create user")
          ),
        UserPasswordError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to hash password")
          ),
        UserStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to create user")
          )
      })
    )
  })
)

const addUpdateRoute = HttpRouter.put(
  "/api/v1/users/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManageUsers(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseUserId(params.id)
      if (id === undefined) {
        return apiError(400, "invalid_request", "invalid user id")
      }
      const users = yield* Users
      const existing = yield* users.get(id)
      const input = yield* parseUserInput(request)
      const roleValid = input.role === ""
        ? false
        : yield* users.roleExists(input.role)
      const fields = validateUser(input, true, roleValid)
      if (Object.keys(fields).length > 0) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          fields
        )
      }
      const isActive = input.isActive ?? existing.is_active
      if (authentication.user.id === id && !isActive) {
        return apiError(
          409,
          "conflict",
          "cannot deactivate your own account"
        )
      }
      const updated = yield* users.update(authentication.user.id, id, {
        fullName: input.fullName.trim(),
        role: input.role,
        isActive,
        ...(input.password === "" ? {} : { password: input.password })
      })
      const metadata = auditDiff(
        {
          full_name: existing.full_name,
          role: existing.role,
          is_active: existing.is_active
        },
        {
          full_name: updated.full_name,
          role: updated.role,
          is_active: updated.is_active
        },
        ["full_name", "role", "is_active"]
      )
      if (metadata !== undefined) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "user.update",
          actor: auditActor(authentication),
          targetType: "user",
          targetId: id,
          targetLabel: existing.username,
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
        UserNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "user not found")),
        UserConflict: () =>
          Effect.succeed(apiError(
            409,
            "conflict",
            "cannot deactivate your own account"
          )),
        UserPasswordError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to hash password")
          ),
        UserStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to update user")
          )
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/api/v1/users/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManageUsers(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseUserId(params.id)
      if (id === undefined) {
        return apiError(400, "invalid_request", "invalid user id")
      }
      if (authentication.user.id === id) {
        return apiError(
          409,
          "conflict",
          "cannot deactivate your own account"
        )
      }
      const users = yield* Users
      const existing = yield* users.deactivate(authentication.user.id, id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "user.delete",
        actor: auditActor(authentication),
        targetType: "user",
        targetId: existing.id,
        targetLabel: existing.username,
        metadata: {
          before: { is_active: existing.is_active },
          after: { is_active: false }
        }
      })
      return HttpServerResponse.empty({ status: 204 })
    }).pipe(
      Effect.catchTags({
        UserNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "user not found")),
        UserConflict: () =>
          Effect.succeed(apiError(
            409,
            "conflict",
            "cannot deactivate your own account"
          )),
        UserStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to deactivate user")
          )
      })
    )
  })
)

export const addUserApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addListRoute,
  addGetRoute,
  addCreateRoute,
  addUpdateRoute,
  addDeleteRoute
)
