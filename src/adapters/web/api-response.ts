import { HttpServerResponse } from "@effect/platform"
import { newRequestId } from "./request-metadata.ts"

const jsonHeaders = {
  "content-type": "application/json; charset=utf-8"
} as const

export const jsonResponse = (
  body: unknown,
  status = 200,
  headers?: Readonly<Record<string, string>>
) => HttpServerResponse.text(`${JSON.stringify(body)}\n`, {
  status,
  headers: { ...jsonHeaders, ...headers }
})

export const apiError = (
  status: number,
  code: string,
  error: string,
  fields?: Readonly<Record<string, string>>,
  requestId = newRequestId()
) => jsonResponse({
  error,
  code,
  ...(fields === undefined ? {} : { fields }),
  request_id: requestId
}, status)
