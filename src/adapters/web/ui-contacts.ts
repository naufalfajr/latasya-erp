import { HttpRouter, HttpServerResponse } from "@effect/platform"
import { SqlClient } from "@effect/sql"
import { Effect } from "effect"
import {
  type Contact,
  type ContactValues,
  Contacts
} from "../../domain/contacts/contacts.ts"
import { Audit, auditDiff } from "../../domain/audit/audit.ts"
import type { CookieAuthentication } from "../../domain/auth/authentication.ts"
import {
  dashboardBasePath,
  protectedUiHandler,
  renderUiPage,
  uiFlashCookie,
  uiPlainError,
  uiRedirect
} from "./ui-auth.ts"
import { requestMetadata } from "./request-metadata.ts"

type ContactInput = ContactValues & {
  readonly routeId: number
}

const hasManage = (authenticated: CookieAuthentication) =>
  authenticated.user.role === "admin" ||
  authenticated.effectiveCapabilities.includes("contacts.manage")

const parseId = (value: string | undefined) =>
  value !== undefined && /^[+-]?\d+$/.test(value)
    ? Number(value)
    : undefined

const parseOptionalInt = (value: string | null) => {
  const parsed = Number.parseInt(value ?? "", 10)
  return Number.isNaN(parsed) ? 0 : parsed
}

const parseOptionalFloat = (value: string | null) => {
  const parsed = Number.parseFloat((value ?? "").trim().replace(",", "."))
  return Number.isNaN(parsed) ? 0 : parsed
}

const valuesFromForm = (form: URLSearchParams): ContactInput => ({
  name: form.get("name") ?? "",
  contactType: form.get("contact_type") ?? "",
  phone: form.get("phone") ?? "",
  email: form.get("email") ?? "",
  address: form.get("address") ?? "",
  notes: form.get("notes") ?? "",
  mapsLink: (form.get("maps_link") ?? "").trim(),
  className: (form.get("class") ?? "").trim(),
  distanceKm: parseOptionalFloat(form.get("distance_km")),
  hasSiblingDiscount: form.get("has_sibling_discount") === "on",
  isReturnOnly: form.get("is_return_only") === "on",
  routeId: parseOptionalInt(form.get("route_id")),
  isActive: form.get("is_active") === "on"
})

const validate = (values: ContactInput) => {
  const errors: Record<string, string> = {}
  if (values.name === "") {
    errors.name = "Name is required"
  }
  if (values.contactType === "") {
    errors.contact_type = "Contact type is required"
  }
  if ([...values.className].length > 5) {
    errors.class = "Class must be 5 characters or fewer"
  }
  if (values.distanceKm < 0) {
    errors.distance_km = "Distance must be 0 or greater"
  }
  return errors
}

const contactData = (
  contact: Contact | (ContactInput & { readonly id?: number })
) => ({
  ID: contact.id ?? 0,
  Name: contact.name,
  ContactType: "contact_type" in contact
    ? contact.contact_type
    : contact.contactType,
  Phone: contact.phone,
  Email: contact.email,
  Address: contact.address,
  Notes: contact.notes,
  MapsLink: "maps_link" in contact ? contact.maps_link : contact.mapsLink,
  Class: "class" in contact ? contact.class : contact.className,
  DistanceKm: "distance_km" in contact
    ? contact.distance_km
    : contact.distanceKm,
  HasSiblingDiscount: "has_sibling_discount" in contact
    ? contact.has_sibling_discount
    : contact.hasSiblingDiscount,
  IsReturnOnly: "is_return_only" in contact
    ? contact.is_return_only
    : contact.isReturnOnly,
  RouteID: "route_id" in contact ? contact.route_id : contact.routeId,
  RouteName: "route_name" in contact ? (contact.route_name ?? "") : "",
  IsActive: "is_active" in contact ? contact.is_active : contact.isActive,
  PortalCode: "portal_code" in contact ? (contact.portal_code ?? "") : ""
})

const routes = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient
  return yield* sql<{
    readonly id: number
    readonly name: string
  }>`
    SELECT id, name
    FROM routes
    WHERE is_active = 1
    ORDER BY name
  `.pipe(Effect.orElseSucceed(() => []))
})

const routeCapacity = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient
  return yield* sql<{
    readonly id: number
    readonly route_name: string
    readonly vehicle_code: string
    readonly capacity: number
    readonly used: number
  }>`
    SELECT
      r.id,
      r.name AS route_name,
      COALESCE(v.code, '') AS vehicle_code,
      COALESCE(v.capacity, 0) AS capacity,
      COUNT(c.id) AS used
    FROM routes r
    LEFT JOIN vehicle_route_assignments vra
      ON vra.route_id = r.id AND vra.ends_on IS NULL
    LEFT JOIN vehicles v
      ON v.id = vra.vehicle_id AND v.is_active = 1
    LEFT JOIN contacts c
      ON c.route_id = r.id AND c.is_active = 1
    WHERE r.is_active = 1
    GROUP BY r.id, r.name, v.code, v.capacity
    ORDER BY r.name
  `.pipe(Effect.orElseSucceed(() => []))
})

const renderForm = (
  authenticated: CookieAuthentication,
  request: Parameters<typeof renderUiPage>[0],
  contact: ReturnType<typeof contactData>,
  errors: Readonly<Record<string, string>>,
  isEdit: boolean,
  portalUrl = ""
) =>
  Effect.gen(function*() {
    const values = yield* routes
    return renderUiPage(
      request,
      "contacts/form",
      isEdit ? "Edit Contact" : "New Contact",
      {
        Contact: contact,
        Routes: values.map((route) => ({ ID: route.id, Name: route.name })),
        Errors: errors,
        IsEdit: isEdit,
        PortalURL: portalUrl
      },
      authenticated
    )
  })

const sortUrls = (
  url: string,
  currentSort: string,
  currentOrder: string
) => {
  const original = new URL(url, "http://localhost").searchParams
  const result: Record<string, string> = {}
  for (const column of ["name", "class", "route", "status"]) {
    const query = new URLSearchParams(original)
    query.set("sort", column)
    query.set(
      "order",
      currentSort === column && currentOrder !== "desc" ? "desc" : "asc"
    )
    query.sort()
    result[column] = `${dashboardBasePath}/contacts?${query.toString()}`
  }
  return result
}

const notFound = () => uiPlainError(404, "404 page not found")
const internal = () => uiPlainError(500, "Internal Server Error")

const addListRoute = HttpRouter.get(
  "/dashboard/contacts",
  protectedUiHandler((authenticated, request) => {
    const query = new URL(request.url, "http://localhost").searchParams
    const filter = query.get("type") ?? ""
    const search = query.get("search") ?? ""
    const sort = query.get("sort") ?? ""
    const order = query.get("order") ?? ""
    return Effect.gen(function*() {
      const contacts = yield* Contacts
      const [values, capacity] = yield* Effect.all([
        contacts.list({ type: filter, search, sort, order }),
        routeCapacity
      ])
      return renderUiPage(
        request,
        "contacts/index",
        "Contacts",
        {
          Contacts: values.map(contactData),
          RouteCapacity: capacity.map((item) => ({
            ID: item.id,
            RouteName: item.route_name,
            VehicleCode: item.vehicle_code,
            Capacity: item.capacity,
            Used: item.used
          })),
          Filter: filter,
          Search: search,
          Sort: sort,
          Order: order,
          SortURLs: sortUrls(request.url, sort, order)
        },
        authenticated
      )
    }).pipe(Effect.catchTag("ContactStoreError", () => Effect.succeed(internal())))
  })
)

const addNewRoute = HttpRouter.get(
  "/dashboard/contacts/new",
  protectedUiHandler((authenticated, request) =>
    renderForm(authenticated, request, contactData({
      name: "",
      contactType: "",
      phone: "",
      email: "",
      address: "",
      notes: "",
      mapsLink: "",
      className: "",
      distanceKm: 0,
      hasSiblingDiscount: false,
      isReturnOnly: false,
      routeId: 0,
      isActive: true
    }), {}, false)
  )
)

const addCreateRoute = HttpRouter.post(
  "/dashboard/contacts",
  protectedUiHandler<SqlClient.SqlClient | Audit | Contacts>(
    (authenticated, request, form) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    const values = valuesFromForm(form)
    const errors = validate(values)
    if (Object.keys(errors).length > 0) {
      return renderForm(
        authenticated,
        request,
        contactData(values),
        errors,
        false
      )
    }
    return Effect.gen(function*() {
      const contacts = yield* Contacts
      const created = yield* contacts.create({
        ...values,
        ...(values.routeId === 0 ? {} : { routeId: values.routeId })
      })
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "contact.create",
        actor: {
          id: authenticated.user.id,
          username: authenticated.user.username
        },
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
      return uiRedirect(`${dashboardBasePath}/contacts`, {
        "set-cookie": uiFlashCookie("Contact created successfully")
      })
    }).pipe(
      Effect.catchTag("ContactStoreError", () => Effect.succeed(internal()))
    )
    }
  )
)

const addEditRoute = (development: boolean) =>
  HttpRouter.get(
    "/dashboard/contacts/:id/edit",
    protectedUiHandler((authenticated, request) =>
      Effect.gen(function*() {
        const id = parseId((yield* HttpRouter.params).id)
        if (id === undefined) {
          return notFound()
        }
        const contacts = yield* Contacts
        const contact = yield* contacts.get(id)
        const portalUrl = contact.portal_code === undefined
          ? ""
          : `${development ? "http" : "https"}://` +
            `${new URL(request.url, "http://localhost").host}` +
            `/p/${contact.portal_code}`
        return yield* renderForm(
          authenticated,
          request,
          contactData(contact),
          {},
          true,
          portalUrl
        )
      }).pipe(
        Effect.catchTags({
          ContactNotFound: () => Effect.succeed(notFound()),
          ContactStoreError: () => Effect.succeed(notFound())
        })
      )
    )
  )

const addUpdateRoute = HttpRouter.post(
  "/dashboard/contacts/:id",
  protectedUiHandler((authenticated, request, form) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    return Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const contacts = yield* Contacts
      const existing = yield* contacts.get(id)
      const values = valuesFromForm(form)
      const errors = validate(values)
      if (Object.keys(errors).length > 0) {
        return yield* renderForm(
          authenticated,
          request,
          contactData({ id, ...values }),
          errors,
          true
        )
      }
      const updated = yield* contacts.update(id, {
        ...values,
        ...(values.routeId === 0 ? {} : { routeId: values.routeId })
      })
      const fields = [
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
        "route_id",
        "is_active"
      ]
      const before = {
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
        route_id: existing.route_id,
        is_active: existing.is_active
      }
      const after = {
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
        route_id: updated.route_id,
        is_active: updated.is_active
      }
      const metadata = auditDiff(before, after, fields)
      if (metadata !== undefined) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "contact.update",
          actor: {
            id: authenticated.user.id,
            username: authenticated.user.username
          },
          targetType: "contact",
          targetId: id,
          targetLabel: existing.name,
          metadata
        })
      }
      return uiRedirect(`${dashboardBasePath}/contacts`, {
        "set-cookie": uiFlashCookie("Contact updated successfully")
      })
    }).pipe(
      Effect.catchTags({
        ContactNotFound: () => Effect.succeed(notFound()),
        ContactStoreError: () => Effect.succeed(internal())
      })
    )
  })
)

const normalizePortalCode = (value: string) =>
  value.toLowerCase().replaceAll("-", "").replaceAll(" ", "")

const randomThreeDigits = () => {
  const limit = Math.floor(0x1_0000_0000 / 1000) * 1000
  const values = new Uint32Array(1)
  do {
    crypto.getRandomValues(values)
  } while ((values[0] ?? 0) >= limit)
  return String((values[0] ?? 0) % 1000).padStart(3, "0")
}

const generatedPrefix = (name: string) => {
  const first = name.trim().split(" ")[0] ?? ""
  const prefix = [...first.toLowerCase()]
    .filter((character) => character >= "a" && character <= "z")
    .join("")
    .slice(0, 12)
  return prefix || "lts"
}

const savePortalCode = (
  contactId: number,
  contactName: string,
  requested: string
) =>
  Effect.gen(function*() {
    const sql = yield* SqlClient.SqlClient
    let code = requested.toLowerCase().trim()
    if (normalizePortalCode(code) === "") {
      const prefix = generatedPrefix(contactName)
      for (let attempt = 0; attempt < 10; attempt += 1) {
        const candidate = `${prefix}-${randomThreeDigits()}`
        const updated = yield* sql`
          UPDATE contacts
          SET portal_code = ${candidate}
          WHERE id = ${contactId}
        `.pipe(Effect.either)
        if (updated._tag === "Right") {
          return candidate
        }
      }
      return yield* Effect.fail(
        new Error(`save portal code: too many collisions for prefix "${prefix}"`)
      )
    }
    if (![...code].every((character) =>
      (character >= "a" && character <= "z") ||
      (character >= "0" && character <= "9") ||
      character === "-"
    )) {
      return yield* Effect.fail(
        new Error(
          "portal code may only contain letters, numbers and dashes"
        )
      )
    }
    const normalized = normalizePortalCode(code)
    if (normalized.length < 4 || normalized.length > 32) {
      return yield* Effect.fail(
        new Error("portal code must be 4-32 characters")
      )
    }
    const duplicates = yield* sql<{ readonly id: number }>`
      SELECT id
      FROM contacts
      WHERE
        id <> ${contactId}
        AND portal_code IS NOT NULL
        AND portal_code <> ''
        AND LOWER(REPLACE(portal_code, '-', '')) = ${normalized}
    `
    if (duplicates.length > 0) {
      return yield* Effect.fail(new Error("portal_code_taken"))
    }
    yield* sql`
      UPDATE contacts
      SET portal_code = ${code}
      WHERE id = ${contactId}
    `
    return code
  })

const addPortalCodeRoute = HttpRouter.post(
  "/dashboard/contacts/:id/portal-code",
  protectedUiHandler((authenticated, request, form) => {
    if (authenticated.user.role !== "admin") {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    return Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const contacts = yield* Contacts
      const contact = yield* contacts.get(id)
      const result = yield* savePortalCode(
        id,
        contact.name,
        form.get("portal_code") ?? ""
      ).pipe(Effect.either)
      let message: string
      if (result._tag === "Left") {
        message = result.left.message === "portal_code_taken"
          ? "Link itu sudah dipakai kontak lain. Coba yang lain."
          : `Error: ${result.left.message}`
      } else {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "contact.portal_token_reset",
          actor: {
            id: authenticated.user.id,
            username: authenticated.user.username
          },
          targetType: "contact",
          targetId: id,
          targetLabel: contact.name,
          metadata: {
            before: contact.portal_code ?? "",
            after: result.right
          }
        })
        message = `Link portal berhasil disimpan: ${result.right}`
      }
      return uiRedirect(`${dashboardBasePath}/contacts/${id}/edit`, {
        "set-cookie": uiFlashCookie(message)
      })
    }).pipe(
      Effect.catchTags({
        ContactNotFound: () => Effect.succeed(notFound()),
        ContactStoreError: () => Effect.succeed(notFound())
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/dashboard/contacts/:id",
  protectedUiHandler((authenticated, request) => {
    if (!hasManage(authenticated)) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    return Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const contacts = yield* Contacts
      const removed = yield* contacts.remove(id)
      if (removed !== undefined) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "contact.delete",
          actor: {
            id: authenticated.user.id,
            username: authenticated.user.username
          },
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
      if (request.headers["hx-request"] === "true") {
        return HttpServerResponse.empty({ status: 200 })
      }
      return uiRedirect(`${dashboardBasePath}/contacts`, {
        "set-cookie": uiFlashCookie("Contact deleted successfully")
      })
    }).pipe(
      Effect.catchTags({
        ContactConflict: () => Effect.succeed(internal()),
        ContactStoreError: () => Effect.succeed(internal())
      })
    )
  })
)

export const addUiContactRoutes = (development: boolean) =>
  <E, R>(router: HttpRouter.HttpRouter<E, R>) =>
    router.pipe(
      addListRoute,
      addNewRoute,
      addCreateRoute,
      addEditRoute(development),
      addUpdateRoute,
      addPortalCodeRoute,
      addDeleteRoute
    )
