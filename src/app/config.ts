import { Config } from "effect"

export interface AppConfig {
  readonly port: number
  readonly databasePath: string
  readonly development: boolean
  readonly googleCalendar: {
    readonly clientId: string
    readonly clientSecret: string
    readonly redirectUrl: string
  }
}

export const appConfig: Config.Config<AppConfig> = Config.all({
  port: Config.integer("PORT").pipe(Config.withDefault(8080)),
  databasePath: Config.string("DB_PATH").pipe(Config.withDefault("./latasya.db")),
  development: Config.string("DEV_MODE").pipe(
    Config.withDefault("false"),
    Config.map((value) => value === "true")
  ),
  googleCalendar: Config.all({
    clientId: Config.string("GOOGLE_CLIENT_ID").pipe(Config.withDefault("")),
    clientSecret: Config.string("GOOGLE_CLIENT_SECRET").pipe(Config.withDefault("")),
    redirectUrl: Config.string("GOOGLE_REDIRECT_URL").pipe(Config.withDefault(""))
  })
})
