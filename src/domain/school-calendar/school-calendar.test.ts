import { afterEach, describe, expect, test } from "bun:test"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect, Layer } from "effect"
import { sqliteDatabaseLayer } from "../../adapters/sqlite/database.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { PasswordHasherLive } from "../auth/password.ts"
import {
  monthlyPriceMultiplierPercent,
  SchoolCalendar,
  SchoolCalendarLive,
  validateSchoolClosure,
  type GoogleCalendarConfig
} from "./school-calendar.ts"

const temporaryDirectories: Array<string> = []
const originalFetch = globalThis.fetch

const setup = async (config: GoogleCalendarConfig = {
  clientId: "",
  clientSecret: "",
  redirectUrl: ""
}) => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-calendar-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const base = Layer.merge(
    sqliteDatabaseLayer(databasePath),
    PasswordHasherLive
  )
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(base)))
  return Layer.mergeAll(
    base,
    SchoolCalendarLive(config).pipe(Layer.provide(base))
  )
}

afterEach(async () => {
  globalThis.fetch = originalFetch
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("SchoolCalendar", () => {
  test("creates, filters, removes, and deduplicates effective closures", async () => {
    const layer = await setup()
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const calendar = yield* SchoolCalendar
        const previous = yield* calendar.createManual(
          "Previous",
          "2026-05-30",
          "2026-06-02"
        )
        yield* calendar.createManual(
          "Overlap A",
          "2026-06-01",
          "2026-06-03"
        )
        yield* calendar.createManual(
          "Overlap B",
          "2026-06-03",
          "2026-06-05"
        )
        yield* calendar.createManual(
          "Next",
          "2026-06-29",
          "2026-07-03"
        )
        const june = yield* calendar.list("2026-06")
        const days = yield* calendar.effectiveDays("2026-06")
        const removed = yield* calendar.remove(previous.id)
        return { june, days, removed }
      }).pipe(Effect.provide(layer))
    )

    expect(result.june).toHaveLength(4)
    expect(result.june[0]).toMatchObject({
      source: "manual",
      title: "Previous"
    })
    expect(result.june[0]?.google_event_id).toBeUndefined()
    expect(result.days).toBe(19)
    expect(result.removed.title).toBe("Previous")
  })

  test("refreshes OAuth and replaces Google closures during sync", async () => {
    let eventsRequests = 0
    globalThis.fetch = (async (input, init) => {
      const rawUrl = input instanceof Request ? input.url : String(input)
      const url = new URL(rawUrl)
      if (url.hostname === "oauth.test") {
        const form = new URLSearchParams(String(init?.body ?? ""))
        expect(form.get("grant_type")).toBe("refresh_token")
        expect(form.get("refresh_token")).toBe("refresh-token")
        return Response.json({
          access_token: "access-token",
          token_type: "Bearer",
          expires_in: 3600
        })
      }
      if (url.pathname === "/cal-primary/events") {
        eventsRequests += 1
        const headers = new Headers(init?.headers)
        expect(headers.get("authorization")).toBe("Bearer access-token")
        return Response.json({
          items: [
            {
              id: "all-day",
              summary: "Semester break",
              start: { date: "2026-06-10" },
              end: { date: "2026-06-13" }
            },
            {
              id: "timed",
              summary: "Overnight break",
              start: { dateTime: "2026-06-15T08:00:00+07:00" },
              end: { dateTime: "2026-06-17T00:00:00+07:00" }
            },
            {
              id: "cancelled",
              status: "cancelled",
              summary: "Cancelled",
              start: { date: "2026-06-01" },
              end: { date: "2026-06-02" }
            }
          ]
        })
      }
      return new Response("not found", { status: 404 })
    }) as typeof fetch
    const layer = await setup({
      clientId: "client-id",
      clientSecret: "client-secret",
      redirectUrl: "https://example.test/callback",
      tokenUrl: "https://oauth.test/token",
      eventsBaseUrl: "https://calendar.test"
    })
    const result = await Effect.runPromise(
      Effect.gen(function*() {
        const calendar = yield* SchoolCalendar
        const empty = yield* calendar.connection
        yield* calendar.saveConnection({
          ...empty,
          calendarId: "cal-primary",
          refreshToken: "refresh-token",
          isActive: true
        })
        yield* calendar.createManual(
          "Manual",
          "2026-06-01",
          "2026-06-02"
        )
        const synced = yield* calendar.sync
        const closures = yield* calendar.list("2026-06")
        const connection = yield* calendar.connection
        yield* calendar.disconnect
        const afterDisconnect = yield* calendar.list("")
        return { synced, closures, connection, afterDisconnect }
      }).pipe(Effect.provide(layer))
    )

    expect(eventsRequests).toBe(1)
    expect(result.synced).toMatchObject({ fetched: 3, stored: 2 })
    expect(result.closures.map((closure) => ({
      source: closure.source,
      title: closure.title,
      start: closure.start_date,
      end: closure.end_date
    }))).toEqual([
      {
        source: "manual",
        title: "Manual",
        start: "2026-06-01",
        end: "2026-06-02"
      },
      {
        source: "google",
        title: "Semester break",
        start: "2026-06-10",
        end: "2026-06-12"
      },
      {
        source: "google",
        title: "Overnight break",
        start: "2026-06-15",
        end: "2026-06-16"
      }
    ])
    expect(result.connection).toMatchObject({
      isActive: true,
      lastSyncStatus: "success",
      lastSyncError: ""
    })
    expect(result.connection.lastSyncAt).not.toBe("")
    expect(result.afterDisconnect.map((closure) => closure.title))
      .toEqual(["Manual"])
  })
})

describe("school calendar rules", () => {
  test("validates dates and maps multiplier thresholds", () => {
    expect(validateSchoolClosure({
      title: "",
      startDate: "2026-02-30",
      endDate: "2026-01-01"
    })).toEqual({
      title: "required",
      start_date: "must be YYYY-MM-DD"
    })
    expect([
      monthlyPriceMultiplierPercent(13),
      monthlyPriceMultiplierPercent(14),
      monthlyPriceMultiplierPercent(19),
      monthlyPriceMultiplierPercent(20)
    ]).toEqual([75, 85, 85, 100])
  })
})
