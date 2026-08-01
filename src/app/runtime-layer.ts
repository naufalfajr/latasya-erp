import * as BunHttpPlatform from "@effect/platform-bun/BunHttpPlatform"
import { Layer } from "effect"
import { sqliteDatabaseLayer } from "../adapters/sqlite/database.ts"
import { AuditLive } from "../domain/audit/audit.ts"
import { AuditLogLive } from "../domain/audit/audit-log.ts"
import { RolesLive } from "../domain/access/roles.ts"
import { UsersLive } from "../domain/access/users.ts"
import { AccountsLive } from "../domain/accounting/accounts.ts"
import { BillsLive } from "../domain/accounting/bills.ts"
import { CreditNotesLive } from "../domain/accounting/credit-notes.ts"
import { ExpensesLive } from "../domain/accounting/expenses.ts"
import { IncomeLive } from "../domain/accounting/income.ts"
import { InvoicesLive } from "../domain/accounting/invoices.ts"
import { ReportingLive } from "../domain/accounting/reporting.ts"
import { ContactsLive } from "../domain/contacts/contacts.ts"
import { CompanyProfilesLive } from "../domain/company/profile.ts"
import { JournalsLive } from "../domain/accounting/journals.ts"
import { AuthenticationLive } from "../domain/auth/authentication.ts"
import { ApiTokensLive } from "../domain/auth/api-tokens.ts"
import { PasswordHasherLive } from "../domain/auth/password.ts"
import { IdempotencyLive } from "../domain/idempotency/idempotency.ts"
import { RateLimiterLive } from "../domain/security/rate-limiter.ts"
import {
  SchoolCalendarLive,
  type GoogleCalendarConfig
} from "../domain/school-calendar/school-calendar.ts"

const emptyGoogleCalendarConfig: GoogleCalendarConfig = {
  clientId: "",
  clientSecret: "",
  redirectUrl: ""
}

export const runtimeLayer = (
  databasePath: string,
  googleCalendar: GoogleCalendarConfig = emptyGoogleCalendarConfig
) => {
  const base = Layer.mergeAll(
    sqliteDatabaseLayer(databasePath),
    PasswordHasherLive,
    BunHttpPlatform.layer
  )
  const journals = JournalsLive.pipe(Layer.provide(base))
  return Layer.mergeAll(
    base,
    AuthenticationLive.pipe(Layer.provide(base)),
    ApiTokensLive.pipe(Layer.provide(base)),
    AuditLive.pipe(Layer.provide(base)),
    AuditLogLive.pipe(Layer.provide(base)),
    IdempotencyLive.pipe(Layer.provide(base)),
    RolesLive.pipe(Layer.provide(base)),
    UsersLive.pipe(Layer.provide(base)),
    AccountsLive.pipe(Layer.provide(base)),
    ContactsLive.pipe(Layer.provide(base)),
    CompanyProfilesLive.pipe(Layer.provide(base)),
    journals,
    ExpensesLive.pipe(Layer.provide(journals)),
    BillsLive.pipe(Layer.provide(Layer.merge(base, journals))),
    CreditNotesLive.pipe(Layer.provide(Layer.merge(base, journals))),
    IncomeLive.pipe(Layer.provide(journals)),
    InvoicesLive.pipe(Layer.provide(Layer.merge(base, journals))),
    ReportingLive.pipe(Layer.provide(base)),
    SchoolCalendarLive(googleCalendar).pipe(Layer.provide(base)),
    RateLimiterLive
  )
}
