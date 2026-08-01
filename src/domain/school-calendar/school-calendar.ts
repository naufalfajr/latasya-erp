import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"

export type GoogleCalendarConfig = {
  readonly clientId: string
  readonly clientSecret: string
  readonly redirectUrl: string
  readonly authUrl?: string
  readonly tokenUrl?: string
  readonly eventsBaseUrl?: string
}

export type SchoolClosure = {
  readonly id: number
  readonly source: "manual" | "google"
  readonly title: string
  readonly start_date: string
  readonly end_date: string
  readonly google_event_id?: string
  readonly created_at: string
  readonly updated_at: string
}

export type GoogleCalendarConnection = {
  readonly id: number
  readonly calendarId: string
  readonly refreshToken: string
  readonly isActive: boolean
  readonly lastSyncAt: string
  readonly lastSyncStatus: string
  readonly lastSyncError: string
  readonly createdAt: string
  readonly updatedAt: string
}

export type GoogleSyncResult = {
  readonly fetched: number
  readonly stored: number
  readonly window_start: string
  readonly window_end: string
}

export class SchoolClosureNotFound extends Data.TaggedError(
  "SchoolClosureNotFound"
) {}

export class SchoolCalendarStoreError extends Data.TaggedError(
  "SchoolCalendarStoreError"
)<{
  readonly cause: unknown
}> {}

export class GoogleCalendarError extends Data.TaggedError(
  "GoogleCalendarError"
)<{
  readonly message: string
}> {}

export interface SchoolCalendar {
  readonly list: (
    month: string
  ) => Effect.Effect<ReadonlyArray<SchoolClosure>, SchoolCalendarStoreError>
  readonly get: (
    id: number
  ) => Effect.Effect<
    SchoolClosure,
    SchoolClosureNotFound | SchoolCalendarStoreError
  >
  readonly createManual: (
    title: string,
    startDate: string,
    endDate: string
  ) => Effect.Effect<SchoolClosure, SchoolCalendarStoreError>
  readonly remove: (
    id: number
  ) => Effect.Effect<
    SchoolClosure,
    SchoolClosureNotFound | SchoolCalendarStoreError
  >
  readonly effectiveDays: (
    month: string
  ) => Effect.Effect<number, SchoolCalendarStoreError>
  readonly connection: Effect.Effect<
    GoogleCalendarConnection,
    SchoolCalendarStoreError
  >
  readonly saveConnection: (
    connection: GoogleCalendarConnection
  ) => Effect.Effect<void, SchoolCalendarStoreError>
  readonly disconnect: Effect.Effect<void, SchoolCalendarStoreError>
  readonly configEnabled: boolean
  readonly beginOAuth: (
    userId: number
  ) => Effect.Effect<string, SchoolCalendarStoreError>
  readonly completeOAuth: (
    userId: number,
    state: string,
    code: string
  ) => Effect.Effect<void, GoogleCalendarError | SchoolCalendarStoreError>
  readonly sync: Effect.Effect<
    GoogleSyncResult,
    GoogleCalendarError | SchoolCalendarStoreError
  >
}

export const SchoolCalendar = Context.GenericTag<SchoolCalendar>(
  "latasya/SchoolCalendar"
)

type ClosureRow = {
  readonly id: number
  readonly source: "manual" | "google"
  readonly title: string
  readonly start_date: string
  readonly end_date: string
  readonly google_event_id: string
  readonly created_at: string
  readonly updated_at: string
}

type ConnectionRow = {
  readonly id: number
  readonly calendar_id: string
  readonly refresh_token: string
  readonly is_active: number
  readonly last_sync_at: string
  readonly last_sync_status: string
  readonly last_sync_error: string
  readonly created_at: string
  readonly updated_at: string
}

type GoogleEvent = {
  readonly id?: unknown
  readonly status?: unknown
  readonly summary?: unknown
  readonly start?: {
    readonly date?: unknown
    readonly dateTime?: unknown
  }
  readonly end?: {
    readonly date?: unknown
    readonly dateTime?: unknown
  }
}

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new SchoolCalendarStoreError({ cause }))
  )

const fromRow = (row: ClosureRow): SchoolClosure => ({
  id: row.id,
  source: row.source,
  title: row.title,
  start_date: row.start_date,
  end_date: row.end_date,
  ...(row.google_event_id === ""
    ? {}
    : { google_event_id: row.google_event_id }),
  created_at: row.created_at,
  updated_at: row.updated_at
})

const pad2 = (value: number) => String(value).padStart(2, "0")

const formatUtcDate = (date: Date) =>
  `${date.getUTCFullYear()}-${pad2(date.getUTCMonth() + 1)}` +
  `-${pad2(date.getUTCDate())}`

const parseDateOnly = (value: string): Date | undefined => {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (match === null) {
    return undefined
  }
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const date = new Date(Date.UTC(year, month - 1, day))
  if (
    date.getUTCFullYear() !== year ||
    date.getUTCMonth() !== month - 1 ||
    date.getUTCDate() !== day
  ) {
    return undefined
  }
  return date
}

export const validSchoolMonth = (value: string) => {
  const match = /^(\d{4})-(\d{2})$/.exec(value)
  if (match === null) {
    return false
  }
  const month = Number(match[2])
  return month >= 1 && month <= 12
}

export const validateSchoolClosure = (input: {
  readonly title: string
  readonly startDate: string
  readonly endDate: string
}) => {
  const fields: Record<string, string> = {}
  if (input.title === "") {
    fields.title = "required"
  }
  const start = parseDateOnly(input.startDate)
  if (start === undefined) {
    fields.start_date = "must be YYYY-MM-DD"
  }
  const end = parseDateOnly(input.endDate)
  if (end === undefined) {
    fields.end_date = "must be YYYY-MM-DD"
  }
  if (
    start !== undefined &&
    end !== undefined &&
    start.getTime() > end.getTime()
  ) {
    fields.end_date = "must be on or after start_date"
  }
  return fields
}

export const monthlyPriceMultiplierPercent = (days: number) =>
  days < 14 ? 75 : days < 20 ? 85 : 100

const randomBase64Url = (bytes: number) => {
  const value = crypto.getRandomValues(new Uint8Array(bytes))
  return Buffer.from(value).toString("base64url")
}

const sha256Base64Url = (value: string) =>
  new Bun.CryptoHasher("sha256").update(value).digest("base64url")

const sqliteTimestamp = (date: Date) =>
  date.toISOString().slice(0, 19).replace("T", " ")

const errorMessage = (cause: unknown) =>
  cause instanceof Error ? cause.message : String(cause)

const make = (config: GoogleCalendarConfig) =>
  Effect.gen(function*() {
    const sql = yield* SqlClient.SqlClient
    const enabled =
      config.clientId !== "" &&
      config.clientSecret !== "" &&
      config.redirectUrl !== ""
    const authUrl =
      config.authUrl ?? "https://accounts.google.com/o/oauth2/v2/auth"
    const tokenUrl =
      config.tokenUrl ?? "https://oauth2.googleapis.com/token"
    const eventsBaseUrl =
      config.eventsBaseUrl ??
      "https://www.googleapis.com/calendar/v3/calendars"

    const closureRows = (query: string, params: ReadonlyArray<unknown>) =>
      store(sql.unsafe<ClosureRow>(query, params)).pipe(
        Effect.map((rows) => rows.map(fromRow))
      )

    const get: SchoolCalendar["get"] = (id) =>
      Effect.gen(function*() {
        const rows = yield* closureRows(`
          SELECT
            id,
            source,
            title,
            start_date,
            end_date,
            COALESCE(google_event_id, '') AS google_event_id,
            created_at,
            updated_at
          FROM school_closures
          WHERE id = ?
        `, [id])
        const closure = rows[0]
        if (closure === undefined) {
          return yield* new SchoolClosureNotFound()
        }
        return closure
      })

    const list: SchoolCalendar["list"] = (month) => {
      let query = `
        SELECT
          id,
          source,
          title,
          start_date,
          end_date,
          COALESCE(google_event_id, '') AS google_event_id,
          created_at,
          updated_at
        FROM school_closures
      `
      const params: Array<unknown> = []
      if (month !== "") {
        const [year, monthNumber] = month.split("-").map(Number)
        const start = `${month}-01`
        const end = formatUtcDate(new Date(Date.UTC(
          year as number,
          monthNumber as number,
          0
        )))
        query += " WHERE start_date <= ? AND end_date >= ?"
        params.push(end, start)
      }
      query += " ORDER BY start_date, id"
      return closureRows(query, params)
    }

    const createManual: SchoolCalendar["createManual"] = (
      title,
      startDate,
      endDate
    ) =>
      Effect.gen(function*() {
        yield* store(sql`
          INSERT INTO school_closures (
            source,
            title,
            start_date,
            end_date,
            google_event_id
          )
          VALUES ('manual', ${title}, ${startDate}, ${endDate}, NULL)
        `)
        const ids = yield* store(sql<{ readonly id: number }>`
          SELECT last_insert_rowid() AS id
        `)
        return yield* get(ids[0]?.id ?? 0).pipe(
          Effect.catchTag(
            "SchoolClosureNotFound",
            (cause) => new SchoolCalendarStoreError({ cause })
          )
        )
      })

    const remove: SchoolCalendar["remove"] = (id) =>
      Effect.gen(function*() {
        const existing = yield* get(id)
        yield* store(sql`DELETE FROM school_closures WHERE id = ${id}`)
        return existing
      })

    const effectiveDays: SchoolCalendar["effectiveDays"] = (month) =>
      Effect.gen(function*() {
        const [year, monthNumber] = month.split("-").map(Number)
        const start = new Date(Date.UTC(
          year as number,
          (monthNumber as number) - 1,
          1
        ))
        const end = new Date(Date.UTC(
          year as number,
          monthNumber as number,
          0
        ))
        const schoolDays = new Set<string>()
        for (
          let day = start;
          day.getTime() <= end.getTime();
          day = new Date(day.getTime() + 24 * 60 * 60 * 1000)
        ) {
          if (day.getUTCDay() !== 0) {
            schoolDays.add(formatUtcDate(day))
          }
        }
        const closures = yield* store(sql<{
          readonly start_date: string
          readonly end_date: string
        }>`
          SELECT start_date, end_date
          FROM school_closures
          WHERE
            start_date <= ${formatUtcDate(end)}
            AND end_date >= ${formatUtcDate(start)}
        `)
        for (const closure of closures) {
          const rawStart = parseDateOnly(closure.start_date)
          const rawEnd = parseDateOnly(closure.end_date)
          if (rawStart === undefined || rawEnd === undefined) {
            return yield* new SchoolCalendarStoreError({
              cause: new Error("invalid school closure date")
            })
          }
          const closureStart = rawStart.getTime() < start.getTime()
            ? start
            : rawStart
          const closureEnd = rawEnd.getTime() > end.getTime()
            ? end
            : rawEnd
          for (
            let day = closureStart;
            day.getTime() <= closureEnd.getTime();
            day = new Date(day.getTime() + 24 * 60 * 60 * 1000)
          ) {
            schoolDays.delete(formatUtcDate(day))
          }
        }
        return schoolDays.size
      })

    const connection: SchoolCalendar["connection"] =
      Effect.gen(function*() {
        const rows = yield* store(sql<ConnectionRow>`
          SELECT
            id,
            calendar_id,
            refresh_token,
            is_active,
            COALESCE(last_sync_at, '') AS last_sync_at,
            last_sync_status,
            last_sync_error,
            created_at,
            updated_at
          FROM google_calendar_connections
          WHERE id = 1
        `)
        const row = rows[0]
        return row === undefined
          ? {
            id: 1,
            calendarId: "",
            refreshToken: "",
            isActive: false,
            lastSyncAt: "",
            lastSyncStatus: "",
            lastSyncError: "",
            createdAt: "",
            updatedAt: ""
          }
          : {
            id: row.id,
            calendarId: row.calendar_id,
            refreshToken: row.refresh_token,
            isActive: row.is_active !== 0,
            lastSyncAt: row.last_sync_at,
            lastSyncStatus: row.last_sync_status,
            lastSyncError: row.last_sync_error,
            createdAt: row.created_at,
            updatedAt: row.updated_at
          }
      })

    const saveConnection: SchoolCalendar["saveConnection"] = (value) =>
      store(sql`
        INSERT INTO google_calendar_connections (
          id,
          calendar_id,
          refresh_token,
          is_active,
          last_sync_at,
          last_sync_status,
          last_sync_error,
          updated_at
        )
        VALUES (
          1,
          ${value.calendarId},
          ${value.refreshToken},
          ${value.isActive ? 1 : 0},
          ${value.lastSyncAt === "" ? null : value.lastSyncAt},
          ${value.lastSyncStatus},
          ${value.lastSyncError},
          datetime('now')
        )
        ON CONFLICT(id) DO UPDATE SET
          calendar_id = excluded.calendar_id,
          refresh_token = excluded.refresh_token,
          is_active = excluded.is_active,
          last_sync_at = excluded.last_sync_at,
          last_sync_status = excluded.last_sync_status,
          last_sync_error = excluded.last_sync_error,
          updated_at = datetime('now')
      `).pipe(Effect.asVoid)

    const disconnect = store(sql.withTransaction(
      Effect.gen(function*() {
        yield* sql`DELETE FROM google_calendar_connections WHERE id = 1`
        yield* sql`
          DELETE FROM school_closures WHERE source = 'google'
        `
      })
    ))

    const updateSyncStatus = (status: string, syncError: string) =>
      store(sql`
        UPDATE google_calendar_connections
        SET
          last_sync_at = COALESCE(
            ${status === "success"
              ? new Date().toISOString().slice(0, 19) + "Z"
              : null},
            last_sync_at
          ),
          last_sync_status = ${status},
          last_sync_error = ${syncError},
          updated_at = datetime('now')
        WHERE id = 1
      `).pipe(Effect.asVoid)

    const beginOAuth: SchoolCalendar["beginOAuth"] = (userId) =>
      Effect.gen(function*() {
        const state = randomBase64Url(32)
        const verifier = randomBase64Url(32)
        const expiresAt = new Date(
          Date.now() + 10 * 60 * 1000
        ).toISOString()
        yield* store(sql`
          INSERT INTO google_oauth_states (
            state,
            user_id,
            pkce_verifier,
            expires_at
          )
          VALUES (${state}, ${userId}, ${verifier}, ${expiresAt})
        `)
        const url = new URL(authUrl)
        url.searchParams.set("client_id", config.clientId)
        url.searchParams.set("redirect_uri", config.redirectUrl)
        url.searchParams.set("response_type", "code")
        url.searchParams.set(
          "scope",
          "https://www.googleapis.com/auth/calendar.events.readonly"
        )
        url.searchParams.set("access_type", "offline")
        url.searchParams.set("prompt", "consent")
        url.searchParams.set("state", state)
        url.searchParams.set("code_challenge", sha256Base64Url(verifier))
        url.searchParams.set("code_challenge_method", "S256")
        return url.toString()
      }).pipe(
        Effect.catchAll((cause) =>
          cause instanceof SchoolCalendarStoreError
            ? Effect.fail(cause)
            : Effect.fail(new SchoolCalendarStoreError({ cause }))
        )
      )

    const consumeOAuthState = (state: string, userId: number) =>
      sql.withTransaction(
        Effect.gen(function*() {
          const rows = yield* sql<{
            readonly pkce_verifier: string
          }>`
            SELECT pkce_verifier
            FROM google_oauth_states
            WHERE
              state = ${state}
              AND user_id = ${userId}
              AND datetime(expires_at) > datetime('now')
          `
          const row = rows[0]
          if (row === undefined) {
            return yield* new GoogleCalendarError({
              message: "google calendar connection expired"
            })
          }
          yield* sql`
            DELETE FROM google_oauth_states WHERE state = ${state}
          `
          return row.pkce_verifier
        })
      ).pipe(Effect.mapError((cause) =>
        cause instanceof GoogleCalendarError
          ? cause
          : new GoogleCalendarError({
            message: "google calendar state validation failed"
          })
      ))

    const tokenRequest = (
      values: Readonly<Record<string, string>>
    ) => Effect.tryPromise({
      try: async () => {
        const response = await fetch(tokenUrl, {
          method: "POST",
          headers: {
            "content-type": "application/x-www-form-urlencoded"
          },
          body: new URLSearchParams(values)
        })
        const body: unknown = await response.json()
        if (
          !response.ok ||
          typeof body !== "object" ||
          body === null
        ) {
          throw new Error(`google oauth token status ${response.status}`)
        }
        return body as Readonly<Record<string, unknown>>
      },
      catch: (cause) =>
        new GoogleCalendarError({ message: errorMessage(cause) })
    })

    const completeOAuth: SchoolCalendar["completeOAuth"] = (
      userId,
      state,
      code
    ) =>
      Effect.gen(function*() {
        if (!enabled) {
          return yield* new GoogleCalendarError({
            message: "google calendar oauth is not configured"
          })
        }
        const verifier = yield* consumeOAuthState(state, userId)
        const token = yield* tokenRequest({
          grant_type: "authorization_code",
          code,
          redirect_uri: config.redirectUrl,
          client_id: config.clientId,
          client_secret: config.clientSecret,
          code_verifier: verifier
        })
        const existing = yield* connection.pipe(Effect.mapError(() =>
          new GoogleCalendarError({
            message: "google calendar settings load failed"
          })
        ))
        const refreshToken = typeof token.refresh_token === "string"
          ? token.refresh_token
          : existing.refreshToken
        if (refreshToken === "") {
          return yield* new GoogleCalendarError({
            message: "google did not return a refresh token"
          })
        }
        yield* saveConnection({
          ...existing,
          refreshToken,
          isActive: true,
          lastSyncStatus: "",
          lastSyncError: ""
        }).pipe(Effect.mapError(() =>
          new GoogleCalendarError({
            message: "google calendar connection save failed"
          })
        ))
      })

    const convertEvent = (
      event: GoogleEvent
    ): {
      readonly title: string
      readonly startDate: string
      readonly endDate: string
      readonly googleEventId: string
    } | undefined => {
      if (
        event.status === "cancelled" ||
        typeof event.id !== "string" ||
        event.id === "" ||
        typeof event.summary !== "string" ||
        event.summary.trim() === ""
      ) {
        return undefined
      }
      const startDate = event.start?.date
      const endDate = event.end?.date
      if (typeof startDate === "string" && typeof endDate === "string") {
        const start = parseDateOnly(startDate)
        const exclusiveEnd = parseDateOnly(endDate)
        if (start === undefined || exclusiveEnd === undefined) {
          return undefined
        }
        const end = new Date(
          exclusiveEnd.getTime() - 24 * 60 * 60 * 1000
        )
        if (end.getTime() < start.getTime()) {
          return undefined
        }
        return {
          title: event.summary,
          startDate: formatUtcDate(start),
          endDate: formatUtcDate(end),
          googleEventId: event.id
        }
      }
      const startText = event.start?.dateTime
      const endText = event.end?.dateTime
      if (
        typeof startText !== "string" ||
        typeof endText !== "string"
      ) {
        return undefined
      }
      const startInstant = Date.parse(startText)
      const endInstant = Date.parse(endText)
      if (Number.isNaN(startInstant) || Number.isNaN(endInstant)) {
        return undefined
      }
      const startLocal = new Date(startInstant + 7 * 60 * 60 * 1000)
      let endLocal = new Date(endInstant + 7 * 60 * 60 * 1000)
      if (
        endLocal.getUTCHours() === 0 &&
        endLocal.getUTCMinutes() === 0 &&
        endLocal.getUTCSeconds() === 0 &&
        endLocal.getUTCMilliseconds() === 0
      ) {
        endLocal = new Date(endLocal.getTime() - 24 * 60 * 60 * 1000)
      }
      if (endLocal.getTime() < startLocal.getTime()) {
        return undefined
      }
      return {
        title: event.summary,
        startDate: formatUtcDate(startLocal),
        endDate: formatUtcDate(endLocal),
        googleEventId: event.id
      }
    }

    const sync: SchoolCalendar["sync"] =
      Effect.gen(function*() {
        const current = yield* connection
        if (
          !enabled ||
          !current.isActive ||
          current.refreshToken === "" ||
          current.calendarId === ""
        ) {
          const message = "google calendar is not connected"
          yield* updateSyncStatus("error", message)
          return yield* new GoogleCalendarError({ message })
        }
        const token = yield* tokenRequest({
          grant_type: "refresh_token",
          refresh_token: current.refreshToken,
          client_id: config.clientId,
          client_secret: config.clientSecret
        }).pipe(
          Effect.tapError((error) =>
            updateSyncStatus("error", error.message)
          )
        )
        if (typeof token.access_token !== "string") {
          const error = new GoogleCalendarError({
            message: "google oauth token response missing access token"
          })
          yield* updateSyncStatus("error", error.message)
          return yield* error
        }

        const now = new Date()
        const start = new Date(now)
        start.setFullYear(start.getFullYear() - 1)
        const end = new Date(now)
        end.setFullYear(end.getFullYear() + 1)
        end.setMonth(end.getMonth() + 6)
        const windowStart =
          `${start.getFullYear()}-${pad2(start.getMonth() + 1)}` +
          `-${pad2(start.getDate())}`
        const windowEnd =
          `${end.getFullYear()}-${pad2(end.getMonth() + 1)}` +
          `-${pad2(end.getDate())}`

        const events: Array<GoogleEvent> = []
        let pageToken = ""
        do {
          const url = new URL(
            `${eventsBaseUrl}/${encodeURIComponent(current.calendarId)}/events`
          )
          url.searchParams.set("timeMin", start.toISOString())
          url.searchParams.set("timeMax", end.toISOString())
          url.searchParams.set("singleEvents", "true")
          url.searchParams.set("showDeleted", "false")
          url.searchParams.set("orderBy", "startTime")
          url.searchParams.set("maxResults", "250")
          if (pageToken !== "") {
            url.searchParams.set("pageToken", pageToken)
          }
          const body = yield* Effect.tryPromise({
            try: async () => {
              const response = await fetch(url, {
                headers: {
                  authorization: `Bearer ${token.access_token}`
                }
              })
              const value: unknown = await response.json()
              if (!response.ok) {
                throw new Error(
                  `fetch google calendar events: status ${response.status}`
                )
              }
              if (typeof value !== "object" || value === null) {
                throw new Error("decode google calendar events")
              }
              return value as Readonly<Record<string, unknown>>
            },
            catch: (cause) =>
              new GoogleCalendarError({ message: errorMessage(cause) })
          }).pipe(
            Effect.tapError((error) =>
              updateSyncStatus("error", error.message)
            )
          )
          if (Array.isArray(body.items)) {
            events.push(...body.items as Array<GoogleEvent>)
          }
          pageToken = typeof body.nextPageToken === "string"
            ? body.nextPageToken
            : ""
        } while (pageToken !== "")

        const closures = events.flatMap((event) => {
          const converted = convertEvent(event)
          return converted === undefined ? [] : [converted]
        })
        yield* store(sql.withTransaction(
          Effect.gen(function*() {
            yield* sql`
              DELETE FROM school_closures
              WHERE
                source = 'google'
                AND start_date <= ${windowEnd}
                AND end_date >= ${windowStart}
            `
            for (const closure of closures) {
              yield* sql`
                INSERT INTO school_closures (
                  source,
                  title,
                  start_date,
                  end_date,
                  google_event_id
                )
                VALUES (
                  'google',
                  ${closure.title},
                  ${closure.startDate},
                  ${closure.endDate},
                  ${closure.googleEventId}
                )
              `
            }
          })
        )).pipe(
          Effect.tapError((error) =>
            updateSyncStatus("error", errorMessage(error.cause))
          )
        )
        yield* updateSyncStatus("success", "")
        return {
          fetched: events.length,
          stored: closures.length,
          window_start: windowStart,
          window_end: windowEnd
        }
      })

    return SchoolCalendar.of({
      list,
      get,
      createManual,
      remove,
      effectiveDays,
      connection,
      saveConnection,
      disconnect,
      configEnabled: enabled,
      beginOAuth,
      completeOAuth,
      sync
    })
  })

export const SchoolCalendarLive = (
  config: GoogleCalendarConfig
) => Layer.effect(SchoolCalendar, make(config))
