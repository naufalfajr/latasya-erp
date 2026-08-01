import {
  HttpRouter,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import { readHealth } from "../../domain/system/health.ts"

const textOptions = {
  headers: {
    "content-type": "text/plain; charset=utf-8"
  }
} as const

const unavailable = HttpServerResponse.text("db unreachable\n", {
  ...textOptions,
  status: 503
})

export const addHealthRoute = (version: string) =>
  HttpRouter.get(
    "/healthz",
    readHealth(version).pipe(
      Effect.match({
        onFailure: () => unavailable,
        onSuccess: ({ migrations }) =>
          HttpServerResponse.text(
            `ok version=${version} migrations=${migrations}\n`,
            textOptions
          )
      })
    )
  )
