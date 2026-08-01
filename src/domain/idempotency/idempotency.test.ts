import { afterEach, describe, expect, test } from "bun:test"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect, Layer } from "effect"
import { sqliteDatabaseLayer } from "../../adapters/sqlite/database.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { PasswordHasherLive } from "../auth/password.ts"
import {
  hashIdempotentRequest,
  Idempotency,
  IdempotencyConflict,
  IdempotencyLive,
  type IdempotencyInput
} from "./idempotency.ts"

const encoder = new TextEncoder()
const decoder = new TextDecoder()
const temporaryDirectories: Array<string> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-idempotency-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const database = sqliteDatabaseLayer(databasePath)
  const bootstrap = Layer.merge(database, PasswordHasherLive)
  await Effect.runPromise(seedDefaultAdmin.pipe(Effect.provide(bootstrap)))
  return Layer.merge(
    database,
    IdempotencyLive.pipe(Layer.provide(database))
  )
}

const input = (
  key: string,
  body = '{"a":1}'
): IdempotencyInput => ({
  key,
  userId: 1,
  method: "POST",
  path: "/api/v1/test",
  body: encoder.encode(body)
})

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("Idempotency", () => {
  test("uses the exact Go request hash", () => {
    expect(hashIdempotentRequest({
      ...input("hash"),
      userId: 7
    })).toBe(
      "1ce466381dc52aa8fef40c69aa3d599f2d969256e93c59d18561bfaad3a22cba"
    )
  })

  test("caches 2xx bytes and replays without invoking the operation", async () => {
    const layer = await setup()
    let calls = 0
    const execute = Effect.gen(function*() {
      const idempotency = yield* Idempotency
      const operation = Effect.sync(() => {
        calls += 1
        return {
          status: 200,
          body: encoder.encode(`{"call":${calls},"ok":true}\n`)
        }
      })
      const first = yield* idempotency.run(input("replay"), operation)
      const second = yield* idempotency.run(input("replay"), operation)
      return { first, second }
    })
    const result = await Effect.runPromise(execute.pipe(Effect.provide(layer)))

    expect(calls).toBe(1)
    expect(result.first.replayed).toBe(false)
    expect(result.second.replayed).toBe(true)
    expect(decoder.decode(result.second.response.body)).toBe(
      '{"call":1,"ok":true}\n'
    )
  })

  test("rejects key reuse with different request bytes", async () => {
    const layer = await setup()
    const execute = Effect.gen(function*() {
      const idempotency = yield* Idempotency
      const operation = Effect.succeed({
        status: 201,
        body: encoder.encode('{"ok":true}\n')
      })
      yield* idempotency.run(input("conflict"), operation)
      return yield* idempotency.run(
        input("conflict", '{"a":2}'),
        operation
      ).pipe(Effect.flip)
    })
    const result = await Effect.runPromise(execute.pipe(Effect.provide(layer)))
    expect(result).toEqual(new IdempotencyConflict())
  })

  test("does not cache non-2xx responses", async () => {
    const layer = await setup()
    let calls = 0
    const execute = Effect.gen(function*() {
      const idempotency = yield* Idempotency
      const operation = Effect.sync(() => {
        calls += 1
        return {
          status: 500,
          body: encoder.encode('{"error":"failed"}\n')
        }
      })
      yield* idempotency.run(input("failure"), operation)
      yield* idempotency.run(input("failure"), operation)
    })
    await Effect.runPromise(execute.pipe(Effect.provide(layer)))
    expect(calls).toBe(2)
  })

  test("serializes concurrent requests for one user and key", async () => {
    const layer = await setup()
    let calls = 0
    const execute = Effect.gen(function*() {
      const idempotency = yield* Idempotency
      const operation = Effect.gen(function*() {
        calls += 1
        yield* Effect.sleep("10 millis")
        return {
          status: 200,
          body: encoder.encode(`{"call":${calls}}\n`)
        }
      })
      return yield* Effect.all(
        Array.from(
          { length: 10 },
          () => idempotency.run(input("concurrent"), operation)
        ),
        { concurrency: "unbounded" }
      )
    })
    const results = await Effect.runPromise(execute.pipe(Effect.provide(layer)))

    expect(calls).toBe(1)
    expect(results.filter((result) => !result.replayed)).toHaveLength(1)
    expect(new Set(
      results.map((result) => decoder.decode(result.response.body))
    ).size).toBe(1)
  })
})
