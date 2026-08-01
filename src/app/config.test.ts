import { describe, expect, test } from "bun:test"
import { ConfigProvider, Effect } from "effect"
import { appConfig } from "./config.ts"

const loadWith = (values: ReadonlyArray<readonly [string, string]>) =>
  Effect.runPromise(
    appConfig.pipe(
      Effect.withConfigProvider(ConfigProvider.fromMap(new Map(values)))
    )
  )

describe("appConfig", () => {
  test("matches the Go server defaults", async () => {
    await expect(loadWith([])).resolves.toEqual({
      port: 8080,
      databasePath: "./latasya.db",
      development: false,
      googleCalendar: {
        clientId: "",
        clientSecret: "",
        redirectUrl: ""
      }
    })
  })

  test("loads the existing environment variable names", async () => {
    await expect(loadWith([
      ["PORT", "9090"],
      ["DB_PATH", "/var/lib/latasya/latasya.db"],
      ["DEV_MODE", "true"],
      ["GOOGLE_CLIENT_ID", "client"],
      ["GOOGLE_CLIENT_SECRET", "secret"],
      ["GOOGLE_REDIRECT_URL", "https://example.test/callback"]
    ])).resolves.toEqual({
      port: 9090,
      databasePath: "/var/lib/latasya/latasya.db",
      development: true,
      googleCalendar: {
        clientId: "client",
        clientSecret: "secret",
        redirectUrl: "https://example.test/callback"
      }
    })
  })

  test("enables development mode only for the exact value true", async () => {
    await expect(loadWith([["DEV_MODE", "TRUE"]])).resolves.toMatchObject({
      development: false
    })
  })
})
