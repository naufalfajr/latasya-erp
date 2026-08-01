import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"

export type CompanyProfile = {
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
  readonly updated_at: string
}

type ProfileRow = Omit<
  CompanyProfile,
  "default_revenue_account_id"
> & {
  readonly default_revenue_account_id: number | null
}

export class CompanyProfileStoreError extends Data.TaggedError(
  "CompanyProfileStoreError"
)<{
  readonly cause: unknown
}> {}

export interface CompanyProfiles {
  readonly get: Effect.Effect<CompanyProfile, CompanyProfileStoreError>
}

export const CompanyProfiles = Context.GenericTag<CompanyProfiles>(
  "latasya/CompanyProfiles"
)

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient
  const get = sql<ProfileRow>`
    SELECT
      name,
      tagline,
      address,
      phone,
      email,
      npwp,
      bank_name,
      bank_account_number,
      bank_account_holder,
      invoice_footer,
      default_revenue_account_id,
      recurring_description_template,
      updated_at
    FROM company_profile
    WHERE id = 1
  `.pipe(
    Effect.mapError(
      (cause) => new CompanyProfileStoreError({ cause })
    ),
    Effect.flatMap((rows) => {
      const row = rows[0]
      if (row === undefined) {
        return Effect.fail(new CompanyProfileStoreError({
          cause: new Error("company profile not found")
        }))
      }
      return Effect.succeed({
        ...row,
        default_revenue_account_id:
          row.default_revenue_account_id ?? 0
      })
    })
  )
  return CompanyProfiles.of({ get })
})

export const CompanyProfilesLive = Layer.effect(CompanyProfiles, make)
