import { Context, Data, Effect, Layer } from "effect"

export class PasswordHashError extends Data.TaggedError("PasswordHashError")<{
  readonly cause: unknown
}> {}

export interface PasswordHasher {
  readonly hash: (
    password: string
  ) => Effect.Effect<string, PasswordHashError>
  readonly verify: (
    password: string,
    hash: string
  ) => Effect.Effect<boolean>
}

export const PasswordHasher = Context.GenericTag<PasswordHasher>(
  "latasya/PasswordHasher"
)

const byteLength = (value: string) => new TextEncoder().encode(value).byteLength

const live: PasswordHasher = {
  hash: (password) => {
    if (byteLength(password) > 72) {
      return Effect.fail(new PasswordHashError({
        cause: new Error("bcrypt password exceeds 72 bytes")
      }))
    }

    return Effect.tryPromise({
      try: () => Bun.password.hash(password, {
        algorithm: "bcrypt",
        cost: 10
      }),
      catch: (cause) => new PasswordHashError({ cause })
    })
  },
  verify: (password, hash) =>
    byteLength(password) > 72
      ? Effect.succeed(false)
      : Effect.tryPromise(() => Bun.password.verify(password, hash)).pipe(
        Effect.catchAll(() => Effect.succeed(false))
      )
}

export const PasswordHasherLive = Layer.succeed(PasswordHasher, live)
