import { Clock, Context, Effect, Layer } from "effect"

export type RateLimitPolicy = "login" | "portal"

export interface RateLimiter {
  readonly take: (
    policy: RateLimitPolicy,
    key: string
  ) => Effect.Effect<boolean>
  readonly refund: (
    policy: RateLimitPolicy,
    key: string
  ) => Effect.Effect<void>
}

export const RateLimiter = Context.GenericTag<RateLimiter>(
  "latasya/RateLimiter"
)

type Bucket = {
  tokens: number
  lastRefill: number
  lastSeen: number
}

const policies = {
  login: {
    size: 5,
    windowMilliseconds: 15 * 60 * 1000
  },
  portal: {
    size: 5,
    windowMilliseconds: 60 * 60 * 1000
  }
} as const

export const retryAfterSeconds = (policy: RateLimitPolicy) =>
  policies[policy].windowMilliseconds / 1000

const make = Effect.gen(function*() {
  const buckets = new Map<string, Bucket>()

  const bucketKey = (policy: RateLimitPolicy, key: string) =>
    `${policy}:${key}`

  const take: RateLimiter["take"] = (policy, key) =>
    Effect.gen(function*() {
      const now = yield* Clock.currentTimeMillis
      return yield* Effect.sync(() => {
        const settings = policies[policy]
        const mapKey = bucketKey(policy, key)
        const bucket = buckets.get(mapKey) ?? {
          tokens: settings.size,
          lastRefill: now,
          lastSeen: now
        }
        const elapsed = now - bucket.lastRefill
        const refill = elapsed / settings.windowMilliseconds * settings.size
        bucket.tokens = Math.min(settings.size, bucket.tokens + refill)
        bucket.lastRefill = now
        bucket.lastSeen = now
        buckets.set(mapKey, bucket)

        if (bucket.tokens < 1) {
          return false
        }
        bucket.tokens -= 1
        return true
      })
    })

  const refund: RateLimiter["refund"] = (policy, key) =>
    Effect.sync(() => {
      const bucket = buckets.get(bucketKey(policy, key))
      if (bucket !== undefined) {
        bucket.tokens = Math.min(policies[policy].size, bucket.tokens + 1)
      }
    })

  const cleanup = Effect.gen(function*() {
    const now = yield* Clock.currentTimeMillis
    yield* Effect.sync(() => {
      for (const [key, bucket] of buckets) {
        if (now - bucket.lastSeen > 60 * 60 * 1000) {
          buckets.delete(key)
        }
      }
    })
  })

  yield* Effect.forkScoped(
    Effect.sleep("1 hour").pipe(
      Effect.zipRight(cleanup),
      Effect.forever
    )
  )

  return RateLimiter.of({ take, refund })
})

export const RateLimiterLive = Layer.scoped(RateLimiter, make)
