import { afterEach, describe, expect, test } from "bun:test"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect, Layer } from "effect"
import { sqliteDatabaseLayer } from "../../adapters/sqlite/database.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import {
  contactPrice,
  Contacts,
  ContactsLive,
  validateContact
} from "./contacts.ts"

const temporaryDirectories: Array<string> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-contacts-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const database = sqliteDatabaseLayer(databasePath)
  return Layer.merge(database, ContactsLive.pipe(Layer.provide(database)))
}

const values = {
  name: "Student",
  contactType: "customer",
  phone: "0812",
  email: "",
  address: "",
  notes: "",
  mapsLink: "",
  className: "3A",
  distanceKm: 7.5,
  hasSiblingDiscount: true,
  isReturnOnly: false,
  isActive: true
} as const

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("Contacts", () => {
  test("creates, filters, updates, and removes contacts", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const contacts = yield* Contacts
        const created = yield* contacts.create(values)
        const listed = yield* contacts.list({
          type: "customer",
          search: "Stud"
        })
        const updated = yield* contacts.update(created.id, {
          ...values,
          name: "Updated",
          distanceKm: 11.4
        })
        const removed = yield* contacts.remove(created.id)
        return { created, listed, updated, removed }
      }).pipe(Effect.provide(layer))
    )
    expect(result.created).toMatchObject({
      name: "Student",
      distance_km: 7.5,
      has_sibling_discount: true
    })
    expect(result.listed).toHaveLength(1)
    expect(result.updated.distance_km).toBe(11.4)
    expect(result.removed?.name).toBe("Updated")
  })

  test("deleting a missing contact remains a successful no-op", async () => {
    const layer = await setup()
    await expect(Effect.runPromise(
      Effect.gen(function*() {
        const contacts = yield* Contacts
        return yield* contacts.remove(999_999)
      }).pipe(Effect.provide(layer))
    )).resolves.toBeUndefined()
  })
})

describe("contact rules", () => {
  test("validates Unicode class length and distance", () => {
    expect(validateContact({
      name: "",
      contactType: "invalid",
      className: "😀😀😀😀😀😀",
      distanceKm: -1
    })).toEqual({
      name: "required",
      contact_type: "must be customer, supplier, or both",
      class: "must be 5 characters or fewer",
      distance_km: "must be 0 or greater"
    })
  })

  test("matches distance and discount pricing", () => {
    expect(contactPrice(3.9, false, false)).toBe(350_000)
    expect(contactPrice(7, true, true)).toBe(350_000)
    expect(contactPrice(13, false, false)).toBe(550_000)
  })
})
