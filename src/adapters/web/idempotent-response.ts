import {
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import type { Authenticated } from "../../domain/auth/authentication.ts"
import {
  Idempotency
} from "../../domain/idempotency/idempotency.ts"
import { apiError } from "./api-response.ts"

const cachedResponse = (
  response: HttpServerResponse.HttpServerResponse
) => {
  if (response.body._tag !== "Uint8Array") {
    return Effect.die("idempotent response body must be a byte array")
  }
  return Effect.succeed({
    status: response.status,
    body: response.body.body
  })
}

export const runIdempotently = <R>(
  authentication: Authenticated,
  request: HttpServerRequest.HttpServerRequest,
  rawBody: string,
  operation: Effect.Effect<HttpServerResponse.HttpServerResponse, never, R>
): Effect.Effect<
  HttpServerResponse.HttpServerResponse,
  never,
  Idempotency | R
> =>
  Effect.gen(function*() {
    const idempotency = yield* Idempotency
    let freshResponse:
      | HttpServerResponse.HttpServerResponse
      | undefined
    const result = yield* idempotency.run(
      {
        key: request.headers["idempotency-key"] ?? "",
        userId: authentication.user.id,
        method: request.method,
        path: new URL(request.url, "http://localhost").pathname,
        body: new TextEncoder().encode(rawBody)
      },
      operation.pipe(
        Effect.tap((response) =>
          Effect.sync(() => {
            freshResponse = response
          })
        ),
        Effect.flatMap(cachedResponse)
      )
    )
    if (!result.replayed && freshResponse !== undefined) {
      return freshResponse
    }
    return HttpServerResponse.uint8Array(result.response.body, {
      status: result.response.status,
      headers: {
        "content-type": "application/json; charset=utf-8"
      }
    })
  }).pipe(
    Effect.catchTag(
      "IdempotencyConflict",
      () => Effect.succeed(
        apiError(
          409,
          "idempotency_conflict",
          "idempotency key reused with different request body"
        )
      )
    )
  )
