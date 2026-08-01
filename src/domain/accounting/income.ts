import { Context, Data, Effect, Layer } from "effect"
import {
  JournalConflict,
  Journals,
  JournalStoreError,
  JournalValidationError,
  type JournalEntry,
  type JournalLine
} from "./journals.ts"

export type AccountReference = {
  readonly id: number
  readonly code: string
  readonly name: string
}

export type IncomeEntry = {
  readonly id: number
  readonly reference: string
  readonly entry_date: string
  readonly description: string
  readonly amount: string
  readonly revenue_account?: AccountReference
  readonly deposit_account?: AccountReference
  readonly created_at: string
}

export type IncomeValues = {
  readonly entryDate: string
  readonly description: string
  readonly amount: number
  readonly revenueAccount: number
  readonly depositAccount: number
}

export type IncomeFilter = {
  readonly dateFrom: string
  readonly dateTo: string
  readonly search: string
  readonly limit: number
  readonly offset: number
}

export class IncomeNotFound extends Data.TaggedError("IncomeNotFound") {}

export interface Income {
  readonly list: (
    filter: IncomeFilter
  ) => Effect.Effect<{
    readonly entries: ReadonlyArray<IncomeEntry>
    readonly total: number
  }, JournalStoreError>
  readonly get: (
    id: number
  ) => Effect.Effect<IncomeEntry, IncomeNotFound | JournalStoreError>
  readonly create: (
    values: IncomeValues,
    createdBy: number
  ) => Effect.Effect<
    IncomeEntry,
    JournalValidationError | JournalStoreError
  >
  readonly update: (
    id: number,
    values: IncomeValues
  ) => Effect.Effect<{
    readonly before: IncomeEntry
    readonly after: IncomeEntry
  }, IncomeNotFound | JournalConflict | JournalValidationError | JournalStoreError>
  readonly remove: (
    id: number
  ) => Effect.Effect<
    IncomeEntry,
    IncomeNotFound | JournalConflict | JournalStoreError
  >
}

export const Income = Context.GenericTag<Income>("latasya/Income")

const accountReference = (line: JournalLine): AccountReference => ({
  id: line.account_id,
  code: line.account_code ?? "",
  name: line.account_name ?? ""
})

const fromJournal = (entry: JournalEntry): IncomeEntry => {
  let depositAccount: AccountReference | undefined
  let revenueAccount: AccountReference | undefined
  for (const line of entry.lines ?? []) {
    if (Number(line.debit) > 0) {
      depositAccount = accountReference(line)
    }
    if (Number(line.credit) > 0) {
      revenueAccount = accountReference(line)
    }
  }
  return {
    id: entry.id,
    reference: entry.reference,
    entry_date: entry.entry_date,
    description: entry.description,
    amount: entry.total_debit,
    ...(revenueAccount === undefined
      ? {}
      : { revenue_account: revenueAccount }),
    ...(depositAccount === undefined
      ? {}
      : { deposit_account: depositAccount }),
    created_at: entry.created_at
  }
}

const requireIncome = (entry: JournalEntry) =>
  entry.source_type === "income"
    ? Effect.succeed(entry)
    : Effect.fail(new IncomeNotFound())

const linesFor = (values: IncomeValues) => [
  {
    accountId: values.depositAccount,
    debit: values.amount,
    credit: 0,
    memo: ""
  },
  {
    accountId: values.revenueAccount,
    debit: 0,
    credit: values.amount,
    memo: ""
  }
] as const

const make = Effect.gen(function*() {
  const journals = yield* Journals

  const get = (id: number) =>
    journals.get(id).pipe(
      Effect.catchTag(
        "JournalNotFound",
        () => Effect.fail(new IncomeNotFound())
      ),
      Effect.flatMap(requireIncome),
      Effect.map(fromJournal)
    )

  const list: Income["list"] = (filter) =>
    journals.list({
      ...filter,
      sourceType: "income"
    }).pipe(
      Effect.map(({ entries, total }) => ({
        entries: entries.map(fromJournal),
        total
      }))
    )

  const create: Income["create"] = (values, createdBy) =>
    journals.create({
      entryDate: values.entryDate,
      description: values.description,
      sourceType: "income",
      isPosted: true,
      createdBy,
      lines: linesFor(values)
    }).pipe(Effect.map(fromJournal))

  const update: Income["update"] = (id, values) =>
    Effect.gen(function*() {
      const before = yield* get(id)
      const after = yield* journals.updateBySource(
        id,
        "income",
        values.entryDate,
        values.description,
        linesFor(values)
      ).pipe(
        Effect.catchTag(
          "JournalNotFound",
          () => Effect.fail(new IncomeNotFound())
        ),
        Effect.map(fromJournal)
      )
      return { before, after }
    })

  const remove: Income["remove"] = (id) =>
    Effect.gen(function*() {
      const existing = yield* get(id)
      yield* journals.removeBySource(id, "income").pipe(
        Effect.catchTag(
          "JournalNotFound",
          () => Effect.fail(new IncomeNotFound())
        )
      )
      return existing
    })

  return Income.of({ list, get, create, update, remove })
})

export const IncomeLive = Layer.effect(Income, make)

const parsePositiveInteger = (value: string) => {
  if (!/^[+-]?\d+$/.test(value)) {
    return undefined
  }
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined
}

export const validateIncome = (input: {
  readonly entryDate: string
  readonly description: string
  readonly amount: string
  readonly revenueAccount: number
  readonly depositAccount: number
}) => {
  const fields: Record<string, string> = {}
  if (input.entryDate === "") {
    fields.entry_date = "required"
  }
  if (input.description === "") {
    fields.description = "required"
  }
  const amount = input.amount === ""
    ? undefined
    : parsePositiveInteger(input.amount)
  if (input.amount === "") {
    fields.amount = "required"
  } else if (amount === undefined) {
    fields.amount = "must be a positive integer"
  }
  if (input.revenueAccount === 0) {
    fields.revenue_account = "required"
  }
  if (input.depositAccount === 0) {
    fields.deposit_account = "required"
  }
  return Object.keys(fields).length > 0
    ? { fields }
    : { fields, amount: amount as number }
}
