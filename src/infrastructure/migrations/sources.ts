import migration001 from "../../../migrations/001_initial_schema.sql" with { type: "text" }
import migration002 from "../../../migrations/002_seed_accounts.sql" with { type: "text" }
import migration003 from "../../../migrations/003_add_indexes.sql" with { type: "text" }
import migration004 from "../../../migrations/004_fix_depreciation.sql" with { type: "text" }
import migration005 from "../../../migrations/005_security.sql" with { type: "text" }
import migration006 from "../../../migrations/006_roles_table.sql" with { type: "text" }
import migration007 from "../../../migrations/007_session_absolute_expiry.sql" with { type: "text" }
import migration008 from "../../../migrations/008_audit_log.sql" with { type: "text" }
import migration009 from "../../../migrations/009_credit_notes.sql" with { type: "text" }
import migration010 from "../../../migrations/010_api_tokens_and_idempotency.sql" with { type: "text" }
import migration011 from "../../../migrations/011_pagination_indexes.sql" with { type: "text" }
import migration012 from "../../../migrations/012_company_profile.sql" with { type: "text" }
import migration013 from "../../../migrations/013_contact_fields.sql" with { type: "text" }
import migration014 from "../../../migrations/014_vehicles_routes.sql" with { type: "text" }
import migration015 from "../../../migrations/015_vehicle_capacity.sql" with { type: "text" }
import migration016 from "../../../migrations/016_contact_distance_pricing.sql" with { type: "text" }
import migration017 from "../../../migrations/017_contact_decimal_distance.sql" with { type: "text" }
import migration018 from "../../../migrations/018_school_calendar.sql" with { type: "text" }
import migration019 from "../../../migrations/019_portal_token.sql" with { type: "text" }
import migration020 from "../../../migrations/020_cash_accounts.sql" with { type: "text" }
import migration021 from "../../../migrations/021_portal_code.sql" with { type: "text" }
import migration022 from "../../../migrations/022_add_south_route.sql" with { type: "text" }

export interface MigrationSource {
  readonly filename: string
  readonly sql: string
}

export const migrationSources: ReadonlyArray<MigrationSource> = [
  { filename: "001_initial_schema.sql", sql: migration001 },
  { filename: "002_seed_accounts.sql", sql: migration002 },
  { filename: "003_add_indexes.sql", sql: migration003 },
  { filename: "004_fix_depreciation.sql", sql: migration004 },
  { filename: "005_security.sql", sql: migration005 },
  { filename: "006_roles_table.sql", sql: migration006 },
  { filename: "007_session_absolute_expiry.sql", sql: migration007 },
  { filename: "008_audit_log.sql", sql: migration008 },
  { filename: "009_credit_notes.sql", sql: migration009 },
  { filename: "010_api_tokens_and_idempotency.sql", sql: migration010 },
  { filename: "011_pagination_indexes.sql", sql: migration011 },
  { filename: "012_company_profile.sql", sql: migration012 },
  { filename: "013_contact_fields.sql", sql: migration013 },
  { filename: "014_vehicles_routes.sql", sql: migration014 },
  { filename: "015_vehicle_capacity.sql", sql: migration015 },
  { filename: "016_contact_distance_pricing.sql", sql: migration016 },
  { filename: "017_contact_decimal_distance.sql", sql: migration017 },
  { filename: "018_school_calendar.sql", sql: migration018 },
  { filename: "019_portal_token.sql", sql: migration019 },
  { filename: "020_cash_accounts.sql", sql: migration020 },
  { filename: "021_portal_code.sql", sql: migration021 },
  { filename: "022_add_south_route.sql", sql: migration022 }
]
