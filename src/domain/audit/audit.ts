import { SqlClient } from "@effect/sql"
import { Context, Effect, Layer } from "effect"

export type AuditRequest = {
  readonly requestId?: string
  readonly clientIp?: string
}

export type AuditActor = {
  readonly id?: number
  readonly username?: string
  readonly tokenId?: number
}

export type AuditEvent = {
  readonly action: string
  readonly targetType?: string
  readonly targetId?: number
  readonly targetLabel?: string
  readonly metadata?: Readonly<Record<string, unknown>>
  readonly result?: "ok" | "fail"
  readonly error?: unknown
  readonly actor?: AuditActor
}

export interface Audit {
  readonly log: (
    request: AuditRequest,
    event: AuditEvent
  ) => Effect.Effect<void>
}

export const Audit = Context.GenericTag<Audit>("latasya/Audit")

const valueOrNull = <A>(value: A | undefined | "") =>
  value === undefined || value === "" ? null : value

const errorMessage = (error: unknown) => {
  if (error === undefined) {
    return null
  }
  return error instanceof Error ? error.message : String(error)
}

const canonicalize = (value: unknown): unknown => {
  if (Array.isArray(value)) {
    return value.map(canonicalize)
  }
  if (typeof value !== "object" || value === null) {
    return value
  }
  return Object.fromEntries(
    Object.entries(value)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, nested]) => [key, canonicalize(nested)])
  )
}

const metadataJson = (
  metadata: Readonly<Record<string, unknown>> | undefined
) => metadata === undefined || Object.keys(metadata).length === 0
  ? null
  : JSON.stringify(canonicalize(metadata))

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient

  const log: Audit["log"] = (request, event) => {
    const result = event.result ??
      (event.error === undefined ? "ok" : "fail")
    const actor = event.actor
    return sql`
      INSERT INTO audit_log (
        request_id,
        actor_id,
        actor_username,
        actor_token_id,
        action,
        target_type,
        target_id,
        target_label,
        result,
        error_message,
        ip,
        metadata
      )
      VALUES (
        ${valueOrNull(request.requestId)},
        ${valueOrNull(actor?.id)},
        ${valueOrNull(actor?.username)},
        ${valueOrNull(actor?.tokenId)},
        ${event.action},
        ${valueOrNull(event.targetType)},
        ${valueOrNull(event.targetId === 0 ? undefined : event.targetId)},
        ${valueOrNull(event.targetLabel)},
        ${result},
        ${errorMessage(event.error)},
        ${valueOrNull(request.clientIp)},
        ${metadataJson(event.metadata)}
      )
    `.pipe(
      Effect.asVoid,
      Effect.catchAll((cause) =>
        Effect.logError("audit: insert").pipe(
          Effect.annotateLogs({ action: event.action, cause })
        )
      )
    )
  }

  return Audit.of({ log })
})

export const AuditLive = Layer.effect(Audit, make)

const jsonEqual = (left: unknown, right: unknown) =>
  JSON.stringify(canonicalize(left)) === JSON.stringify(canonicalize(right))

export const auditDiff = (
  beforeValues: Readonly<Record<string, unknown>>,
  afterValues: Readonly<Record<string, unknown>>,
  fields: ReadonlyArray<string>
): Readonly<Record<string, unknown>> | undefined => {
  const before: Record<string, unknown> = {}
  const after: Record<string, unknown> = {}
  for (const field of fields) {
    if (!(field in beforeValues) && !(field in afterValues)) {
      continue
    }
    const previous = beforeValues[field]
    const next = afterValues[field]
    if (jsonEqual(previous, next)) {
      continue
    }
    before[field] = previous
    after[field] = next
  }
  return Object.keys(before).length === 0 && Object.keys(after).length === 0
    ? undefined
    : { before, after }
}
