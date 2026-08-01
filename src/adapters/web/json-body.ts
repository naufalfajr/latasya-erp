import { HttpServerRequest } from "@effect/platform"
import { Data, Effect } from "effect"

export class InvalidJsonBody extends Data.TaggedError("InvalidJsonBody") {}

const maximumBodyBytes = 1 << 20

export const readBodyText = (
  request: HttpServerRequest.HttpServerRequest
): Effect.Effect<string, InvalidJsonBody> =>
  request.text.pipe(
    Effect.mapError(() => new InvalidJsonBody()),
    Effect.filterOrFail(
      (body) =>
        new TextEncoder().encode(body).byteLength <= maximumBodyBytes,
      () => new InvalidJsonBody()
    )
  )

export const parseJsonObject = (
  body: string,
  allowedFields: ReadonlyArray<string>
): Effect.Effect<Readonly<Record<string, unknown>>, InvalidJsonBody> =>
  Effect.try({
    try: () => {
      const decoded: unknown = JSON.parse(body)
      if (
        typeof decoded !== "object" ||
        decoded === null ||
        Array.isArray(decoded)
      ) {
        throw new Error("request body must be an object")
      }
      const input = decoded as Readonly<Record<string, unknown>>
      if (Object.keys(input).some((key) => !allowedFields.includes(key))) {
        throw new Error("unknown field")
      }
      return input
    },
    catch: () => new InvalidJsonBody()
  })

export const readJsonObject = (
  request: HttpServerRequest.HttpServerRequest,
  allowedFields: ReadonlyArray<string>
): Effect.Effect<Readonly<Record<string, unknown>>, InvalidJsonBody> =>
  readBodyText(request).pipe(
    Effect.flatMap((body) => parseJsonObject(body, allowedFields))
  )

export const readStringRecord = (
  request: HttpServerRequest.HttpServerRequest,
  allowedFields: ReadonlyArray<string>
): Effect.Effect<Readonly<Record<string, string>>, InvalidJsonBody> =>
  readJsonObject(request, allowedFields).pipe(
    Effect.flatMap((input) =>
      Effect.try({
        try: () => {
          const result: Record<string, string> = {}
          for (const key of allowedFields) {
            const value = input[key]
            if (value === undefined || value === null) {
              result[key] = ""
            } else if (typeof value === "string") {
              result[key] = value
            } else {
              throw new Error("field must be a string")
            }
          }
          return result
        },
        catch: () => new InvalidJsonBody()
      })
    )
  )
