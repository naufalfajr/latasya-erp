import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"

export type NullableInt64 = {
  readonly Int64: number
  readonly Valid: boolean
}

export type AuditLogEntry = {
  readonly ID: number
  readonly OccurredAt: string
  readonly RequestID: string
  readonly ActorID: NullableInt64
  readonly ActorUsername: string
  readonly Action: string
  readonly TargetType: string
  readonly TargetID: NullableInt64
  readonly TargetLabel: string
  readonly Result: string
  readonly ErrorMessage: string
  readonly IP: string
  readonly Metadata: string
}

export type AuditLogFilter = {
  readonly actorUsername: string
  readonly actionPrefix: string
  readonly from?: string
  readonly to?: string
  readonly limit: number
  readonly offset: number
}

export class AuditLogStoreError extends Data.TaggedError(
  "AuditLogStoreError"
)<{
  readonly cause: unknown
}> {}

export interface AuditLog {
  readonly list: (
    filter: AuditLogFilter
  ) => Effect.Effect<{
    readonly entries: ReadonlyArray<AuditLogEntry>
    readonly total: number
  }, AuditLogStoreError>
}

export const AuditLog = Context.GenericTag<AuditLog>("latasya/AuditLog")

type AuditLogRow = {
  readonly id: number
  readonly occurred_at: string
  readonly request_id: string
  readonly actor_id: number | null
  readonly actor_username: string
  readonly action: string
  readonly target_type: string
  readonly target_id: number | null
  readonly target_label: string
  readonly result: string
  readonly error_message: string
  readonly ip: string
  readonly metadata: string
}

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new AuditLogStoreError({ cause }))
  )

const nullableInt64 = (value: number | null): NullableInt64 => ({
  Int64: value ?? 0,
  Valid: value !== null
})

const fromRow = (row: AuditLogRow): AuditLogEntry => ({
  ID: row.id,
  OccurredAt: row.occurred_at,
  RequestID: row.request_id,
  ActorID: nullableInt64(row.actor_id),
  ActorUsername: row.actor_username,
  Action: row.action,
  TargetType: row.target_type,
  TargetID: nullableInt64(row.target_id),
  TargetLabel: row.target_label,
  Result: row.result,
  ErrorMessage: row.error_message,
  IP: row.ip,
  Metadata: row.metadata
})

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient

  const list: AuditLog["list"] = (filter) => {
    const where: Array<string> = []
    const params: Array<unknown> = []
    if (filter.actorUsername !== "") {
      where.push("actor_username = ?")
      params.push(filter.actorUsername)
    }
    if (filter.actionPrefix !== "") {
      where.push("action LIKE ?")
      params.push(`${filter.actionPrefix}%`)
    }
    if (filter.from !== undefined) {
      where.push("occurred_at >= ?")
      params.push(filter.from)
    }
    if (filter.to !== undefined) {
      where.push("occurred_at <= ?")
      params.push(filter.to)
    }
    const whereSql = where.length === 0
      ? ""
      : `WHERE ${where.join(" AND ")}`
    const countQuery = `
      SELECT COUNT(*) AS count
      FROM audit_log
      ${whereSql}
    `
    const entriesQuery = `
      SELECT
        id,
        occurred_at,
        COALESCE(request_id, '') AS request_id,
        actor_id,
        COALESCE(actor_username, '') AS actor_username,
        action,
        COALESCE(target_type, '') AS target_type,
        target_id,
        COALESCE(target_label, '') AS target_label,
        result,
        COALESCE(error_message, '') AS error_message,
        COALESCE(ip, '') AS ip,
        COALESCE(metadata, '') AS metadata
      FROM audit_log
      ${whereSql}
      ORDER BY occurred_at DESC, id DESC
      LIMIT ? OFFSET ?
    `
    return Effect.gen(function*() {
      const counts = yield* store(sql.unsafe<{ readonly count: number }>(
        countQuery,
        params
      ))
      const rows = yield* store(sql.unsafe<AuditLogRow>(
        entriesQuery,
        [...params, filter.limit <= 0 ? 50 : filter.limit, filter.offset]
      ))
      return {
        entries: rows.map(fromRow),
        total: counts[0]?.count ?? 0
      }
    })
  }

  return AuditLog.of({ list })
})

export const AuditLogLive = Layer.effect(AuditLog, make)
