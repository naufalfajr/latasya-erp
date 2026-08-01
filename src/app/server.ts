import {
  HttpApp,
  HttpMiddleware
} from "@effect/platform"
import { SqlClient } from "@effect/sql"
import {
  Data,
  Effect,
  ManagedRuntime
} from "effect"
import { makeRouter } from "../adapters/web/router.ts"
import { seedDefaultAdmin } from "../infrastructure/bootstrap/default-admin.ts"
import type { AppConfig } from "./config.ts"
import { runtimeLayer } from "./runtime-layer.ts"

export class ServerStartupError extends Data.TaggedError("ServerStartupError")<{
  readonly cause: unknown
}> {}

interface RunningServer {
  readonly server: Bun.Server<undefined>
  readonly disposeRuntime: () => Promise<void>
}

const start = (
  config: AppConfig,
  version: string
): Effect.Effect<RunningServer, ServerStartupError> =>
  Effect.tryPromise({
    try: async () => {
      const runtime = ManagedRuntime.make(runtimeLayer(
        config.databasePath,
        config.googleCalendar
      ))
      try {
        await runtime.runPromise(seedDefaultAdmin)
        const effectRuntime = await runtime.runtime()
        const handler = HttpApp.toWebHandlerRuntime(effectRuntime)(
          makeRouter(version, config.development),
          HttpMiddleware.logger
        )
        const server = Bun.serve({
          port: config.port,
          idleTimeout: 60,
          development: config.development,
          fetch: (request) => handler(request)
        })
        return {
          server,
          disposeRuntime: () => runtime.dispose()
        }
      } catch (cause) {
        await runtime.dispose()
        throw cause
      }
    },
    catch: (cause) => new ServerStartupError({ cause })
  })

const stop = ({ disposeRuntime, server }: RunningServer) =>
  Effect.gen(function*() {
    yield* Effect.logInfo("shutting down server...")
    yield* Effect.tryPromise(() => server.stop(false)).pipe(
      Effect.timeout("10 seconds"),
      Effect.catchTag(
        "TimeoutException",
        () => Effect.tryPromise(() => server.stop(true))
      ),
      Effect.ignore
    )
    yield* Effect.promise(disposeRuntime)
    yield* Effect.logInfo("server stopped")
  })

export const serve = (
  config: AppConfig,
  version: string
): Effect.Effect<never, ServerStartupError> =>
  Effect.scoped(
    Effect.acquireRelease(
      start(config, version).pipe(
        Effect.tap(({ server }) =>
          Effect.logInfo("starting server").pipe(
            Effect.annotateLogs({
              port: server.port,
              dev: config.development
            })
          )
        )
      ),
      stop
    ).pipe(
      Effect.flatMap(() => Effect.never)
    )
  )
