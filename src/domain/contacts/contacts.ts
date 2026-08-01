import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"

export type Contact = {
  readonly id: number
  readonly name: string
  readonly contact_type: string
  readonly phone: string
  readonly email: string
  readonly address: string
  readonly notes: string
  readonly maps_link: string
  readonly class: string
  readonly distance_km: number
  readonly has_sibling_discount: boolean
  readonly is_return_only: boolean
  readonly route_id: number
  readonly is_active: boolean
  readonly created_at: string
  readonly updated_at: string
  readonly route_name?: string
  readonly portal_code?: string
}

type ContactRow = Omit<
  Contact,
  "has_sibling_discount" | "is_return_only" | "is_active" | "route_name"
> & {
  readonly has_sibling_discount: number
  readonly is_return_only: number
  readonly is_active: number
  readonly route_name?: string
}

export type ContactValues = {
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
  readonly isActive: boolean
  readonly routeId?: number
}

export type ContactFilter = {
  readonly type: string
  readonly search: string
  readonly sort?: string
  readonly order?: string
}

export class ContactNotFound extends Data.TaggedError("ContactNotFound") {}

export class ContactConflict extends Data.TaggedError("ContactConflict")<{
  readonly entity: "invoice" | "bill"
  readonly count: number
}> {}

export class ContactStoreError extends Data.TaggedError("ContactStoreError")<{
  readonly cause: unknown
}> {}

export interface Contacts {
  readonly list: (
    filter: ContactFilter
  ) => Effect.Effect<ReadonlyArray<Contact>, ContactStoreError>
  readonly get: (
    id: number
  ) => Effect.Effect<Contact, ContactNotFound | ContactStoreError>
  readonly create: (
    values: ContactValues
  ) => Effect.Effect<Contact, ContactStoreError>
  readonly update: (
    id: number,
    values: ContactValues
  ) => Effect.Effect<Contact, ContactNotFound | ContactStoreError>
  readonly remove: (
    id: number
  ) => Effect.Effect<
    Contact | undefined,
    ContactConflict | ContactStoreError
  >
}

export const Contacts = Context.GenericTag<Contacts>("latasya/Contacts")

const listColumns = `
  c.id,
  c.name,
  c.contact_type,
  COALESCE(c.phone, '') AS phone,
  COALESCE(c.email, '') AS email,
  COALESCE(c.address, '') AS address,
  COALESCE(c.notes, '') AS notes,
  c.maps_link,
  c.class,
  c.distance_km,
  c.has_sibling_discount,
  c.is_return_only,
  COALESCE(c.route_id, 0) AS route_id,
  c.is_active,
  c.created_at,
  c.updated_at,
  COALESCE(r.name, '') AS route_name
`

const getColumns = `
  id,
  name,
  contact_type,
  COALESCE(phone, '') AS phone,
  COALESCE(email, '') AS email,
  COALESCE(address, '') AS address,
  COALESCE(notes, '') AS notes,
  maps_link,
  class,
  distance_km,
  has_sibling_discount,
  is_return_only,
  COALESCE(route_id, 0) AS route_id,
  is_active,
  created_at,
  updated_at,
  COALESCE(portal_code, '') AS portal_code
`

const fromRow = (row: ContactRow): Contact => ({
  id: row.id,
  name: row.name,
  contact_type: row.contact_type,
  phone: row.phone,
  email: row.email,
  address: row.address,
  notes: row.notes,
  maps_link: row.maps_link,
  class: row.class,
  distance_km: row.distance_km,
  has_sibling_discount: row.has_sibling_discount !== 0,
  is_return_only: row.is_return_only !== 0,
  route_id: row.route_id,
  is_active: row.is_active !== 0,
  created_at: row.created_at,
  updated_at: row.updated_at,
  ...(row.route_name === undefined || row.route_name === ""
    ? {}
    : { route_name: row.route_name }),
  ...(row.portal_code === undefined || row.portal_code === ""
    ? {}
    : { portal_code: row.portal_code })
})

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new ContactStoreError({ cause }))
  )

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient

  const get: Contacts["get"] = (id) =>
    Effect.gen(function*() {
      const rows = yield* store(sql.unsafe<ContactRow>(
        `SELECT ${getColumns} FROM contacts WHERE id = ?`,
        [id]
      ))
      const row = rows[0]
      if (row === undefined) {
        return yield* new ContactNotFound()
      }
      return fromRow(row)
    })

  const list: Contacts["list"] = (filter) => {
    let query = `
      SELECT ${listColumns}
      FROM contacts c
      LEFT JOIN routes r ON r.id = c.route_id
      WHERE 1 = 1
    `
    const params: Array<unknown> = []
    if (filter.type !== "") {
      query += " AND (c.contact_type = ? OR c.contact_type = 'both')"
      params.push(filter.type)
    }
    if (filter.search !== "") {
      query += " AND (c.name LIKE ? OR c.phone LIKE ? OR c.email LIKE ?)"
      const search = `%${filter.search}%`
      params.push(search, search, search)
    }
    const column = filter.sort === "class"
      ? "c.class"
      : filter.sort === "route"
      ? "COALESCE(r.name, '')"
      : filter.sort === "status"
      ? "c.is_active"
      : "c.name"
    const direction = filter.order === "desc" ? "DESC" : "ASC"
    query += ` ORDER BY ${column} ${direction}, c.name ASC`
    return store(sql.unsafe<ContactRow>(query, params)).pipe(
      Effect.map((rows) => rows.map(fromRow))
    )
  }

  const create: Contacts["create"] = (values) =>
    Effect.gen(function*() {
      yield* store(sql`
        INSERT INTO contacts (
          name,
          contact_type,
          phone,
          email,
          address,
          notes,
          maps_link,
          class,
          distance_km,
          has_sibling_discount,
          is_return_only,
          route_id,
          is_active
        )
        VALUES (
          ${values.name},
          ${values.contactType},
          ${values.phone},
          ${values.email},
          ${values.address},
          ${values.notes},
          ${values.mapsLink},
          ${values.className},
          ${values.distanceKm},
          ${values.hasSiblingDiscount ? 1 : 0},
          ${values.isReturnOnly ? 1 : 0},
          ${values.routeId ?? null},
          ${values.isActive ? 1 : 0}
        )
      `)
      const ids = yield* store(sql<{ readonly id: number }>`
        SELECT last_insert_rowid() AS id
      `)
      return yield* get(ids[0]?.id ?? 0).pipe(
        Effect.catchTag(
          "ContactNotFound",
          (cause) => new ContactStoreError({ cause })
        )
      )
    })

  const update: Contacts["update"] = (id, values) =>
    Effect.gen(function*() {
      yield* get(id)
      yield* store(sql`
        UPDATE contacts
        SET
          name = ${values.name},
          contact_type = ${values.contactType},
          phone = ${values.phone},
          email = ${values.email},
          address = ${values.address},
          notes = ${values.notes},
          maps_link = ${values.mapsLink},
          class = ${values.className},
          distance_km = ${values.distanceKm},
          has_sibling_discount = ${values.hasSiblingDiscount ? 1 : 0},
          is_return_only = ${values.isReturnOnly ? 1 : 0},
          route_id = ${values.routeId ?? null},
          is_active = ${values.isActive ? 1 : 0},
          updated_at = datetime('now')
        WHERE id = ${id}
      `)
      return yield* get(id)
    })

  const remove: Contacts["remove"] = (id) =>
    Effect.gen(function*() {
      const existing = yield* get(id).pipe(
        Effect.catchTag("ContactNotFound", () => Effect.succeed(undefined))
      )
      const invoices = yield* store(sql<{ readonly count: number }>`
        SELECT COUNT(*) AS count
        FROM invoices
        WHERE contact_id = ${id}
      `)
      const invoiceCount = invoices[0]?.count ?? 0
      if (invoiceCount > 0) {
        return yield* new ContactConflict({
          entity: "invoice",
          count: invoiceCount
        })
      }
      const bills = yield* store(sql<{ readonly count: number }>`
        SELECT COUNT(*) AS count
        FROM bills
        WHERE contact_id = ${id}
      `)
      const billCount = bills[0]?.count ?? 0
      if (billCount > 0) {
        return yield* new ContactConflict({
          entity: "bill",
          count: billCount
        })
      }
      yield* store(sql`DELETE FROM contacts WHERE id = ${id}`)
      return existing
    })

  return Contacts.of({ list, get, create, update, remove })
})

export const ContactsLive = Layer.effect(Contacts, make)

export const validateContact = (input: {
  readonly name: string
  readonly contactType: string
  readonly className: string
  readonly distanceKm: number
}): Readonly<Record<string, string>> => {
  const fields: Record<string, string> = {}
  if (input.name === "") {
    fields.name = "required"
  }
  if (input.contactType === "") {
    fields.contact_type = "required"
  } else if (!["customer", "supplier", "both"].includes(input.contactType)) {
    fields.contact_type = "must be customer, supplier, or both"
  }
  if ([...input.className].length > 5) {
    fields.class = "must be 5 characters or fewer"
  }
  if (input.distanceKm < 0) {
    fields.distance_km = "must be 0 or greater"
  }
  return fields
}

export const contactPrice = (
  distanceKm: number,
  hasSiblingDiscount: boolean,
  isReturnOnly: boolean
) => {
  const price = distanceKm < 4
    ? 350_000
    : distanceKm < 7
    ? 400_000
    : distanceKm < 10
    ? 450_000
    : distanceKm < 13
    ? 500_000
    : 550_000
  return price -
    (hasSiblingDiscount ? 50_000 : 0) -
    (isReturnOnly ? 50_000 : 0)
}
