import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"
import { Journals } from "./journals.ts"

export type CreditNoteLineValues = {
  readonly description: string
  readonly quantity: number
  readonly unitPrice: number
  readonly accountId: number
}

export type CreditNoteLine = {
  readonly id: number
  readonly credit_note_id: number
  readonly description: string
  readonly quantity: number
  readonly unit_price: number
  readonly amount: number
  readonly account_id: number
  readonly account_code?: string
  readonly account_name?: string
}

export type CreditNote = {
  readonly id: number
  readonly cn_number: string
  readonly contact_id: number
  readonly invoice_id?: number
  readonly cn_date: string
  readonly reason: string
  readonly status: string
  readonly subtotal: number
  readonly tax_amount: number
  readonly total: number
  readonly notes: string
  readonly journal_id?: number
  readonly created_by: number
  readonly created_at: string
  readonly updated_at: string
  readonly contact_name?: string
  readonly invoice_number?: string
  readonly lines?: ReadonlyArray<CreditNoteLine>
}

export type CreditNoteValues = {
  readonly contactId: number
  readonly invoiceId?: number
  readonly cnDate: string
  readonly reason: string
  readonly taxAmount: number
  readonly notes: string
  readonly lines: ReadonlyArray<CreditNoteLineValues>
}

export type CreditNoteFilter = {
  readonly status: string
  readonly search: string
  readonly limit: number
  readonly offset: number
}

export class CreditNoteNotFound extends Data.TaggedError(
  "CreditNoteNotFound"
) {}

export class CreditNoteConflict extends Data.TaggedError(
  "CreditNoteConflict"
)<{
  readonly message: string
}> {}

export class CreditNoteStoreError extends Data.TaggedError(
  "CreditNoteStoreError"
)<{
  readonly cause: unknown
}> {}

export interface CreditNotes {
  readonly list: (
    filter: CreditNoteFilter
  ) => Effect.Effect<{
    readonly creditNotes: ReadonlyArray<CreditNote>
    readonly total: number
  }, CreditNoteStoreError>
  readonly get: (
    id: number
  ) => Effect.Effect<CreditNote, CreditNoteNotFound | CreditNoteStoreError>
  readonly create: (
    values: CreditNoteValues,
    createdBy: number
  ) => Effect.Effect<CreditNote, CreditNoteStoreError>
  readonly update: (
    id: number,
    values: CreditNoteValues
  ) => Effect.Effect<
    { readonly before: CreditNote; readonly after: CreditNote },
    CreditNoteConflict | CreditNoteNotFound | CreditNoteStoreError
  >
  readonly remove: (
    id: number
  ) => Effect.Effect<
    CreditNote,
    CreditNoteConflict | CreditNoteNotFound | CreditNoteStoreError
  >
  readonly issue: (
    id: number,
    userId: number
  ) => Effect.Effect<
    CreditNote,
    CreditNoteConflict | CreditNoteNotFound | CreditNoteStoreError
  >
  readonly void: (
    id: number,
    userId: number
  ) => Effect.Effect<
    CreditNote,
    CreditNoteConflict | CreditNoteNotFound | CreditNoteStoreError
  >
}

export const CreditNotes = Context.GenericTag<CreditNotes>(
  "latasya/CreditNotes"
)

type CreditNoteRow = {
  readonly id: number
  readonly cn_number: string
  readonly contact_id: number
  readonly invoice_id: number | null
  readonly cn_date: string
  readonly reason: string
  readonly status: string
  readonly subtotal: number
  readonly tax_amount: number
  readonly total: number
  readonly notes: string
  readonly journal_id: number | null
  readonly created_by: number
  readonly created_at: string
  readonly updated_at: string
  readonly contact_name: string
  readonly invoice_number: string
}

type CreditNoteLineRow = {
  readonly id: number
  readonly credit_note_id: number
  readonly description: string
  readonly quantity: number
  readonly unit_price: number
  readonly amount: number
  readonly account_id: number
  readonly account_code: string
  readonly account_name: string
}

type InvoiceCreditRow = {
  readonly total: number
  readonly amount_paid: number
  readonly amount_credited: number
  readonly status: string
}

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new CreditNoteStoreError({ cause }))
  )

const fromLine = (row: CreditNoteLineRow): CreditNoteLine => ({
  id: row.id,
  credit_note_id: row.credit_note_id,
  description: row.description,
  quantity: row.quantity,
  unit_price: row.unit_price,
  amount: row.amount,
  account_id: row.account_id,
  ...(row.account_code === "" ? {} : { account_code: row.account_code }),
  ...(row.account_name === "" ? {} : { account_name: row.account_name })
})

const fromRow = (
  row: CreditNoteRow,
  lines?: ReadonlyArray<CreditNoteLine>
): CreditNote => ({
  id: row.id,
  cn_number: row.cn_number,
  contact_id: row.contact_id,
  ...(row.invoice_id === null ? {} : { invoice_id: row.invoice_id }),
  cn_date: row.cn_date,
  reason: row.reason,
  status: row.status,
  subtotal: row.subtotal,
  tax_amount: row.tax_amount,
  total: row.total,
  notes: row.notes,
  ...(row.journal_id === null ? {} : { journal_id: row.journal_id }),
  created_by: row.created_by,
  created_at: row.created_at,
  updated_at: row.updated_at,
  ...(row.contact_name === "" ? {} : { contact_name: row.contact_name }),
  ...(row.invoice_number === ""
    ? {}
    : { invoice_number: row.invoice_number }),
  ...(lines === undefined ? {} : { lines })
})

const currentMonth = () => {
  const now = new Date()
  return `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, "0")}`
}

const errorMessage = (cause: unknown) =>
  cause instanceof Error ? cause.message : String(cause)

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient
  const journals = yield* Journals

  const linesFor = (creditNoteId: number) =>
    store(sql<CreditNoteLineRow>`
      SELECT
        cnl.id,
        cnl.credit_note_id,
        cnl.description,
        cnl.quantity,
        cnl.unit_price,
        cnl.amount,
        cnl.account_id,
        a.code AS account_code,
        a.name AS account_name
      FROM credit_note_lines cnl
      JOIN accounts a ON a.id = cnl.account_id
      WHERE cnl.credit_note_id = ${creditNoteId}
      ORDER BY cnl.id
    `).pipe(Effect.map((rows) => rows.map(fromLine)))

  const get: CreditNotes["get"] = (id) =>
    Effect.gen(function*() {
      const rows = yield* store(sql<CreditNoteRow>`
        SELECT
          cn.id,
          cn.cn_number,
          cn.contact_id,
          cn.invoice_id,
          cn.cn_date,
          cn.reason,
          cn.status,
          cn.subtotal,
          cn.tax_amount,
          cn.total,
          COALESCE(cn.notes, '') AS notes,
          cn.journal_id,
          cn.created_by,
          cn.created_at,
          cn.updated_at,
          c.name AS contact_name,
          COALESCE(i.invoice_number, '') AS invoice_number
        FROM credit_notes cn
        JOIN contacts c ON c.id = cn.contact_id
        LEFT JOIN invoices i ON i.id = cn.invoice_id
        WHERE cn.id = ${id}
      `)
      const row = rows[0]
      if (row === undefined) {
        return yield* new CreditNoteNotFound()
      }
      return fromRow(row, yield* linesFor(id))
    })

  const generateNumber = Effect.gen(function*() {
    const prefix = `CN-${currentMonth()}`
    const rows = yield* store(sql<{ readonly maximum: number }>`
      SELECT COALESCE(
        MAX(CAST(SUBSTR(cn_number, ${prefix.length + 2}) AS INTEGER)),
        0
      ) AS maximum
      FROM credit_notes
      WHERE cn_number LIKE ${`${prefix}-%`}
    `)
    return `${prefix}-${String((rows[0]?.maximum ?? 0) + 1).padStart(4, "0")}`
  })

  const calculatedLines = (values: CreditNoteValues) =>
    values.lines.map((line) => ({
      ...line,
      amount: Math.trunc(line.quantity * line.unitPrice / 100)
    }))

  const insertLines = (
    creditNoteId: number,
    lines: ReturnType<typeof calculatedLines>
  ) => Effect.forEach(
    lines,
    (line) => sql`
      INSERT INTO credit_note_lines (
        credit_note_id,
        description,
        quantity,
        unit_price,
        amount,
        account_id
      )
      VALUES (
        ${creditNoteId},
        ${line.description},
        ${line.quantity},
        ${line.unitPrice},
        ${line.amount},
        ${line.accountId}
      )
    `,
    { discard: true }
  )

  const create: CreditNotes["create"] = (values, createdBy) =>
    Effect.gen(function*() {
      const number = yield* generateNumber
      const lines = calculatedLines(values)
      const subtotal = lines.reduce((sum, line) => sum + line.amount, 0)
      const total = subtotal + values.taxAmount
      const id = yield* store(sql.withTransaction(
        Effect.gen(function*() {
          yield* sql`
            INSERT INTO credit_notes (
              cn_number,
              contact_id,
              invoice_id,
              cn_date,
              reason,
              status,
              subtotal,
              tax_amount,
              total,
              notes,
              created_by
            )
            VALUES (
              ${number},
              ${values.contactId},
              ${values.invoiceId ?? null},
              ${values.cnDate},
              ${values.reason},
              'draft',
              ${subtotal},
              ${values.taxAmount},
              ${total},
              ${values.notes},
              ${createdBy}
            )
          `
          const ids = yield* sql<{ readonly id: number }>`
            SELECT last_insert_rowid() AS id
          `
          const creditNoteId = ids[0]?.id ?? 0
          yield* insertLines(creditNoteId, lines)
          return creditNoteId
        })
      ))
      return yield* get(id).pipe(
        Effect.catchTag(
          "CreditNoteNotFound",
          (cause) => new CreditNoteStoreError({ cause })
        )
      )
    })

  const requireDraft = (
    creditNote: CreditNote,
    action: "edit" | "delete" | "issue"
  ) => creditNote.status === "draft"
    ? Effect.succeed(creditNote)
    : Effect.fail(new CreditNoteConflict({
      message:
        `can only ${action} draft credit notes (current: ${creditNote.status})`
    }))

  const update: CreditNotes["update"] = (id, values) =>
    Effect.gen(function*() {
      const before = yield* get(id)
      yield* requireDraft(before, "edit")
      const lines = calculatedLines(values)
      const subtotal = lines.reduce((sum, line) => sum + line.amount, 0)
      const total = subtotal + values.taxAmount
      yield* store(sql.withTransaction(
        Effect.gen(function*() {
          yield* sql`
            UPDATE credit_notes
            SET
              contact_id = ${values.contactId},
              invoice_id = ${values.invoiceId ?? null},
              cn_date = ${values.cnDate},
              reason = ${values.reason},
              subtotal = ${subtotal},
              tax_amount = ${values.taxAmount},
              total = ${total},
              notes = ${values.notes},
              updated_at = datetime('now')
            WHERE id = ${id}
          `
          yield* sql`
            DELETE FROM credit_note_lines
            WHERE credit_note_id = ${id}
          `
          yield* insertLines(id, lines)
        })
      ))
      return { before, after: yield* get(id) }
    })

  const remove: CreditNotes["remove"] = (id) =>
    Effect.gen(function*() {
      const existing = yield* get(id)
      yield* requireDraft(existing, "delete")
      yield* store(sql`DELETE FROM credit_notes WHERE id = ${id}`)
      return existing
    })

  const accountId = (code: string) =>
    store(sql<{ readonly id: number }>`
      SELECT id FROM accounts WHERE code = ${code}
    `).pipe(Effect.map((rows) => rows[0]?.id ?? 0))

  const asConflict = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
    effect.pipe(
      Effect.mapError((cause) =>
        new CreditNoteConflict({ message: errorMessage(cause) })
      )
    )

  const issue: CreditNotes["issue"] = (id, userId) =>
    Effect.gen(function*() {
      const creditNote = yield* get(id)
      yield* requireDraft(creditNote, "issue")
      const arAccount = yield* accountId("1-1100")
      if (arAccount === 0) {
        return yield* new CreditNoteConflict({
          message: "accounts receivable account not found"
        })
      }

      if (creditNote.invoice_id !== undefined) {
        const invoices = yield* store(sql<{
          readonly contact_id: number
          readonly tax_amount: number
        }>`
          SELECT contact_id, tax_amount
          FROM invoices
          WHERE id = ${creditNote.invoice_id}
        `)
        const invoice = invoices[0]
        if (
          invoice !== undefined &&
          invoice.contact_id !== creditNote.contact_id
        ) {
          return yield* new CreditNoteConflict({
            message: "credit note contact does not match invoice contact"
          })
        }
        if (
          invoice !== undefined &&
          creditNote.tax_amount > invoice.tax_amount
        ) {
          return yield* new CreditNoteConflict({
            message:
              `credit note tax (${creditNote.tax_amount}) exceeds original invoice tax (${invoice.tax_amount})`
          })
        }
      }

      const lines = (creditNote.lines ?? []).map((line) => ({
        accountId: line.account_id,
        debit: line.amount,
        credit: 0,
        memo: line.description
      }))
      if (creditNote.tax_amount > 0) {
        const taxAccount = yield* accountId("2-1200")
        if (taxAccount > 0) {
          lines.push({
            accountId: taxAccount,
            debit: creditNote.tax_amount,
            credit: 0,
            memo: "Tax reversal"
          })
        }
      }
      lines.push({
        accountId: arAccount,
        debit: 0,
        credit: creditNote.total,
        memo: creditNote.cn_number
      })

      const description = creditNote.invoice_number === undefined
        ? `Credit Note ${creditNote.cn_number} - ${creditNote.contact_name ?? ""}`
        : `Credit Note ${creditNote.cn_number} for invoice ${creditNote.invoice_number}`
      const journalId = creditNote.journal_id ?? (yield* journals.create({
        entryDate: creditNote.cn_date,
        description,
        sourceType: "credit_note",
        isPosted: true,
        createdBy: userId,
        lines
      }).pipe(asConflict, Effect.map((journal) => journal.id)))

      yield* asConflict(sql.withTransaction(
        Effect.gen(function*() {
          yield* sql`
            UPDATE credit_notes
            SET
              status = 'issued',
              journal_id = ${journalId},
              updated_at = datetime('now')
            WHERE id = ${id}
          `
          if (creditNote.invoice_id === undefined) {
            return
          }
          const invoices = yield* sql<InvoiceCreditRow>`
            SELECT total, amount_paid, amount_credited, status
            FROM invoices
            WHERE id = ${creditNote.invoice_id}
          `
          const invoice = invoices[0]
          if (invoice === undefined) {
            return yield* new CreditNoteConflict({
              message: "read invoice for credit: sql: no rows in result set"
            })
          }
          if (
            invoice.status === "draft" ||
            invoice.status === "cancelled"
          ) {
            return yield* new CreditNoteConflict({
              message:
                `cannot apply credit to a ${invoice.status} invoice`
            })
          }
          const amountCredited =
            invoice.amount_credited + creditNote.total
          const remaining = invoice.total - invoice.amount_paid
          if (amountCredited > remaining) {
            return yield* new CreditNoteConflict({
              message:
                `credit (${creditNote.total}) exceeds remaining balance (${remaining}) on invoice`
            })
          }
          const status =
            invoice.amount_paid + amountCredited >= invoice.total
              ? invoice.amount_paid === 0 ? "cancelled" : "paid"
              : invoice.status
          yield* sql`
            UPDATE invoices
            SET
              amount_credited = ${amountCredited},
              status = ${status},
              updated_at = datetime('now')
            WHERE id = ${creditNote.invoice_id}
          `
        })
      ))
      return yield* get(id)
    })

  const voidCreditNote: CreditNotes["void"] = (id, userId) =>
    Effect.gen(function*() {
      const creditNote = yield* get(id)
      if (creditNote.status !== "issued") {
        return yield* new CreditNoteConflict({
          message:
            `can only void issued credit notes (current: ${creditNote.status})`
        })
      }
      const arAccount = yield* accountId("1-1100")
      if (arAccount === 0) {
        return yield* new CreditNoteConflict({
          message: "accounts receivable account not found"
        })
      }
      const lines = [{
        accountId: arAccount,
        debit: creditNote.total,
        credit: 0,
        memo: `Void ${creditNote.cn_number}`
      }]
      for (const line of creditNote.lines ?? []) {
        lines.push({
          accountId: line.account_id,
          debit: 0,
          credit: line.amount,
          memo: line.description
        })
      }
      if (creditNote.tax_amount > 0) {
        const taxAccount = yield* accountId("2-1200")
        if (taxAccount > 0) {
          lines.push({
            accountId: taxAccount,
            debit: 0,
            credit: creditNote.tax_amount,
            memo: "Tax"
          })
        }
      }
      yield* journals.create({
        entryDate: creditNote.cn_date,
        description: `Void Credit Note ${creditNote.cn_number}`,
        sourceType: "credit_note",
        isPosted: true,
        createdBy: userId,
        lines
      }).pipe(asConflict)

      yield* asConflict(sql.withTransaction(
        Effect.gen(function*() {
          yield* sql`
            UPDATE credit_notes
            SET status = 'void', updated_at = datetime('now')
            WHERE id = ${id}
          `
          if (creditNote.invoice_id === undefined) {
            return
          }
          const invoices = yield* sql<InvoiceCreditRow>`
            SELECT total, amount_paid, amount_credited, status
            FROM invoices
            WHERE id = ${creditNote.invoice_id}
          `
          const invoice = invoices[0]
          if (invoice === undefined) {
            return yield* new CreditNoteConflict({
              message: "read invoice for void: sql: no rows in result set"
            })
          }
          const amountCredited = Math.max(
            invoice.amount_credited - creditNote.total,
            0
          )
          const settled =
            invoice.amount_paid + amountCredited >= invoice.total
          const status = settled
            ? invoice.amount_paid === 0 ? "cancelled" : "paid"
            : invoice.amount_paid > 0 ? "partial" : "sent"
          yield* sql`
            UPDATE invoices
            SET
              amount_credited = ${amountCredited},
              status = ${status},
              updated_at = datetime('now')
            WHERE id = ${creditNote.invoice_id}
          `
        })
      ))
      return yield* get(id)
    })

  const list: CreditNotes["list"] = (filter) => {
    let where = ""
    const params: Array<unknown> = []
    if (filter.status !== "") {
      where += " AND cn.status = ?"
      params.push(filter.status)
    }
    if (filter.search !== "") {
      where +=
        " AND (cn.cn_number LIKE ? OR c.name LIKE ? OR i.invoice_number LIKE ?)"
      const search = `%${filter.search}%`
      params.push(search, search, search)
    }
    const countQuery = `
      SELECT COUNT(*) AS count
      FROM credit_notes cn
      JOIN contacts c ON c.id = cn.contact_id
      LEFT JOIN invoices i ON i.id = cn.invoice_id
      WHERE 1 = 1 ${where}
    `
    let listQuery = `
      SELECT
        cn.id,
        cn.cn_number,
        cn.contact_id,
        cn.invoice_id,
        cn.cn_date,
        cn.reason,
        cn.status,
        cn.subtotal,
        cn.tax_amount,
        cn.total,
        COALESCE(cn.notes, '') AS notes,
        cn.journal_id,
        cn.created_by,
        cn.created_at,
        cn.updated_at,
        c.name AS contact_name,
        COALESCE(i.invoice_number, '') AS invoice_number
      FROM credit_notes cn
      JOIN contacts c ON c.id = cn.contact_id
      LEFT JOIN invoices i ON i.id = cn.invoice_id
      WHERE 1 = 1 ${where}
      ORDER BY cn.cn_date DESC, cn.id DESC
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
      const rows = yield* store(sql.unsafe<CreditNoteRow>(
        listQuery,
        listParams
      ))
      return {
        creditNotes: rows.map((row) => fromRow(row)),
        total: counts[0]?.count ?? 0
      }
    })
  }

  return CreditNotes.of({
    list,
    get,
    create,
    update,
    remove,
    issue,
    void: voidCreditNote
  })
})

export const CreditNotesLive = Layer.effect(CreditNotes, make)

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

const parseQuantity = (value: string) => {
  const trimmed = value.trim()
  if (trimmed === "") {
    return 0
  }
  const separator = trimmed.indexOf(".")
  const wholeText = separator === -1
    ? trimmed
    : trimmed.slice(0, separator)
  if (!/^[+-]?\d+$/.test(wholeText)) {
    return undefined
  }
  const whole = Number(wholeText)
  if (!Number.isSafeInteger(whole) || whole < 0) {
    return undefined
  }
  if (separator === -1) {
    return whole * 100
  }
  let fraction = trimmed.slice(separator + 1)
  if (
    fraction.length === 0 ||
    fraction.length > 2 ||
    !/^\d+$/.test(fraction)
  ) {
    return undefined
  }
  fraction = fraction.padEnd(2, "0")
  const quantity = whole * 100 + Number(fraction)
  return Number.isSafeInteger(quantity) ? quantity : undefined
}

export type CreditNoteInputLine = {
  readonly description: string
  readonly quantity: string
  readonly unitPrice: string
  readonly accountId: number
}

const validReasons = new Set([
  "cancellation",
  "return",
  "discount",
  "other"
])

export const validateCreditNote = (input: {
  readonly contactId: number
  readonly invoiceId?: number
  readonly cnDate: string
  readonly reason: string
  readonly taxAmount: string
  readonly notes: string
  readonly lines: ReadonlyArray<CreditNoteInputLine>
}) => {
  const fields: Record<string, string> = {}
  if (input.contactId === 0) {
    fields.contact_id = "required"
  }
  if (input.cnDate.trim() === "") {
    fields.cn_date = "required"
  }
  if (input.reason.trim() === "") {
    fields.reason = "required"
  } else if (!validReasons.has(input.reason)) {
    fields.reason = "must be one of: cancellation, return, discount, other"
  }
  const taxAmount = parseIdr(input.taxAmount)
  if (taxAmount === undefined) {
    fields.tax_amount = "invalid amount"
  }
  if (input.lines.length === 0) {
    fields.lines = "at least one line required"
  }

  const lines: Array<CreditNoteLineValues> = []
  input.lines.forEach((line, index) => {
    let quantity = parseQuantity(line.quantity)
    if (quantity === undefined) {
      fields[`lines[${index}].quantity`] = "invalid quantity"
      return
    }
    if (quantity === 0) {
      quantity = 100
    }
    const unitPrice = parseIdr(line.unitPrice)
    if (unitPrice === undefined) {
      fields[`lines[${index}].unit_price`] = "invalid amount"
      return
    }
    if (line.description.trim() === "") {
      fields[`lines[${index}].description`] = "required"
      return
    }
    if (unitPrice <= 0) {
      fields[`lines[${index}].unit_price`] = "must be positive"
      return
    }
    if (line.accountId === 0) {
      fields[`lines[${index}].account_id`] = "required"
      return
    }
    lines.push({
      description: line.description,
      quantity,
      unitPrice,
      accountId: line.accountId
    })
  })

  return Object.keys(fields).length > 0
    ? { fields }
    : {
      fields,
      taxAmount: taxAmount as number,
      lines
    }
}
