import { HttpRouter, HttpServerResponse } from "@effect/platform"
import { SqlClient } from "@effect/sql"
import { Effect } from "effect"
import { Accounts } from "../../domain/accounting/accounts.ts"
import { Audit } from "../../domain/audit/audit.ts"
import { AuditLog } from "../../domain/audit/audit-log.ts"
import { ApiTokens } from "../../domain/auth/api-tokens.ts"
import type { CookieAuthentication } from "../../domain/auth/authentication.ts"
import { allCapabilities } from "../../domain/auth/capability.ts"
import { CompanyProfiles } from "../../domain/company/profile.ts"
import {
  monthlyPriceMultiplierPercent,
  SchoolCalendar,
  validSchoolMonth,
  validateSchoolClosure
} from "../../domain/school-calendar/school-calendar.ts"
import {
  dashboardBasePath,
  protectedUiHandler,
  renderUiPage,
  uiFlashCookie,
  uiPlainError,
  uiRedirect
} from "./ui-auth.ts"
import { requestMetadata } from "./request-metadata.ts"

const actor = (auth: CookieAuthentication) => ({
  id: auth.user.id,
  username: auth.user.username
})
const admin = (auth: CookieAuthentication) => auth.user.role === "admin"
const parseId = (value: string | undefined) => {
  const parsed = Number(value)
  return value !== undefined && /^[+-]?\d+$/.test(value) &&
      Number.isSafeInteger(parsed)
    ? parsed
    : undefined
}
const internal = () => uiPlainError(500, "Internal Server Error")
const accountData = (account: {
  readonly id: number
  readonly code: string
  readonly name: string
}) => ({ ID: account.id, Code: account.code, Name: account.name })
const companyData = (company: {
  readonly name: string
  readonly tagline: string
  readonly address: string
  readonly phone: string
  readonly email: string
  readonly npwp: string
  readonly bank_name: string
  readonly bank_account_number: string
  readonly bank_account_holder: string
  readonly invoice_footer: string
  readonly default_revenue_account_id: number
  readonly recurring_description_template: string
}) => ({
  Name: company.name,
  Tagline: company.tagline,
  Address: company.address,
  Phone: company.phone,
  Email: company.email,
  NPWP: company.npwp,
  BankName: company.bank_name,
  BankAccountNumber: company.bank_account_number,
  BankAccountHolder: company.bank_account_holder,
  InvoiceFooter: company.invoice_footer,
  DefaultRevenueAccountID: company.default_revenue_account_id,
  RecurringDescriptionTemplate: company.recurring_description_template
})
const renderCompany = (
  auth: CookieAuthentication,
  request: Parameters<typeof renderUiPage>[0],
  company: ReturnType<typeof companyData>,
  errors: Readonly<Record<string, string>>
) => Effect.gen(function*() {
  const accounts = yield* Accounts
  const revenue = yield* accounts.list({ type: "revenue", search: "" }).pipe(
    Effect.orElseSucceed(() => [])
  )
  return renderUiPage(request, "settings/company", "Company Profile", {
    Company: company,
    RevenueAccounts: revenue.filter((item) => item.is_active).map(accountData),
    Errors: errors
  }, auth)
})
const companyPage = HttpRouter.get(`${dashboardBasePath}/settings/company`,
  protectedUiHandler((auth, request) => {
    if (!admin(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const profiles = yield* CompanyProfiles
      return yield* renderCompany(auth, request, companyData(
        yield* profiles.get
      ), {})
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))
const companySave = HttpRouter.post(`${dashboardBasePath}/settings/company`,
  protectedUiHandler((auth, request, form) => {
    if (!admin(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    const values = {
      Name: (form.get("name") ?? "").trim(),
      Tagline: (form.get("tagline") ?? "").trim(),
      Address: (form.get("address") ?? "").trim(),
      Phone: (form.get("phone") ?? "").trim(),
      Email: (form.get("email") ?? "").trim(),
      NPWP: (form.get("npwp") ?? "").trim(),
      BankName: (form.get("bank_name") ?? "").trim(),
      BankAccountNumber: (form.get("bank_account_number") ?? "").trim(),
      BankAccountHolder: (form.get("bank_account_holder") ?? "").trim(),
      InvoiceFooter: (form.get("invoice_footer") ?? "").trim(),
      DefaultRevenueAccountID:
        Number.parseInt(form.get("default_revenue_account_id") ?? "", 10) || 0,
      RecurringDescriptionTemplate:
        (form.get("recurring_description_template") ?? "").trim()
    }
    if (values.Name === "") {
      return renderCompany(auth, request, values, {
        name: "Company name is required"
      })
    }
    return Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      yield* sql`
        UPDATE company_profile
        SET
          name = ${values.Name},
          tagline = ${values.Tagline},
          address = ${values.Address},
          phone = ${values.Phone},
          email = ${values.Email},
          npwp = ${values.NPWP},
          bank_name = ${values.BankName},
          bank_account_number = ${values.BankAccountNumber},
          bank_account_holder = ${values.BankAccountHolder},
          invoice_footer = ${values.InvoiceFooter},
          default_revenue_account_id =
            ${values.DefaultRevenueAccountID || null},
          recurring_description_template =
            ${values.RecurringDescriptionTemplate},
          updated_at = datetime('now')
        WHERE id = 1
      `
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "company_profile.update",
        actor: actor(auth),
        targetType: "company_profile",
        targetId: 1,
        targetLabel: values.Name,
        metadata: { after: {
          name: values.Name,
          npwp: values.NPWP,
          bank_name: values.BankName
        } }
      })
      return uiRedirect(`${dashboardBasePath}/settings/company`, {
        "set-cookie": uiFlashCookie("Company profile saved")
      })
    }).pipe(Effect.catchAll(() =>
      renderCompany(auth, request, values, {
        general: "Failed to save company profile"
      })
    ))
  }))

const tokenData = (token: {
  readonly id: number
  readonly name: string
  readonly prefix: string
  readonly scopes: ReadonlyArray<string>
  readonly expires_at: string | null
  readonly last_used_at: string | null
  readonly revoked_at: string | null
  readonly created_at: string
}) => ({
  ID: token.id,
  Name: token.name,
  TokenPrefix: token.prefix,
  Scopes: token.scopes,
  ExpiresAt: token.expires_at,
  LastUsedAt: token.last_used_at,
  RevokedAt: token.revoked_at,
  CreatedAt: token.created_at
})
const scopesFor = (auth: CookieAuthentication) =>
  auth.user.role === "admin"
    ? allCapabilities
    : auth.effectiveCapabilities
const tokenFormData = (
  auth: CookieAuthentication,
  name = "",
  expiresAt = "",
  selected: ReadonlyArray<string> = [],
  errors: Readonly<Record<string, string>> = {}
) => ({
  AvailableScopes: scopesFor(auth),
  SelectedScopes: Object.fromEntries(selected.map((scope) => [scope, true])),
  IsScopeChecked: (scope: unknown) => selected.includes(String(scope)),
  Errors: errors,
  Name: name,
  ExpiresAt: expiresAt
})
const tokensPage = HttpRouter.get(`${dashboardBasePath}/settings/api-tokens`,
  protectedUiHandler((auth, request) => Effect.gen(function*() {
    const tokens = yield* ApiTokens
    return renderUiPage(request, "settings/api_tokens", "API Tokens", {
      Tokens: (yield* tokens.list(auth.user.id)).map(tokenData),
      AvailableScopes: scopesFor(auth),
      Errors: {}
    }, auth)
  }).pipe(Effect.catchAll(() => Effect.succeed(internal())))))
const tokensNew = HttpRouter.get(`${dashboardBasePath}/settings/api-tokens/new`,
  protectedUiHandler((auth, request) => Effect.succeed(renderUiPage(
    request,
    "settings/api_tokens_form",
    "Create API Token",
    tokenFormData(auth),
    auth
  ))))
const tokensCreate = HttpRouter.post(`${dashboardBasePath}/settings/api-tokens`,
  protectedUiHandler((auth, request, form) => {
    const name = (form.get("name") ?? "").trim()
    const selected = form.getAll("scopes")
    const expiresRaw = (form.get("expires_at") ?? "").trim()
    const allowed = new Set(scopesFor(auth))
    const errors: Record<string, string> = {}
    if (name === "") errors.name = "Name is required"
    if (selected.length === 0) errors.scopes = "Select at least one scope"
    else {
      const unknown = selected.find((scope) => !allowed.has(scope as never))
      if (unknown) errors.scopes = `Unknown or unauthorized scope: ${unknown}`
    }
    let expiresAt: string | undefined
    if (expiresRaw !== "") {
      const date = /^\d{4}-\d{2}-\d{2}$/.test(expiresRaw)
        ? new Date(`${expiresRaw}T00:00:00.000Z`)
        : new Date(Number.NaN)
      if (Number.isNaN(date.getTime())) errors.expires_at = "Invalid date format"
      else expiresAt = date.toISOString()
    }
    if (Object.keys(errors).length) {
      return Effect.succeed(renderUiPage(
        request,
        "settings/api_tokens_form",
        "Create API Token",
        tokenFormData(auth, name, expiresRaw, selected, errors),
        auth
      ))
    }
    return Effect.gen(function*() {
      const tokens = yield* ApiTokens
      const created = yield* tokens.create(
        auth.user.id,
        name,
        selected,
        expiresAt
      )
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "api_token.create",
        actor: actor(auth),
        targetType: "api_token",
        targetId: created.id,
        targetLabel: created.name,
        metadata: {
          name: created.name,
          scopes: created.scopes,
          expires_at: created.expires_at
        }
      })
      return uiRedirect(`${dashboardBasePath}/settings/api-tokens/created`, {
        "set-cookie": uiFlashCookie(created.plaintext)
      })
    }).pipe(Effect.catchAll(() => Effect.succeed(renderUiPage(
      request,
      "settings/api_tokens_form",
      "Create API Token",
      tokenFormData(auth, name, expiresRaw, selected, {
        general: "Failed to create token"
      }),
      auth
    ))))
  }))
const flashValue = (request: Parameters<typeof renderUiPage>[0]) => {
  const value = request.cookies.flash ?? ""
  const raw = value.startsWith('"') && value.endsWith('"')
    ? value.slice(1, -1)
    : value
  try {
    return decodeURIComponent(raw)
  } catch {
    return raw
  }
}
const tokensCreated = HttpRouter.get(
  `${dashboardBasePath}/settings/api-tokens/created`,
  protectedUiHandler((auth, request) => {
    const token = flashValue(request)
    if (token === "") {
      return Effect.succeed(uiRedirect(
        `${dashboardBasePath}/settings/api-tokens`
      ))
    }
    const response = renderUiPage(
      request,
      "settings/api_tokens_created",
      "Token Created",
      { Token: token },
      auth,
      [],
      ""
    )
    return Effect.succeed(HttpServerResponse.setHeader(
      response,
      "cache-control",
      "no-store, private"
    ))
  }))
const tokenRevoke = HttpRouter.post(
  `${dashboardBasePath}/settings/api-tokens/:id/revoke`,
  protectedUiHandler((auth, request) => Effect.gen(function*() {
    const tokenId = parseId((yield* HttpRouter.params).id)
    if (tokenId === undefined || tokenId <= 0) {
      return uiPlainError(400, "Invalid token ID")
    }
    const tokens = yield* ApiTokens
    const revoked = yield* tokens.revoke(auth.user.id, tokenId).pipe(
      Effect.either
    )
    if (revoked._tag === "Left") {
      return uiRedirect(`${dashboardBasePath}/settings/api-tokens`, {
        "set-cookie": uiFlashCookie("Token not found")
      })
    }
    const audit = yield* Audit
    yield* audit.log(requestMetadata(request), {
      action: "api_token.revoke",
      actor: actor(auth),
      targetType: "api_token",
      targetId: tokenId
    })
    return uiRedirect(`${dashboardBasePath}/settings/api-tokens`, {
      "set-cookie": uiFlashCookie("Token revoked")
    })
  })))

const dateFilter = (value: string, end: boolean) => {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return undefined
  const date = new Date(`${value}T${end ? "23:59:59.999" : "00:00:00.000"}Z`)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}
const auditPage = HttpRouter.get(`${dashboardBasePath}/audit`,
  protectedUiHandler((auth, request) => {
    if (
      auth.user.role !== "admin" &&
      !auth.effectiveCapabilities.includes("audit.view")
    ) return Effect.succeed(uiPlainError(403, "Forbidden"))
    const query = new URL(request.url, "http://localhost").searchParams
    const filter = {
      Actor: (query.get("actor") ?? "").trim(),
      Action: (query.get("action") ?? "").trim(),
      From: (query.get("from") ?? "").trim(),
      To: (query.get("to") ?? "").trim()
    }
    const requested = Math.max(1, Number.parseInt(query.get("page") ?? "", 10) || 1)
    return Effect.gen(function*() {
      const log = yield* AuditLog
      const result = yield* log.list({
        actorUsername: filter.Actor,
        actionPrefix: filter.Action,
        ...(dateFilter(filter.From, false) === undefined
          ? {}
          : { from: dateFilter(filter.From, false)! }),
        ...(dateFilter(filter.To, true) === undefined
          ? {}
          : { to: dateFilter(filter.To, true)! }),
        limit: 50,
        offset: (requested - 1) * 50
      })
      const totalPages = Math.max(1, Math.ceil(result.total / 50))
      return renderUiPage(request, "audit/list", "Audit Log", {
        Entries: result.entries,
        Total: result.total,
        Page: Math.min(requested, totalPages),
        PageSize: 50,
        TotalPages: totalPages,
        Filter: filter
      }, auth)
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))

const currentMonth = () => {
  const now = new Date()
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`
}
const monthFor = (request: Parameters<typeof renderUiPage>[0]) => {
  const value = (new URL(request.url, "http://localhost").searchParams
    .get("month") ?? "").trim()
  return validSchoolMonth(value) ? value : currentMonth()
}
const closureData = (closure: {
  readonly id: number
  readonly source: string
  readonly title: string
  readonly start_date: string
  readonly end_date: string
}) => ({
  ID: closure.id,
  Source: closure.source,
  Title: closure.title,
  StartDate: closure.start_date,
  EndDate: closure.end_date
})
const renderCalendar = (
  auth: CookieAuthentication,
  request: Parameters<typeof renderUiPage>[0],
  month: string,
  manual = { Title: "", StartDate: "", EndDate: "" },
  errors: Readonly<Record<string, string>> = {}
) => Effect.gen(function*() {
  const calendar = yield* SchoolCalendar
  const [closures, days, connection] = yield* Effect.all([
    calendar.list(month),
    calendar.effectiveDays(month),
    calendar.connection
  ])
  return renderUiPage(request, "settings/school_calendar", "School Calendar", {
    Month: month,
    Closures: closures.map(closureData),
    EffectiveSchoolDays: days,
    MultiplierPercent: monthlyPriceMultiplierPercent(days),
    GoogleConnection: {
      IsActive: connection.isActive,
      LastSyncAt: connection.lastSyncAt,
      LastSyncStatus: connection.lastSyncStatus,
      LastSyncError: connection.lastSyncError
    },
    GoogleConfigEnabled: calendar.configEnabled,
    GoogleConnectionActive:
      connection.isActive && connection.refreshToken !== "",
    GoogleCalendarID: connection.calendarId,
    GoogleCalendarIDSaved: connection.calendarId !== "",
    ManualClosure: manual,
    Errors: errors
  }, auth)
})
const calendarPage = HttpRouter.get(
  `${dashboardBasePath}/settings/school-calendar`,
  protectedUiHandler((auth, request) => {
    if (!admin(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return renderCalendar(auth, request, monthFor(request)).pipe(
      Effect.catchAll(() => Effect.succeed(internal()))
    )
  }))
const closureCreate = HttpRouter.post(
  `${dashboardBasePath}/settings/school-calendar/closures`,
  protectedUiHandler((auth, request, form) => {
    if (!admin(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    const title = (form.get("title") ?? "").trim()
    const startDate = (form.get("start_date") ?? "").trim()
    const endDate = (form.get("end_date") ?? "").trim()
    const month = /^\d{4}-\d{2}/.test(startDate)
      ? startDate.slice(0, 7)
      : monthFor(request)
    const rawErrors = validateSchoolClosure({ title, startDate, endDate })
    const errors: Record<string, string> = {}
    if (rawErrors.title) errors.title = "Title is required"
    if (rawErrors.start_date) {
      errors.start_date = "Valid start date is required"
    }
    if (rawErrors.end_date) {
      errors.end_date = rawErrors.end_date.includes("after")
        ? "End date must be on or after start date"
        : "Valid end date is required"
    }
    if (Object.keys(errors).length) {
      return renderCalendar(auth, request, month, {
        Title: title,
        StartDate: startDate,
        EndDate: endDate
      }, errors).pipe(Effect.catchAll(() => Effect.succeed(internal())))
    }
    return Effect.gen(function*() {
      const calendar = yield* SchoolCalendar
      const created = yield* calendar.createManual(title, startDate, endDate)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "school_closure.create",
        actor: actor(auth),
        targetType: "school_closure",
        targetId: created.id,
        targetLabel: created.title,
        metadata: { after: {
          source: created.source,
          title,
          start_date: startDate,
          end_date: endDate
        } }
      })
      return uiRedirect(
        `${dashboardBasePath}/settings/school-calendar?month=${month}`,
        { "set-cookie": uiFlashCookie("School closure added") }
      )
    }).pipe(Effect.catchAll(() =>
      renderCalendar(auth, request, month, {
        Title: title,
        StartDate: startDate,
        EndDate: endDate
      }, { general: "Failed to add closure" }).pipe(
        Effect.catchAll(() => Effect.succeed(internal()))
      )
    ))
  }))
const closureDelete = HttpRouter.post(
  `${dashboardBasePath}/settings/school-calendar/closures/:id/delete`,
  protectedUiHandler((auth, request) => {
    if (!admin(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    const month = monthFor(request)
    return Effect.gen(function*() {
      const closureId = parseId((yield* HttpRouter.params).id)
      if (closureId === undefined) return uiPlainError(404, "404 page not found")
      const calendar = yield* SchoolCalendar
      const removed = yield* calendar.remove(closureId)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "school_closure.delete",
        actor: actor(auth),
        targetType: "school_closure",
        targetId: closureId,
        targetLabel: removed.title,
        metadata: { before: {
          source: removed.source,
          title: removed.title,
          start_date: removed.start_date,
          end_date: removed.end_date
        } }
      })
      return uiRedirect(
        `${dashboardBasePath}/settings/school-calendar?month=${month}`,
        { "set-cookie": uiFlashCookie("School closure deleted") }
      )
    }).pipe(Effect.catchAll((error) => Effect.succeed(uiRedirect(
      `${dashboardBasePath}/settings/school-calendar?month=${month}`,
      { "set-cookie": uiFlashCookie(`Error deleting closure: ${String(error)}`) }
    ))))
  }))
const calendarIdSave = HttpRouter.post(
  `${dashboardBasePath}/settings/school-calendar/google-calendar-id`,
  protectedUiHandler((auth, request, form) => {
    if (!admin(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    const month = monthFor(request)
    return Effect.gen(function*() {
      const calendar = yield* SchoolCalendar
      const connection = yield* calendar.connection
      yield* calendar.saveConnection({
        ...connection,
        calendarId: (form.get("calendar_id") ?? "").trim()
      })
      return uiRedirect(
        `${dashboardBasePath}/settings/school-calendar?month=${month}`,
        { "set-cookie": uiFlashCookie("Google Calendar ID saved") }
      )
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))
const googleAction = (action: "connect" | "sync" | "disconnect") =>
  HttpRouter.post(
    `${dashboardBasePath}/integrations/google-calendar/${action}`,
    protectedUiHandler((auth, request) => {
      if (!admin(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
      const month = monthFor(request)
      return Effect.gen(function*() {
        const calendar = yield* SchoolCalendar
        if (action === "connect") {
          if (!calendar.configEnabled) {
            return uiRedirect(
              `${dashboardBasePath}/settings/school-calendar?month=${month}`,
              { "set-cookie": uiFlashCookie(
                "Google Calendar OAuth is not configured"
              ) }
            )
          }
          return uiRedirect(yield* calendar.beginOAuth(auth.user.id))
        }
        if (action === "disconnect") {
          yield* calendar.disconnect
          return uiRedirect(
            `${dashboardBasePath}/settings/school-calendar?month=${month}`,
            { "set-cookie": uiFlashCookie("Google Calendar disconnected") }
          )
        }
        const result = yield* calendar.sync
        return uiRedirect(
          `${dashboardBasePath}/settings/school-calendar?month=${month}`,
          { "set-cookie": uiFlashCookie(
            `Google Calendar synced: fetched ${result.fetched} event(s), ` +
            `stored ${result.stored} closure(s).`
          ) }
        )
      }).pipe(Effect.catchAll((error) => Effect.succeed(uiRedirect(
        `${dashboardBasePath}/settings/school-calendar?month=${month}`,
        { "set-cookie": uiFlashCookie(
          `Google Calendar ${action} failed: ${String(error)}`
        ) }
      ))))
    }))

const googleCallbackFlash = (error: unknown) => {
  const message =
    typeof error === "object" && error !== null && "message" in error &&
      typeof error.message === "string"
      ? error.message
      : ""
  if (message === "google calendar connection expired") {
    return "Google Calendar connection expired. Please try again."
  }
  if (message === "google calendar state validation failed") {
    return "Error validating Google connection"
  }
  if (message === "google calendar settings load failed") {
    return "Error loading Google Calendar settings"
  }
  if (message === "google calendar connection save failed") {
    return "Error saving Google Calendar connection"
  }
  if (message === "google did not return a refresh token") {
    return "Google did not return a refresh token. " +
      "Please reconnect and approve offline access."
  }
  return "Google Calendar authorization failed"
}
const googleCallback = HttpRouter.get(
  `${dashboardBasePath}/integrations/google-calendar/callback`,
  protectedUiHandler((auth, request) => {
    if (!admin(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    const month = monthFor(request)
    const target = `${dashboardBasePath}/settings/school-calendar?month=${month}`
    const redirect = (message: string) => uiRedirect(target, {
      "set-cookie": uiFlashCookie(message)
    })
    return Effect.gen(function*() {
      const calendar = yield* SchoolCalendar
      if (!calendar.configEnabled) {
        return redirect("Google Calendar OAuth is not configured")
      }
      const query = new URL(request.originalUrl, "http://localhost")
        .searchParams
      if (query.get("error") !== null && query.get("error") !== "") {
        return redirect("Google Calendar connection cancelled")
      }
      const code = (query.get("code") ?? "").trim()
      const state = (query.get("state") ?? "").trim()
      if (code === "" || state === "") {
        return redirect(
          "Google Calendar callback was missing required values"
        )
      }
      const result = yield* calendar.completeOAuth(
        auth.user.id,
        state,
        code
      ).pipe(Effect.either)
      if (result._tag === "Left") {
        return redirect(googleCallbackFlash(result.left))
      }
      const audit = yield* Audit
      const connection = yield* calendar.connection
      yield* audit.log(requestMetadata(request), {
        action: "google_calendar.connect",
        actor: actor(auth),
        targetType: "google_calendar_connection",
        targetId: 1,
        metadata: {
          calendar_id_set: connection.calendarId !== ""
        }
      })
      return redirect("Google Calendar connected")
    }).pipe(Effect.catchAll(() =>
      Effect.succeed(redirect("Error saving Google Calendar connection"))
    ))
  })
)

export const addUiSettingsRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  companyPage,
  companySave,
  tokensPage,
  tokensNew,
  tokensCreate,
  tokensCreated,
  tokenRevoke,
  auditPage,
  calendarPage,
  closureCreate,
  closureDelete,
  calendarIdSave,
  googleAction("connect"),
  googleCallback,
  googleAction("sync"),
  googleAction("disconnect")
)
