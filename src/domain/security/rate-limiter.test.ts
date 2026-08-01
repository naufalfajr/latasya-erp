import { describe, expect, test } from "bun:test"
import { Effect } from "effect"
import {
  RateLimiter,
  RateLimiterLive,
  retryAfterSeconds
} from "./rate-limiter.ts"

describe("RateLimiter", () => {
  test("allows five failed attempts and blocks the sixth", async () => {
    const attempts = Effect.gen(function*() {
      const limiter = yield* RateLimiter
      return yield* Effect.all(
        Array.from({ length: 6 }, () => limiter.take("login", "ip:alice"))
      )
    })
    const result = await Effect.runPromise(
      attempts.pipe(Effect.provide(RateLimiterLive))
    )

    expect(result).toEqual([true, true, true, true, true, false])
    expect(retryAfterSeconds("login")).toBe(900)
  })

  test("keeps keys and policies independent", async () => {
    const attempts = Effect.gen(function*() {
      const limiter = yield* RateLimiter
      for (let index = 0; index < 5; index += 1) {
        yield* limiter.take("login", "first")
      }
      return {
        otherIp: yield* limiter.take("login", "second"),
        portal: yield* limiter.take("portal", "first")
      }
    })
    const result = await Effect.runPromise(
      attempts.pipe(Effect.provide(RateLimiterLive))
    )
    expect(result).toEqual({ otherIp: true, portal: true })
  })

  test("refunds successful attempts without exceeding capacity", async () => {
    const attempts = Effect.gen(function*() {
      const limiter = yield* RateLimiter
      for (let index = 0; index < 4; index += 1) {
        yield* limiter.take("login", "success")
      }
      yield* limiter.take("login", "success")
      yield* limiter.refund("login", "success")
      return yield* limiter.take("login", "success")
    })
    await expect(
      Effect.runPromise(attempts.pipe(Effect.provide(RateLimiterLive)))
    ).resolves.toBe(true)
  })
})
