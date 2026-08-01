import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"

export type JournalLineValues = {
  readonly accountId: number
  readonly debit: number
  readonly credit: number
  readonly memo: string
}

export type JournalLine = {
  readonly id: number
  readonly entry_id: number
  readonly account_id: number
  readonly memo: string
  readonly account_code?: string
  readonly account_name?: string
  readonly debit: string
  readonly credit: string
}

export type JournalEntry = {
  readonly id: number
  readonly entry_date: string
  readonly reference: string
  readonly description: string
  readonly source_type: string
  readonly source_id: number | null
  readonly is_posted: boolean
  readonly created_by: number
  readonly created_at: string
  readonly updated_at: string
  readonly lines: ReadonlyArray<JournalLine> | null
  readonly created_by_name?: string
  readonly vehicle_id?: number
  readonly vehicle_code?: string
  readonly account_summary?: string
  readonly total_debit: string
  readonly total_credit: string
}

type EntryRow = {
  readonly id: number
  readonly entry_date: string
  readonly reference: string
  readonly description: string
  readonly source_type: string
  readonly source_id: number | null
  readonly is_posted: number
  readonly vehicle_id: number
  readonly created_by: number
  readonly created_at: string
  readonly updated_at: string
  readonly created_by_name: string
  readonly total_debit?: number
  readonly total_credit?: number
  readonly account_summary?: string
  readonly vehicle_code: string
}

type LineRow = {
  readonly id: number
  readonly entry_id: number
  readonly account_id: number
  readonly debit: number
  readonly credit: number
  readonly memo: string
  readonly account_code: string
  readonly account_name: string
}

export type CreateJournalValues = {
  readonly entryDate: string
  readonly reference?: string
  readonly description: string
  readonly sourceType: string
  readonly sourceId?: number
  readonly vehicleId?: number
  readonly isPosted: boolean
  readonly createdBy: number
  readonly lines: ReadonlyArray<JournalLineValues>
}

export type JournalFilter = {
  readonly dateFrom: string
  readonly dateTo: string
  readonly sourceType: string
  readonly search: string
  readonly limit: number
  readonly offset: number
}

export class JournalNotFound extends Data.TaggedError("JournalNotFound") {}

export class JournalConflict extends Data.TaggedError("JournalConflict")<{
  readonly message: string
}> {}

export class JournalValidationError extends Data.TaggedError(
  "JournalValidationError"
)<{
  readonly message: string
}> {}

export class JournalStoreError extends Data.TaggedError("JournalStoreError")<{
  readonly cause: unknown
}> {}

export interface Journals {
  readonly list: (
    filter: JournalFilter
  ) => Effect.Effect<{
    readonly entries: ReadonlyArray<JournalEntry>
    readonly total: number
  }, JournalStoreError>
  readonly get: (
    id: number
  ) => Effect.Effect<JournalEntry, JournalNotFound | JournalStoreError>
  readonly create: (
    values: CreateJournalValues
  ) => Effect.Effect<JournalEntry, JournalValidationError | JournalStoreError>
  readonly updateManual: (
    id: number,
    entryDate: string,
    description: string,
    lines: ReadonlyArray<JournalLineValues>
  ) => Effect.Effect<
    JournalEntry,
    JournalConflict | JournalNotFound | JournalValidationError | JournalStoreError
  >
  readonly updateBySource: (
    id: number,
    sourceType: string,
    entryDate: string,
    description: string,
    lines: ReadonlyArray<JournalLineValues>,
    vehicleId?: number
  ) => Effect.Effect<
    JournalEntry,
    JournalConflict | JournalNotFound | JournalValidationError | JournalStoreError
  >
  readonly removeManual: (
    id: number
  ) => Effect.Effect<
    JournalEntry,
    JournalConflict | JournalNotFound | JournalStoreError
  >
  readonly removeBySource: (
    id: number,
    sourceType: string
  ) => Effect.Effect<void, JournalConflict | JournalNotFound | JournalStoreError>
}

export const Journals = Context.GenericTag<Journals>("latasya/Journals")

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new JournalStoreError({ cause }))
  )

const validateBalance = (lines: ReadonlyArray<JournalLineValues>) => {
  const totalDebit = lines.reduce((sum, line) => sum + line.debit, 0)
  const totalCredit = lines.reduce((sum, line) => sum + line.credit, 0)
  if (totalDebit !== totalCredit) {
    return new JournalValidationError({
      message: `debits (${totalDebit}) must equal credits (${totalCredit})`
    })
  }
  if (totalDebit === 0) {
    return new JournalValidationError({
      message: "journal entry must have at least one debit and credit line"
    })
  }
  return undefined
}

const currentMonth = () => {
  const now = new Date()
  return `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, "0")}`
}

const fromLine = (row: LineRow): JournalLine => ({
  id: row.id,
  entry_id: row.entry_id,
  account_id: row.account_id,
  memo: row.memo,
  ...(row.account_code === "" ? {} : { account_code: row.account_code }),
  ...(row.account_name === "" ? {} : { account_name: row.account_name }),
  debit: String(row.debit),
  credit: String(row.credit)
})

const fromEntry = (
  row: EntryRow,
  lines: ReadonlyArray<JournalLine> | null,
  totalDebit: number,
  totalCredit: number
): JournalEntry => ({
  id: row.id,
  entry_date: row.entry_date,
  reference: row.reference,
  description: row.description,
  source_type: row.source_type,
  source_id: row.source_id,
  is_posted: row.is_posted !== 0,
  ...(row.vehicle_id === 0 ? {} : { vehicle_id: row.vehicle_id }),
  created_by: row.created_by,
  created_at: row.created_at,
  updated_at: row.updated_at,
  lines,
  ...(row.created_by_name === ""
    ? {}
    : { created_by_name: row.created_by_name }),
  ...(row.account_summary === undefined || row.account_summary === ""
    ? {}
    : { account_summary: row.account_summary }),
  ...(row.vehicle_code === "" ? {} : { vehicle_code: row.vehicle_code }),
  total_debit: String(totalDebit),
  total_credit: String(totalCredit)
})

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient

  const linesFor = (entryId: number) =>
    store(sql<LineRow>`
      SELECT
        jl.id,
        jl.entry_id,
        jl.account_id,
        jl.debit,
        jl.credit,
        COALESCE(jl.memo, '') AS memo,
        a.code AS account_code,
        a.name AS account_name
      FROM journal_lines jl
      JOIN accounts a ON a.id = jl.account_id
      WHERE jl.entry_id = ${entryId}
      ORDER BY jl.id
    `).pipe(
      Effect.map((rows) => rows.map(fromLine))
    )

  const get: Journals["get"] = (id) =>
    Effect.gen(function*() {
      const rows = yield* store(sql<EntryRow>`
        SELECT
          je.id,
          je.entry_date,
          COALESCE(je.reference, '') AS reference,
          je.description,
          COALESCE(je.source_type, '') AS source_type,
          je.source_id,
          je.is_posted,
          COALESCE(je.vehicle_id, 0) AS vehicle_id,
          je.created_by,
          je.created_at,
          je.updated_at,
          u.full_name AS created_by_name,
          COALESCE(v.code, '') AS vehicle_code
        FROM journal_entries je
        JOIN users u ON u.id = je.created_by
        LEFT JOIN vehicles v ON v.id = je.vehicle_id
        WHERE je.id = ${id}
      `)
      const row = rows[0]
      if (row === undefined) {
        return yield* new JournalNotFound()
      }
      const lines = yield* linesFor(id)
      const totalDebit = lines.reduce(
        (sum, line) => sum + Number(line.debit),
        0
      )
      const totalCredit = lines.reduce(
        (sum, line) => sum + Number(line.credit),
        0
      )
      return fromEntry(row, lines, totalDebit, totalCredit)
    })

  const generateReference = Effect.gen(function*() {
    const prefix = `JE-${currentMonth()}`
    const rows = yield* store(sql<{ readonly maximum: number }>`
      SELECT COALESCE(
        MAX(CAST(SUBSTR(reference, ${prefix.length + 2}) AS INTEGER)),
        0
      ) AS maximum
      FROM journal_entries
      WHERE reference LIKE ${`${prefix}-%`}
    `)
    return `${prefix}-${String((rows[0]?.maximum ?? 0) + 1).padStart(4, "0")}`
  })

  const insertLines = (
    entryId: number,
    lines: ReadonlyArray<JournalLineValues>
  ) => Effect.forEach(
    lines,
    (line) => sql`
      INSERT INTO journal_lines (
        entry_id, account_id, debit, credit, memo
      )
      VALUES (
        ${entryId},
        ${line.accountId},
        ${line.debit},
        ${line.credit},
        ${line.memo}
      )
    `,
    { discard: true }
  )

  const create: Journals["create"] = (values) =>
    Effect.gen(function*() {
      const invalid = validateBalance(values.lines)
      if (invalid !== undefined) {
        return yield* invalid
      }
      const reference = values.reference ?? (yield* generateReference)
      const id = yield* store(sql.withTransaction(
        Effect.gen(function*() {
          yield* sql`
            INSERT INTO journal_entries (
              entry_date,
              reference,
              description,
              source_type,
              source_id,
              is_posted,
              vehicle_id,
              created_by
            )
            VALUES (
              ${values.entryDate},
              ${reference},
              ${values.description},
              ${values.sourceType},
              ${values.sourceId ?? null},
              ${values.isPosted ? 1 : 0},
              ${values.vehicleId ?? null},
              ${values.createdBy}
            )
          `
          const ids = yield* sql<{ readonly id: number }>`
            SELECT last_insert_rowid() AS id
          `
          const entryId = ids[0]?.id ?? 0
          yield* insertLines(entryId, values.lines)
          return entryId
        })
      ))
      return yield* get(id).pipe(
        Effect.catchTag(
          "JournalNotFound",
          (cause) => new JournalStoreError({ cause })
        )
      )
    })

  const updateValues = (
    id: number,
    entryDate: string,
    description: string,
    lines: ReadonlyArray<JournalLineValues>,
    vehicleId?: number
  ) =>
    Effect.gen(function*() {
      const invalid = validateBalance(lines)
      if (invalid !== undefined) {
        return yield* invalid
      }
      yield* store(sql.withTransaction(
        Effect.gen(function*() {
          yield* sql`
            UPDATE journal_entries
            SET
              entry_date = ${entryDate},
              description = ${description},
              vehicle_id = ${vehicleId ?? null},
              updated_at = datetime('now')
            WHERE id = ${id}
          `
          yield* sql`DELETE FROM journal_lines WHERE entry_id = ${id}`
          yield* insertLines(id, lines)
        })
      ))
      return yield* get(id)
    })

  const updateManual: Journals["updateManual"] = (
    id,
    entryDate,
    description,
    lines
  ) =>
    Effect.gen(function*() {
      const existing = yield* get(id)
      if (
        existing.source_type !== "" &&
        existing.source_type !== "manual"
      ) {
        return yield* new JournalConflict({
          message: "cannot edit auto-generated journal entry"
        })
      }
      return yield* updateValues(id, entryDate, description, lines)
    })

  const updateBySource: Journals["updateBySource"] = (
    id,
    sourceType,
    entryDate,
    description,
    lines,
    vehicleId
  ) =>
    Effect.gen(function*() {
      const existing = yield* get(id)
      if (existing.source_type !== sourceType) {
        return yield* new JournalConflict({
          message: `journal entry ${id} is not a ${sourceType} entry`
        })
      }
      return yield* updateValues(id, entryDate, description, lines, vehicleId)
    })

  const removeManual: Journals["removeManual"] = (id) =>
    Effect.gen(function*() {
      const existing = yield* get(id)
      if (
        existing.source_type !== "" &&
        existing.source_type !== "manual"
      ) {
        return yield* new JournalConflict({
          message:
            `cannot delete auto-generated journal entry (source: ${existing.source_type})`
        })
      }
      yield* store(sql`DELETE FROM journal_entries WHERE id = ${id}`)
      return existing
    })

  const removeBySource: Journals["removeBySource"] = (id, sourceType) =>
    Effect.gen(function*() {
      const existing = yield* get(id)
      if (existing.source_type !== sourceType) {
        return yield* new JournalConflict({
          message: `journal entry ${id} is not a ${sourceType} entry`
        })
      }
      yield* store(sql`DELETE FROM journal_entries WHERE id = ${id}`)
    })

  const list: Journals["list"] = (filter) => {
    let where = ""
    const params: Array<unknown> = []
    if (filter.dateFrom !== "") {
      where += " AND je.entry_date >= ?"
      params.push(filter.dateFrom)
    }
    if (filter.dateTo !== "") {
      where += " AND je.entry_date <= ?"
      params.push(filter.dateTo)
    }
    if (filter.sourceType !== "") {
      where += " AND je.source_type = ?"
      params.push(filter.sourceType)
    }
    if (filter.search !== "") {
      where += " AND (je.reference LIKE ? OR je.description LIKE ?)"
      params.push(`%${filter.search}%`, `%${filter.search}%`)
    }
    const accountExpression = filter.sourceType === "income"
      ? `COALESCE((
          SELECT a.code || ' ' || a.name
          FROM journal_lines jl2
          JOIN accounts a ON a.id = jl2.account_id
          WHERE jl2.entry_id = je.id AND jl2.credit > 0
          ORDER BY jl2.id LIMIT 1
        ), '')`
      : filter.sourceType === "expense"
      ? `COALESCE((
          SELECT a.code || ' ' || a.name
          FROM journal_lines jl2
          JOIN accounts a ON a.id = jl2.account_id
          WHERE jl2.entry_id = je.id AND jl2.debit > 0
          ORDER BY jl2.id LIMIT 1
        ), '')`
      : "''"
    const countQuery = `
      SELECT COUNT(*) AS count
      FROM journal_entries je
      JOIN users u ON u.id = je.created_by
      WHERE 1 = 1 ${where}
    `
    let listQuery = `
      SELECT
        je.id,
        je.entry_date,
        COALESCE(je.reference, '') AS reference,
        je.description,
        COALESCE(je.source_type, '') AS source_type,
        je.source_id,
        je.is_posted,
        COALESCE(je.vehicle_id, 0) AS vehicle_id,
        je.created_by,
        je.created_at,
        je.updated_at,
        u.full_name AS created_by_name,
        COALESCE((
          SELECT SUM(debit) FROM journal_lines WHERE entry_id = je.id
        ), 0) AS total_debit,
        COALESCE((
          SELECT SUM(credit) FROM journal_lines WHERE entry_id = je.id
        ), 0) AS total_credit,
        ${accountExpression} AS account_summary,
        COALESCE(v.code, '') AS vehicle_code
      FROM journal_entries je
      JOIN users u ON u.id = je.created_by
      LEFT JOIN vehicles v ON v.id = je.vehicle_id
      WHERE 1 = 1 ${where}
      ORDER BY je.entry_date DESC, je.id DESC
    `
    const listParams = [...params]
    if (filter.limit > 0) {
      listQuery += " LIMIT ? OFFSET ?"
      listParams.push(filter.limit, filter.offset)
    }
    return Effect.gen(function*() {
      const counts = yield* store(sql.unsafe<{ readonly count: number }>(
        countQuery,
        params
      ))
      const rows = yield* store(sql.unsafe<EntryRow>(listQuery, listParams))
      return {
        entries: rows.map((row) =>
          fromEntry(
            row,
            null,
            row.total_debit ?? 0,
            row.total_credit ?? 0
          )
        ),
        total: counts[0]?.count ?? 0
      }
    })
  }

  return Journals.of({
    list,
    get,
    create,
    updateManual,
    updateBySource,
    removeManual,
    removeBySource
  })
})

export const JournalsLive = Layer.effect(Journals, make)

export type JournalInputLine = {
  readonly accountId: number
  readonly debit: string
  readonly credit: string
  readonly memo: string
}

const parseIdr = (value: string) => {
  const trimmed = value.trim()
  if (trimmed === "") {
    return 0
  }
  if (!/^[+-]?\d+$/.test(trimmed)) {
    return undefined
  }
  const parsed = Number(trimmed)
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : undefined
}

export const validateJournal = (input: {
  readonly entryDate: string
  readonly description: string
  readonly lines: ReadonlyArray<JournalInputLine>
}): {
  readonly fields: Readonly<Record<string, string>>
  readonly lines?: ReadonlyArray<JournalLineValues>
} => {
  const fields: Record<string, string> = {}
  if (input.entryDate.trim() === "") {
    fields.entry_date = "required"
  }
  if (input.description.trim() === "") {
    fields.description = "required"
  }
  if (input.lines.length < 2) {
    fields.lines = "at least two lines required"
  }
  if (input.lines.length > 100) {
    fields.lines = "too many lines (max 100)"
  }

  let totalDebit = 0
  let totalCredit = 0
  const lines: Array<JournalLineValues> = []
  input.lines.forEach((line, index) => {
    if (line.accountId <= 0) {
      fields[`lines[${index}].account_id`] = "required"
      return
    }
    const debit = parseIdr(line.debit)
    if (debit === undefined) {
      fields[`lines[${index}].debit`] = "invalid amount"
      return
    }
    const credit = parseIdr(line.credit)
    if (credit === undefined) {
      fields[`lines[${index}].credit`] = "invalid amount"
      return
    }
    if (debit > 0 && credit > 0) {
      fields[`lines[${index}]`] = "cannot have both debit and credit"
      return
    }
    totalDebit += debit
    totalCredit += credit
    lines.push({
      accountId: line.accountId,
      debit,
      credit,
      memo: line.memo
    })
  })

  if (Object.keys(fields).length === 0) {
    if (totalDebit === 0 || totalCredit === 0) {
      fields.lines = "must have at least one debit and one credit line"
    } else if (totalDebit !== totalCredit) {
      fields.lines = "debits must equal credits"
    }
  }
  return Object.keys(fields).length > 0 ? { fields } : { fields, lines }
}
