import { describe, expect, test } from "bun:test"
import { Effect } from "effect"
import {
  PasswordHasher,
  PasswordHasherLive
} from "./password.ts"

const run = <A>(effect: Effect.Effect<A, unknown, PasswordHasher>) =>
  Effect.runPromise(effect.pipe(Effect.provide(PasswordHasherLive)))

describe("PasswordHasher", () => {
  test("creates Go-compatible bcrypt cost-10 hashes", async () => {
    const result = await run(Effect.gen(function*() {
      const passwords = yield* PasswordHasher
      const hash = yield* passwords.hash("compat-password")
      const verified = yield* passwords.verify("compat-password", hash)
      return { hash, verified }
    }))

    expect(result.hash).toStartWith("$2b$10$")
    expect(result.verified).toBe(true)
  })

  test("verifies an existing bcrypt hash", async () => {
    const hash = "$2y$10$pAdwMt4k0p2lkAoQ1Ua7SOi8s.TAowGlWSFS4PmAsg5qt/cGy3/mS"
    const verified = await run(Effect.gen(function*() {
      const passwords = yield* PasswordHasher
      return yield* passwords.verify("compat-password", hash)
    }))

    expect(verified).toBe(true)
  })

  test("matches Go bcrypt's 72-byte input limit", async () => {
    const tooLong = "x".repeat(73)
    const exit = await Effect.runPromiseExit(
      Effect.gen(function*() {
        const passwords = yield* PasswordHasher
        return yield* passwords.hash(tooLong)
      }).pipe(Effect.provide(PasswordHasherLive))
    )

    expect(exit._tag).toBe("Failure")
  })
})
