import * as BunRuntime from "@effect/platform-bun/BunRuntime"
import { Effect } from "effect"
import { appConfig } from "./app/config.ts"
import { serve } from "./app/server.ts"
import { buildVersion } from "./app/version.ts"
import { migrateDatabase } from "./infrastructure/migrations/migrate.ts"

const main = Effect.gen(function*() {
  const config = yield* appConfig
  yield* migrateDatabase(config.databasePath)
  return yield* serve(config, buildVersion)
})

BunRuntime.runMain(main)
