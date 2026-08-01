import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import {
  ContactConflict,
  ContactNotFound,
  Contacts,
  ContactStoreError,
  validateContact
} from "../../domain/contacts/contacts.ts"
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

type ContactInput = {
  readonly name: string
  readonly contactType: string
  readonly phone: string
  readonly email: string
  readonly address: string
  readonly notes: string
  readonly mapsLink: string
  readonly className: string
  readonly distanceKm: number
  readonly hasSiblingDiscount: boolean
  readonly isReturnOnly: boolean
  readonly isActive: boolean | undefined
}

const parseContactInput = (
  request: HttpServerRequest.HttpServerRequest
): Effect.Effect<ContactInput, InvalidJsonBody> =>
  readJsonObject(request, [
    "name",
    "contact_type",
    "phone",
    "email",
    "address",
    "notes",
    "maps_link",
    "class",
    "distance_km",
    "has_sibling_discount",
    "is_return_only",
    "is_active"
  ]).pipe(
    Effect.flatMap((input) =>
      Effect.try({
        try: () => {
          for (const field of [
            "name",
            "contact_type",
            "phone",
            "email",
            "address",
            "notes",
            "maps_link",
            "class"
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
          const distance = input.distance_km
          if (
            distance !== undefined &&
            distance !== null &&
            typeof distance !== "number"
          ) {
            throw new Error("invalid distance_km")
          }
          for (const field of [
            "has_sibling_discount",
            "is_return_only",
            "is_active"
          ]) {
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
            name: typeof input.name === "string" ? input.name : "",
            contactType: typeof input.contact_type === "string"
              ? input.contact_type
              : "",
            phone: typeof input.phone === "string" ? input.phone : "",
            email: typeof input.email === "string" ? input.email : "",
            address: typeof input.address === "string" ? input.address : "",
            notes: typeof input.notes === "string" ? input.notes : "",
            mapsLink: typeof input.maps_link === "string"
              ? input.maps_link
              : "",
            className: typeof input.class === "string" ? input.class : "",
            distanceKm: typeof distance === "number" ? distance : 0,
            hasSiblingDiscount: input.has_sibling_discount === true,
            isReturnOnly: input.is_return_only === true,
            isActive: typeof input.is_active === "boolean"
              ? input.is_active
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
  authentication.effectiveCapabilities.includes("contacts.manage")

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
    "contacts.manage capability required"
  )

const addListRoute = HttpRouter.get(
  "/api/v1/contacts",
  protectedApiHandler((_authentication, request) =>
    Effect.gen(function*() {
      const query = new URL(request.url, "http://localhost").searchParams
      const contacts = yield* Contacts
      const values = yield* contacts.list({
        type: query.get("type") ?? "",
        search: query.get("search") ?? ""
      })
      return jsonResponse(paginate(values, parsePage(request)))
    }).pipe(
      Effect.catchTag(
        "ContactStoreError",
        () => Effect.succeed(
          apiError(500, "internal_error", "failed to list contacts")
        )
      )
    )
  )
)

const addGetRoute = HttpRouter.get(
  "/api/v1/contacts/:id",
  protectedApiHandler(() =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "contact not found")
      }
      const contacts = yield* Contacts
      const contact = yield* contacts.get(id)
      return jsonResponse(contact)
    }).pipe(
      Effect.catchTags({
        ContactNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "contact not found")),
        ContactStoreError: () =>
          Effect.succeed(apiError(404, "not_found", "contact not found"))
      })
    )
  )
)

const addCreateRoute = HttpRouter.post(
  "/api/v1/contacts",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const input = yield* parseContactInput(request)
      const fields = validateContact(input)
      if (Object.keys(fields).length > 0) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          fields
        )
      }
      const contacts = yield* Contacts
      const created = yield* contacts.create({
        ...input,
        isActive: input.isActive ?? true
      })
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "contact.create",
        actor: actor(authentication),
        targetType: "contact",
        targetId: created.id,
        targetLabel: created.name,
        metadata: {
          after: {
            name: created.name,
            contact_type: created.contact_type,
            email: created.email,
            phone: created.phone,
            class: created.class,
            distance_km: created.distance_km,
            has_sibling_discount: created.has_sibling_discount,
            is_return_only: created.is_return_only,
            is_active: created.is_active
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
        ContactStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to create contact")
          )
      })
    )
  })
)

const addUpdateRoute = HttpRouter.put(
  "/api/v1/contacts/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "contact not found")
      }
      const contacts = yield* Contacts
      const existing = yield* contacts.get(id)
      const input = yield* parseContactInput(request)
      const fields = validateContact(input)
      if (Object.keys(fields).length > 0) {
        return apiError(
          422,
          "validation_failed",
          "validation failed",
          fields
        )
      }
      const updated = yield* contacts.update(id, {
        ...input,
        isActive: input.isActive ?? existing.is_active
      })
      const metadata = auditDiff(
        {
          name: existing.name,
          contact_type: existing.contact_type,
          email: existing.email,
          phone: existing.phone,
          address: existing.address,
          notes: existing.notes,
          maps_link: existing.maps_link,
          class: existing.class,
          distance_km: existing.distance_km,
          has_sibling_discount: existing.has_sibling_discount,
          is_return_only: existing.is_return_only,
          is_active: existing.is_active
        },
        {
          name: updated.name,
          contact_type: updated.contact_type,
          email: updated.email,
          phone: updated.phone,
          address: updated.address,
          notes: updated.notes,
          maps_link: updated.maps_link,
          class: updated.class,
          distance_km: updated.distance_km,
          has_sibling_discount: updated.has_sibling_discount,
          is_return_only: updated.is_return_only,
          is_active: updated.is_active
        },
        [
          "name",
          "contact_type",
          "email",
          "phone",
          "address",
          "notes",
          "maps_link",
          "class",
          "distance_km",
          "has_sibling_discount",
          "is_return_only",
          "is_active"
        ]
      )
      if (metadata !== undefined) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "contact.update",
          actor: actor(authentication),
          targetType: "contact",
          targetId: id,
          targetLabel: existing.name,
          metadata
        })
      }
      return jsonResponse(updated)
    }).pipe(
      Effect.catchTags({
        InvalidJsonBody: () =>
          Effect.succeed(
            apiError(400, "invalid_request", "invalid request body")
          ),
        ContactNotFound: () =>
          Effect.succeed(apiError(404, "not_found", "contact not found")),
        ContactStoreError: () =>
          Effect.succeed(
            apiError(500, "internal_error", "failed to update contact")
          )
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/api/v1/contacts/:id",
  protectedApiHandler((authentication, request) => {
    if (!canManage(authentication)) {
      return Effect.succeed(forbidden())
    }
    return Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const id = parseId(params.id)
      if (id === undefined) {
        return apiError(404, "not_found", "contact not found")
      }
      const contacts = yield* Contacts
      const removed = yield* contacts.remove(id)
      if (removed !== undefined) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "contact.delete",
          actor: actor(authentication),
          targetType: "contact",
          targetId: id,
          targetLabel: removed.name,
          metadata: {
            before: {
              name: removed.name,
              contact_type: removed.contact_type,
              email: removed.email
            }
          }
        })
      }
      return HttpServerResponse.empty({ status: 204 })
    }).pipe(
      Effect.catchTags({
        ContactConflict: (error) =>
          Effect.succeed(apiError(
            409,
            "conflict",
            `cannot delete contact: has ${error.count} linked ${error.entity}(s)`
          )),
        ContactStoreError: (error) =>
          Effect.succeed(apiError(
            409,
            "conflict",
            String(error.cause)
          ))
      })
    )
  })
)

export const addContactApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  addListRoute,
  addGetRoute,
  addCreateRoute,
  addUpdateRoute,
  addDeleteRoute
)
