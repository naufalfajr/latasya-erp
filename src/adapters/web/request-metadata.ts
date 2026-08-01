import type { HttpServerRequest } from "@effect/platform"
import { Option } from "effect"

export type RequestMetadata = {
  readonly requestId: string
  readonly clientIp: string
}

const metadataCache = new WeakMap<object, RequestMetadata>()

export const newRequestId = () => {
  const value = crypto.getRandomValues(new Uint8Array(16))
  return Array.from(value, (byte) => byte.toString(16).padStart(2, "0")).join("")
}

const socketAddress = (request: HttpServerRequest.HttpServerRequest) =>
  Option.getOrElse(request.remoteAddress, () => "")

export const unspoofableClientIp = (
  request: HttpServerRequest.HttpServerRequest
) => request.headers["cf-connecting-ip"] || socketAddress(request)

export const requestMetadata = (
  request: HttpServerRequest.HttpServerRequest
): RequestMetadata => {
  const cached = metadataCache.get(request)
  if (cached !== undefined) {
    return cached
  }
  const forwarded = request.headers["x-forwarded-for"]
  const clientIp = forwarded !== undefined && forwarded !== ""
    ? (forwarded.split(",")[0]?.trim() ?? "")
    : request.headers["cf-connecting-ip"] || socketAddress(request)
  const metadata = {
    requestId: newRequestId(),
    clientIp
  }
  metadataCache.set(request, metadata)
  return metadata
}
