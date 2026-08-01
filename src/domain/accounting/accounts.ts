import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"

export type Account = {
  readonly id: number
  readonly code: string
  readonly name: string
  readonly account_type: string
  readonly normal_balance: string
  readonly parent_id?: number
  readonly is_system: boolean
  readonly is_active: boolean
  readonly is_cash: boolean
  readonly description: string
  readonly created_at: string
  readonly updated_at: string
}

type AccountRow = Omit<
  Account,
  "parent_id" | "is_system" | "is_active" | "is_cash"
> & {
  readonly parent_id: number | null
  readonly is_system: number
  readonly is_active: number
  readonly is_cash: number
}

export type AccountValues = {
  readonly code: string
  readonly name: string
  readonly accountType: string
  readonly normalBalance: string
  readonly isActive: boolean
  readonly isCash: boolean
  readonly description: string
}

export type AccountFilter = {
  readonly type: string
  readonly search: string
}

export class AccountNotFound extends Data.TaggedError("AccountNotFound") {}

export class AccountConflict extends Data.TaggedError("AccountConflict")<{
  readonly reason: "duplicate_code" | "system" | "linked_transactions"
}> {}

export class AccountStoreError extends Data.TaggedError("AccountStoreError")<{
  readonly cause: unknown
}> {}

export interface Accounts {
  readonly list: (
    filter: AccountFilter
  ) => Effect.Effect<ReadonlyArray<Account>, AccountStoreError>
  readonly get: (
    id: number
  ) => Effect.Effect<Account, AccountNotFound | AccountStoreError>
  readonly create: (
    values: AccountValues
  ) => Effect.Effect<Account, AccountConflict | AccountStoreError>
  readonly update: (
    id: number,
    values: AccountValues
  ) => Effect.Effect<Account, AccountNotFound | AccountStoreError>
  readonly remove: (
    id: number
  ) => Effect.Effect<
    Account,
    AccountConflict | AccountNotFound | AccountStoreError
  >
}

export const Accounts = Context.GenericTag<Accounts>("latasya/Accounts")

const columns = `
  id, code, name, account_type, normal_balance, parent_id,
  is_system, is_active, is_cash, COALESCE(description, '') AS description,
  created_at, updated_at
`

const fromRow = (row: AccountRow): Account => ({
  id: row.id,
  code: row.code,
  name: row.name,
  account_type: row.account_type,
  normal_balance: row.normal_balance,
  ...(row.parent_id === null ? {} : { parent_id: row.parent_id }),
  is_system: row.is_system !== 0,
  is_active: row.is_active !== 0,
  is_cash: row.is_cash !== 0,
  description: row.description,
  created_at: row.created_at,
  updated_at: row.updated_at
})

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new AccountStoreError({ cause }))
  )

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient

  const get: Accounts["get"] = (id) =>
    Effect.gen(function*() {
      const rows = yield* store(sql.unsafe<AccountRow>(
        `SELECT ${columns} FROM accounts WHERE id = ?`,
        [id]
      ))
      const row = rows[0]
      if (row === undefined) {
        return yield* new AccountNotFound()
      }
      return fromRow(row)
    })

  const list: Accounts["list"] = (filter) => {
    let query = `SELECT ${columns} FROM accounts WHERE 1 = 1`
    const params: Array<unknown> = []
    if (filter.type !== "") {
      query += " AND account_type = ?"
      params.push(filter.type)
    }
    if (filter.search !== "") {
      query += " AND (code LIKE ? OR name LIKE ?)"
      params.push(`%${filter.search}%`, `%${filter.search}%`)
    }
    query += " ORDER BY code"
    return store(sql.unsafe<AccountRow>(query, params)).pipe(
      Effect.map((rows) => rows.map(fromRow))
    )
  }

  const create: Accounts["create"] = (values) =>
    Effect.gen(function*() {
      const duplicate = yield* store(sql<{ readonly found: number }>`
        SELECT 1 AS found
        FROM accounts
        WHERE code = ${values.code}
      `)
      if (duplicate.length > 0) {
        return yield* new AccountConflict({ reason: "duplicate_code" })
      }
      yield* store(sql`
        INSERT INTO accounts (
          code,
          name,
          account_type,
          normal_balance,
          parent_id,
          is_system,
          is_active,
          is_cash,
          description
        )
        VALUES (
          ${values.code},
          ${values.name},
          ${values.accountType},
          ${values.normalBalance},
          NULL,
          0,
          ${values.isActive ? 1 : 0},
          ${values.isCash ? 1 : 0},
          ${values.description}
        )
      `)
      const rows = yield* store(sql.unsafe<AccountRow>(
        `SELECT ${columns} FROM accounts WHERE code = ?`,
        [values.code]
      ))
      const row = rows[0]
      if (row === undefined) {
        return yield* new AccountStoreError({
          cause: new Error("created account could not be read")
        })
      }
      return fromRow(row)
    })

  const update: Accounts["update"] = (id, values) =>
    Effect.gen(function*() {
      const existing = yield* get(id)
      yield* store(sql`
        UPDATE accounts
        SET
          code = ${values.code},
          name = ${values.name},
          account_type = ${values.accountType},
          normal_balance = ${values.normalBalance},
          parent_id = ${existing.parent_id ?? null},
          is_active = ${values.isActive ? 1 : 0},
          is_cash = ${values.isCash ? 1 : 0},
          description = ${values.description},
          updated_at = datetime('now')
        WHERE id = ${id}
      `)
      return yield* get(id)
    })

  const remove: Accounts["remove"] = (id) =>
    Effect.gen(function*() {
      const account = yield* get(id)
      if (account.is_system) {
        return yield* new AccountConflict({ reason: "system" })
      }
      const counts = yield* store(sql<{ readonly count: number }>`
        SELECT COUNT(*) AS count
        FROM journal_lines
        WHERE account_id = ${id}
      `)
      if ((counts[0]?.count ?? 0) > 0) {
        return yield* new AccountConflict({ reason: "linked_transactions" })
      }
      yield* store(sql`
        DELETE FROM accounts
        WHERE id = ${id} AND is_system = 0
      `)
      return account
    })

  return Accounts.of({ list, get, create, update, remove })
})

export const AccountsLive = Layer.effect(Accounts, make)

const accountTypes = new Set([
  "asset",
  "liability",
  "equity",
  "revenue",
  "expense"
])
const normalBalances = new Set(["debit", "credit"])

export const validateAccount = (
  input: {
    readonly code: string
    readonly name: string
    readonly accountType: string
    readonly normalBalance: string
    readonly isCash: boolean | undefined
  }
): Readonly<Record<string, string>> => {
  const fields: Record<string, string> = {}
  if (input.code.trim() === "") {
    fields.code = "required"
  }
  if (input.name.trim() === "") {
    fields.name = "required"
  }
  if (input.accountType === "") {
    fields.account_type = "required"
  } else if (!accountTypes.has(input.accountType)) {
    fields.account_type =
      "must be one of: asset, liability, equity, revenue, expense"
  }
  if (input.normalBalance === "") {
    fields.normal_balance = "required"
  } else if (!normalBalances.has(input.normalBalance)) {
    fields.normal_balance = "must be one of: debit, credit"
  }
  if (
    input.isCash === true &&
    (input.accountType !== "asset" || input.normalBalance !== "debit")
  ) {
    fields.is_cash = "cash accounts must be debit-normal assets"
  }
  return fields
}
