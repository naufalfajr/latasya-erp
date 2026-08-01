import { SqlClient } from "@effect/sql"
import { Clock, Context, Data, Effect, Layer } from "effect"

export type ApiToken = {
  readonly id: number
  readonly name: string
  readonly prefix: string
  readonly scopes: ReadonlyArray<string>
  readonly expires_at: string | null
  readonly last_used_at: string | null
  readonly revoked_at: string | null
  readonly created_at: string
}

export type CreatedApiToken = ApiToken & {
  readonly plaintext: string
}

export class ApiTokenNotFound extends Data.TaggedError(
  "ApiTokenNotFound"
) {}

export class ApiTokenStoreError extends Data.TaggedError(
  "ApiTokenStoreError"
)<{
  readonly cause: unknown
}> {}

export interface ApiTokens {
  readonly list: (
    userId: number
  ) => Effect.Effect<ReadonlyArray<ApiToken>, ApiTokenStoreError>
  readonly create: (
    userId: number,
    name: string,
    scopes: ReadonlyArray<string>,
    expiresAt: string | undefined
  ) => Effect.Effect<CreatedApiToken, ApiTokenStoreError>
  readonly revoke: (
    userId: number,
    tokenId: number
  ) => Effect.Effect<
    { readonly id: number; readonly name: string },
    ApiTokenNotFound | ApiTokenStoreError
  >
}

export const ApiTokens = Context.GenericTag<ApiTokens>(
  "latasya/ApiTokens"
)

type ApiTokenRow = {
  readonly id: number
  readonly name: string
  readonly token_prefix: string
  readonly scopes: string
  readonly expires_at: string | null
  readonly last_used_at: string | null
  readonly revoked_at: string | null
  readonly created_at: string
}

const zeroTime = "0001-01-01T00:00:00Z"
const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new ApiTokenStoreError({ cause }))
  )

const parseScopes = (encoded: string) => {
  const value: unknown = JSON.parse(encoded)
  if (!Array.isArray(value) || !value.every((scope) =>
    typeof scope === "string"
  )) {
    throw new Error("invalid scopes")
  }
  return value
}

const parsedRfc3339 = (value: string | null) => {
  if (value === null || !value.includes("T")) {
    return null
  }
  return Number.isNaN(Date.parse(value)) ? null : value
}

const fromRow = (row: ApiTokenRow): ApiToken => ({
  id: row.id,
  name: row.name,
  prefix: row.token_prefix,
  scopes: parseScopes(row.scopes),
  expires_at: parsedRfc3339(row.expires_at),
  last_used_at: parsedRfc3339(row.last_used_at),
  revoked_at: parsedRfc3339(row.revoked_at),
  created_at: parsedRfc3339(row.created_at) ?? zeroTime
})

const generateToken = Effect.try({
  try: () => {
    const bytes = crypto.getRandomValues(new Uint8Array(32))
    let random = ""
    for (const byte of bytes) {
      random += base62[byte % base62.length]
    }
    const plaintext = `lat_${random}`
    return {
      plaintext,
      prefix: plaintext.slice(0, 8),
      hash: new Bun.CryptoHasher("sha256")
        .update(plaintext)
        .digest("hex")
    }
  },
  catch: (cause) => new ApiTokenStoreError({ cause })
})

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient

  const list: ApiTokens["list"] = (userId) =>
    Effect.gen(function*() {
      const rows = yield* store(sql<ApiTokenRow>`
        SELECT
          id,
          name,
          token_prefix,
          scopes,
          expires_at,
          last_used_at,
          revoked_at,
          created_at
        FROM api_tokens
        WHERE user_id = ${userId}
        ORDER BY created_at DESC
      `)
      return yield* Effect.try({
        try: () => rows.map(fromRow),
        catch: (cause) => new ApiTokenStoreError({ cause })
      })
    })

  const create: ApiTokens["create"] = (
    userId,
    name,
    scopes,
    expiresAt
  ) =>
    Effect.gen(function*() {
      const generated = yield* generateToken
      const createdAt = new Date(yield* Clock.currentTimeMillis).toISOString()
      yield* store(sql`
        INSERT INTO api_tokens (
          user_id,
          name,
          token_prefix,
          token_hash,
          scopes,
          expires_at
        )
        VALUES (
          ${userId},
          ${name},
          ${generated.prefix},
          ${generated.hash},
          ${JSON.stringify(scopes)},
          ${expiresAt ?? null}
        )
      `)
      const ids = yield* store(sql<{ readonly id: number }>`
        SELECT last_insert_rowid() AS id
      `)
      return {
        id: ids[0]?.id ?? 0,
        name,
        prefix: generated.prefix,
        scopes,
        expires_at: expiresAt ?? null,
        last_used_at: null,
        revoked_at: null,
        created_at: createdAt,
        plaintext: generated.plaintext
      }
    })

  const revoke: ApiTokens["revoke"] = (userId, tokenId) =>
    Effect.gen(function*() {
      const rows = yield* store(sql<{ readonly name: string }>`
        SELECT name
        FROM api_tokens
        WHERE id = ${tokenId} AND user_id = ${userId}
      `)
      const existing = rows[0]
      if (existing === undefined) {
        return yield* new ApiTokenNotFound()
      }
      const revoked = yield* store(sql<{ readonly name: string }>`
        UPDATE api_tokens
        SET revoked_at = datetime('now')
        WHERE
          id = ${tokenId}
          AND user_id = ${userId}
          AND revoked_at IS NULL
        RETURNING name
      `)
      if (revoked[0] === undefined) {
        return yield* new ApiTokenNotFound()
      }
      return { id: tokenId, name: existing.name }
    })

  return ApiTokens.of({ list, create, revoke })
})

export const ApiTokensLive = Layer.effect(ApiTokens, make)
