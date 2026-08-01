import { Context, Data, Effect, Layer } from "effect"
import {
  JournalConflict,
  Journals,
  JournalStoreError,
  JournalValidationError,
  type JournalEntry,
  type JournalLine
} from "./journals.ts"

export type ExpenseAccountReference = {
  readonly id: number
  readonly code: string
  readonly name: string
}

export type ExpenseEntry = {
  readonly id: number
  readonly reference: string
  readonly entry_date: string
  readonly description: string
  readonly amount: string
  readonly expense_account?: ExpenseAccountReference
  readonly payment_account?: ExpenseAccountReference
  readonly created_at: string
}

export type ExpenseValues = {
  readonly entryDate: string
  readonly description: string
  readonly amount: number
  readonly expenseAccount: number
  readonly paymentAccount: number
}

export type ExpenseFilter = {
  readonly dateFrom: string
  readonly dateTo: string
  readonly search: string
  readonly limit: number
  readonly offset: number
}

export class ExpenseNotFound extends Data.TaggedError("ExpenseNotFound") {}

export interface Expenses {
  readonly list: (
    filter: ExpenseFilter
  ) => Effect.Effect<{
    readonly entries: ReadonlyArray<ExpenseEntry>
    readonly total: number
  }, JournalStoreError>
  readonly get: (
    id: number
  ) => Effect.Effect<ExpenseEntry, ExpenseNotFound | JournalStoreError>
  readonly create: (
    values: ExpenseValues,
    createdBy: number
  ) => Effect.Effect<
    ExpenseEntry,
    JournalValidationError | JournalStoreError
  >
  readonly update: (
    id: number,
    values: ExpenseValues
  ) => Effect.Effect<{
    readonly before: ExpenseEntry
    readonly after: ExpenseEntry
  }, ExpenseNotFound | JournalConflict | JournalValidationError | JournalStoreError>
  readonly remove: (
    id: number
  ) => Effect.Effect<
    ExpenseEntry,
    ExpenseNotFound | JournalConflict | JournalStoreError
  >
}

export const Expenses = Context.GenericTag<Expenses>("latasya/Expenses")

const accountReference = (
  line: JournalLine
): ExpenseAccountReference => ({
  id: line.account_id,
  code: line.account_code ?? "",
  name: line.account_name ?? ""
})

const fromJournal = (entry: JournalEntry): ExpenseEntry => {
  let expenseAccount: ExpenseAccountReference | undefined
  let paymentAccount: ExpenseAccountReference | undefined
  for (const line of entry.lines ?? []) {
    if (Number(line.debit) > 0) {
      expenseAccount = accountReference(line)
    }
    if (Number(line.credit) > 0) {
      paymentAccount = accountReference(line)
    }
  }
  return {
    id: entry.id,
    reference: entry.reference,
    entry_date: entry.entry_date,
    description: entry.description,
    amount: entry.total_debit,
    ...(expenseAccount === undefined
      ? {}
      : { expense_account: expenseAccount }),
    ...(paymentAccount === undefined
      ? {}
      : { payment_account: paymentAccount }),
    created_at: entry.created_at
  }
}

const requireExpense = (entry: JournalEntry) =>
  entry.source_type === "expense"
    ? Effect.succeed(entry)
    : Effect.fail(new ExpenseNotFound())

const linesFor = (values: ExpenseValues) => [
  {
    accountId: values.expenseAccount,
    debit: values.amount,
    credit: 0,
    memo: ""
  },
  {
    accountId: values.paymentAccount,
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
        () => Effect.fail(new ExpenseNotFound())
      ),
      Effect.flatMap(requireExpense),
      Effect.map(fromJournal)
    )

  const list: Expenses["list"] = (filter) =>
    journals.list({
      ...filter,
      sourceType: "expense"
    }).pipe(
      Effect.map(({ entries, total }) => ({
        entries: entries.map(fromJournal),
        total
      }))
    )

  const create: Expenses["create"] = (values, createdBy) =>
    journals.create({
      entryDate: values.entryDate,
      description: values.description,
      sourceType: "expense",
      isPosted: true,
      createdBy,
      lines: linesFor(values)
    }).pipe(Effect.map(fromJournal))

  const update: Expenses["update"] = (id, values) =>
    Effect.gen(function*() {
      const before = yield* get(id)
      const after = yield* journals.updateBySource(
        id,
        "expense",
        values.entryDate,
        values.description,
        linesFor(values)
      ).pipe(
        Effect.catchTag(
          "JournalNotFound",
          () => Effect.fail(new ExpenseNotFound())
        ),
        Effect.map(fromJournal)
      )
      return { before, after }
    })

  const remove: Expenses["remove"] = (id) =>
    Effect.gen(function*() {
      const existing = yield* get(id)
      yield* journals.removeBySource(id, "expense").pipe(
        Effect.catchTag(
          "JournalNotFound",
          () => Effect.fail(new ExpenseNotFound())
        )
      )
      return existing
    })

  return Expenses.of({ list, get, create, update, remove })
})

export const ExpensesLive = Layer.effect(Expenses, make)

const parsePositiveInteger = (value: string) => {
  if (!/^[+-]?\d+$/.test(value)) {
    return undefined
  }
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined
}

export const validateExpense = (input: {
  readonly entryDate: string
  readonly description: string
  readonly amount: string
  readonly expenseAccount: number
  readonly paymentAccount: number
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
  if (input.expenseAccount === 0) {
    fields.expense_account = "required"
  }
  if (input.paymentAccount === 0) {
    fields.payment_account = "required"
  }
  return Object.keys(fields).length > 0
    ? { fields }
    : { fields, amount: amount as number }
}
