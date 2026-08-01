import { SqlClient } from "@effect/sql"
import {
  Clock,
  Context,
  Data,
  Effect,
  Layer
} from "effect"

const idempotencyTtlMilliseconds = 24 * 60 * 60 * 1000

export type IdempotencyInput = {
  readonly key: string
  readonly userId: number
  readonly method: string
  readonly path: string
  readonly body: Uint8Array
}

export type CachedHttpResponse = {
  readonly status: number
  readonly body: Uint8Array
}

export type IdempotencyResult = {
  readonly response: CachedHttpResponse
  readonly replayed: boolean
}

export class IdempotencyConflict extends Data.TaggedError(
  "IdempotencyConflict"
) {}

export interface Idempotency {
  readonly run: <E, R>(
    input: IdempotencyInput,
    operation: Effect.Effect<CachedHttpResponse, E, R>
  ) => Effect.Effect<IdempotencyResult, E | IdempotencyConflict, R>
  readonly cleanExpired: Effect.Effect<void>
}

export const Idempotency = Context.GenericTag<Idempotency>(
  "latasya/Idempotency"
)

type RecordRow = {
  readonly request_hash: string
  readonly response_status: number
  readonly response_body: Uint8Array
}

const sqliteDateTime = (milliseconds: number) =>
  new Date(milliseconds).toISOString().slice(0, 19).replace("T", " ")

export const hashIdempotentRequest = (input: IdempotencyInput) => {
  const hasher = new Bun.CryptoHasher("sha256")
  hasher.update(
    new TextEncoder().encode(
      `${input.userId}:${input.method}:${input.path}:`
    )
  )
  hasher.update(input.body)
  return hasher.digest("hex")
}

const makeMutex = () => {
  const tails = new Map<string, Promise<void>>()

  return <A, E, R>(
    key: string,
    effect: Effect.Effect<A, E, R>
  ): Effect.Effect<A, E, R> =>
    Effect.acquireUseRelease(
      Effect.promise(async () => {
        const previous = tails.get(key) ?? Promise.resolve()
        let release = () => {}
        const current = new Promise<void>((resolve) => {
          release = resolve
        })
        const tail = previous.then(() => current)
        tails.set(key, tail)
        await previous
        return { release, tail }
      }),
      () => effect,
      ({ release, tail }) =>
        Effect.sync(() => {
          release()
          if (tails.get(key) === tail) {
            tails.delete(key)
          }
        })
    )
}

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient
  const withLock = makeMutex()

  const lookup = (input: IdempotencyInput, requestHash: string) =>
    sql<RecordRow>`
      SELECT request_hash, response_status, response_body
      FROM idempotency_keys
      WHERE key = ${input.key}
        AND user_id = ${input.userId}
        AND expires_at > datetime('now')
    `.pipe(
      Effect.map((rows) => {
        const row = rows[0]
        if (row === undefined) {
          return undefined
        }
        if (row.request_hash !== requestHash) {
          return new IdempotencyConflict()
        }
        return {
          response: {
            status: row.response_status,
            body: new Uint8Array(row.response_body)
          },
          replayed: true
        } satisfies IdempotencyResult
      })
    )

  const store = (
    input: IdempotencyInput,
    requestHash: string,
    response: CachedHttpResponse
  ) => Effect.gen(function*() {
    const now = yield* Clock.currentTimeMillis
    yield* sql`
      INSERT OR IGNORE INTO idempotency_keys (
        key,
        user_id,
        request_hash,
        response_status,
        response_body,
        expires_at
      )
      VALUES (
        ${input.key},
        ${input.userId},
        ${requestHash},
        ${response.status},
        ${response.body},
        ${sqliteDateTime(now + idempotencyTtlMilliseconds)}
      )
    `
  }).pipe(
    Effect.catchAll((cause) =>
      Effect.logError("idempotency: store failed").pipe(
        Effect.annotateLogs({ cause })
      )
    )
  )

  const run: Idempotency["run"] = (input, operation) => {
    const requestHash = hashIdempotentRequest(input)
    const lockKey = `${input.userId}:${input.key}`
    return withLock(
      lockKey,
      Effect.gen(function*() {
        const found = yield* lookup(input, requestHash).pipe(
          Effect.catchAll((cause) =>
            Effect.logError("idempotency: lookup failed").pipe(
              Effect.annotateLogs({ cause }),
              Effect.as(undefined)
            )
          )
        )
        if (found instanceof IdempotencyConflict) {
          return yield* found
        }
        if (found !== undefined) {
          return found
        }

        const response = yield* operation
        if (response.status >= 200 && response.status < 300) {
          yield* store(input, requestHash, response)
        }
        return { response, replayed: false }
      })
    )
  }

  const cleanExpired = sql`
    DELETE FROM idempotency_keys
    WHERE expires_at < datetime('now')
  `.pipe(
    Effect.asVoid,
    Effect.catchAll((cause) =>
      Effect.logError("clean expired idempotency keys").pipe(
        Effect.annotateLogs({ cause })
      )
    )
  )

  return Idempotency.of({ run, cleanExpired })
})

export const IdempotencyLive = Layer.effect(Idempotency, make)
