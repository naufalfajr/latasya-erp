import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import {
  RoleConflict,
  RoleNotFound,
  Roles,
  RoleStoreError,
  validateRole
} from "../../domain/access/roles.ts"
import { Audit, auditDiff } from "../../domain/audit/audit.ts"
import {
  allCapabilities,
  type Capability
} from "../../domain/auth/capability.ts"
import type { Authenticated } from "../../domain/auth/authentication.ts"
import { apiError, jsonResponse } from "./api-response.ts"
import { protectedApiHandler } from "./auth-api.ts"
import {
  InvalidJsonBody,
  readJsonObject
} from "./json-body.ts"
import { paginate, parsePage } from "./pagination.ts"
import { requestMetadata } from "./request-metadata.ts"

type RoleInput = {
  readonly name: string
  readonly description: string
  readonly capabilities: ReadonlyArray<string>
}

const parseRoleInput = (
  request: HttpServerRequest.HttpServerRequest
): Effect.Effect<RoleInput, InvalidJsonBody> =>
  readJsonObject(request, [
    "name",
    "description",
    "capabilities"
  ]).pipe(
    Effect.flatMap((input) =>
      Effect.try({
        try: () => {
          const name = input.name
          const description = input.description
          const capabilities = input.capabilities
          if (
            (name !== undefined && name !== null && typeof name !== "string") ||
            (
              description !== undefined &&
              description !== null &&
              typeof description !== "string"
            ) ||
            (
              capabilities !== undefined &&
              capabilities !== null &&
              (
                !Array.isArray(capabilities) ||
                !capabilities.every((value) => typeof value === "string")
              )
            )
          ) {
            throw new Error("invalid role input")
          }
          return {
            name: typeof name === "string" ? name : "",
            description: typeof description === "string" ? description : "",
            capabilities: Array.isArray(capabilities) ? capabilities : []
          }
        },
        catch: () => new InvalidJsonBody()
      })
    )
  )

const canManageRoles = (authentication: Authenticated) =>
  authentication.effectiveCapabilities.includes("roles.manage")

const forbidden = () =>
  apiError(403, "forbidden", "insufficient permissions")

const auditActor = (authentication: Authenticated) => ({
  id: authentication.user.id,
  username: authentication.user.username,
  ...(authentication.method === "bearer"
    ? { tokenId: authentication.tokenId }
    : {})
})

const addCapabilitiesRoute = HttpRouter.get(
  "/api/v1/roles/capabilities",
  protectedApiHandler(() =>
    Effect.succeed(jsonResponse({ data: allCapabilities }))
  )
)

const addListRoute = HttpRouter.get(
  "/api/v1/roles",
  protectedApiHandler((authentication, request) => {
    if (!canManageRoles(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const roles = yield* Roles
      const values = yield* roles.list
      return jsonResponse(paginate(values, parsePage(request)))
    }).pipe(
      Effect.catchTag(
        "RoleStoreError",
        () => Effect.succeed(
          apiError(500, "internal_error", "failed to list roles")
        )
      )
    )
  })
)

const addGetRoute = HttpRouter.get(
  "/api/v1/roles/:name",
  protectedApiHandler((authentication) => {
    if (!canManageRoles(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const roles = yield* Roles
      const role = yield* roles.get(params.name ?? "")
      return jsonResponse({ data: role })
    }).pipe(
      Effect.catchTags({
        RoleNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "role not found")),
        RoleStoreError: () =>
          Effect.succeed(apiError(404, "not_found", "role not found"))
      })
    )
  })
)

const addCreateRoute = HttpRouter.post(
  "/api/v1/roles",
  protectedApiHandler((authentication, request) => {
    if (!canManageRoles(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const input = yield* parseRoleInput(request)
      const fields = validateRole(input, false)
      if (Object.keys(fields).length > 0) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          fields
        )
      }
      const roles = yield* Roles
      const created = yield* roles.create({
        name: input.name,
        description: input.description.trim(),
        capabilities: input.capabilities as ReadonlyArray<Capability>
      })
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "role.create",
        actor: auditActor(authentication),
        targetType: "role",
        targetLabel: created.name,
        metadata: {
          after: {
            name: created.name,
            description: created.description,
            capabilities: created.capabilities
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
        RoleConflict: (error) =>
          Effect.succeed(
            error.reason === "duplicate"
              ? apiError(
                422,
                "validation_failed",
                "validation failed",
                { name: "role name already exists" }
              )
              : apiError(500, "internal_error", "failed to create role")
          ),
        RoleStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to create role")
          )
      })
    )
  })
)

const addUpdateRoute = HttpRouter.put(
  "/api/v1/roles/:name",
  protectedApiHandler((authentication, request) => {
    if (!canManageRoles(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const name = params.name ?? ""
      const roles = yield* Roles
      const existing = yield* roles.get(name)
      if (existing.name === "admin") {
        return apiError(
          409,
          "conflict",
          "the admin role cannot be edited"
        )
      }
      const input = yield* parseRoleInput(request)
      const fields = validateRole(input, true)
      if (Object.keys(fields).length > 0) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          fields
        )
      }
      const updated = yield* roles.update(name, {
        description: input.description.trim(),
        capabilities: input.capabilities as ReadonlyArray<Capability>
      })
      const metadata = auditDiff(
        {
          description: existing.description,
          capabilities: existing.capabilities
        },
        {
          description: updated.description,
          capabilities: updated.capabilities
        },
        ["description", "capabilities"]
      )
      if (metadata !== undefined) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "role.update",
          actor: auditActor(authentication),
          targetType: "role",
          targetLabel: updated.name,
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
        RoleNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "role not found")),
        RoleConflict: (error) =>
          Effect.succeed(
            error.reason === "admin_edit"
              ? apiError(
                409,
                "conflict",
                "the admin role cannot be edited"
              )
              : apiError(500, "internal_error", "failed to update role")
          ),
        RoleStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to update role")
          )
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/api/v1/roles/:name",
  protectedApiHandler((authentication, request) => {
    if (!canManageRoles(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const roles = yield* Roles
      const removed = yield* roles.remove(params.name ?? "")
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "role.delete",
        actor: auditActor(authentication),
        targetType: "role",
        targetLabel: removed.name,
        metadata: {
          before: {
            name: removed.name,
            description: removed.description,
            capabilities: removed.capabilities
          }
        }
      })
      return HttpServerResponse.empty({ status: 204 })
    }).pipe(
      Effect.catchTags({
        RoleNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "role not found")),
        RoleConflict: (error) => {
          const message = error.reason === "admin_delete"
            ? "the admin role cannot be deleted"
            : error.reason === "in_use"
            ? "cannot delete role: still assigned to users"
            : "failed to delete role"
          return Effect.succeed(apiError(409, "conflict", message))
        },
        RoleStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to delete role")
          )
      })
    )
  })
)

export const addRoleApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addCapabilitiesRoute,
  addListRoute,
  addGetRoute,
  addCreateRoute,
  addUpdateRoute,
  addDeleteRoute
)
