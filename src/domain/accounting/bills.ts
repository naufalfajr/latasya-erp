import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"
import { Journals } from "./journals.ts"

export type BillLineValues = {
  readonly description: string
  readonly quantity: number
  readonly unitPrice: number
  readonly accountId: number
}

export type BillLine = {
  readonly id: number
  readonly bill_id: number
  readonly description: string
  readonly quantity: number
  readonly unit_price: number
  readonly amount: number
  readonly account_id: number
  readonly account_code?: string
  readonly account_name?: string
}

export type Bill = {
  readonly id: number
  readonly bill_number: string
  readonly contact_id: number
  readonly bill_date: string
  readonly due_date: string
  readonly status: string
  readonly subtotal: number
  readonly tax_amount: number
  readonly total: number
  readonly amount_paid: number
  readonly notes: string
  readonly journal_id?: number
  readonly created_by: number
  readonly created_at: string
  readonly updated_at: string
  readonly contact_name?: string
  readonly lines?: ReadonlyArray<BillLine>
}

export type BillValues = {
  readonly contactId: number
  readonly billDate: string
  readonly dueDate: string
  readonly taxAmount: number
  readonly notes: string
  readonly lines: ReadonlyArray<BillLineValues>
}

export type BillFilter = {
  readonly status: string
  readonly search: string
  readonly limit: number
  readonly offset: number
}

export class BillNotFound extends Data.TaggedError("BillNotFound") {}

export class BillConflict extends Data.TaggedError("BillConflict")<{
  readonly message: string
}> {}

export class BillStoreError extends Data.TaggedError("BillStoreError")<{
  readonly cause: unknown
}> {}

export interface Bills {
  readonly list: (
    filter: BillFilter
  ) => Effect.Effect<{
    readonly bills: ReadonlyArray<Bill>
    readonly total: number
  }, BillStoreError>
  readonly get: (
    id: number
  ) => Effect.Effect<Bill, BillNotFound | BillStoreError>
  readonly create: (
    values: BillValues,
    createdBy: number
  ) => Effect.Effect<Bill, BillStoreError>
  readonly update: (
    id: number,
    values: BillValues
  ) => Effect.Effect<
    { readonly before: Bill; readonly after: Bill },
    BillConflict | BillNotFound | BillStoreError
  >
  readonly remove: (
    id: number
  ) => Effect.Effect<
    Bill,
    BillConflict | BillNotFound | BillStoreError
  >
  readonly receive: (
    id: number,
    userId: number
  ) => Effect.Effect<
    Bill,
    BillConflict | BillNotFound | BillStoreError
  >
  readonly recordPayment: (
    id: number,
    amount: number,
    paymentDate: string,
    paymentAccount: number,
    userId: number
  ) => Effect.Effect<
    Bill,
    BillConflict | BillNotFound | BillStoreError
  >
}

export const Bills = Context.GenericTag<Bills>("latasya/Bills")

type BillRow = {
  readonly id: number
  readonly bill_number: string
  readonly contact_id: number
  readonly bill_date: string
  readonly due_date: string
  readonly status: string
  readonly subtotal: number
  readonly tax_amount: number
  readonly total: number
  readonly amount_paid: number
  readonly notes: string
  readonly journal_id: number | null
  readonly created_by: number
  readonly created_at: string
  readonly updated_at: string
  readonly contact_name: string
}

type BillLineRow = {
  readonly id: number
  readonly bill_id: number
  readonly description: string
  readonly quantity: number
  readonly unit_price: number
  readonly amount: number
  readonly account_id: number
  readonly account_code: string
  readonly account_name: string
}

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(Effect.mapError((cause) => new BillStoreError({ cause })))

const fromLine = (row: BillLineRow): BillLine => ({
  id: row.id,
  bill_id: row.bill_id,
  description: row.description,
  quantity: row.quantity,
  unit_price: row.unit_price,
  amount: row.amount,
  account_id: row.account_id,
  ...(row.account_code === "" ? {} : { account_code: row.account_code }),
  ...(row.account_name === "" ? {} : { account_name: row.account_name })
})

const fromRow = (
  row: BillRow,
  lines?: ReadonlyArray<BillLine>
): Bill => ({
  id: row.id,
  bill_number: row.bill_number,
  contact_id: row.contact_id,
  bill_date: row.bill_date,
  due_date: row.due_date,
  status: row.status,
  subtotal: row.subtotal,
  tax_amount: row.tax_amount,
  total: row.total,
  amount_paid: row.amount_paid,
  notes: row.notes,
  ...(row.journal_id === null ? {} : { journal_id: row.journal_id }),
  created_by: row.created_by,
  created_at: row.created_at,
  updated_at: row.updated_at,
  ...(row.contact_name === "" ? {} : { contact_name: row.contact_name }),
  ...(lines === undefined ? {} : { lines })
})

const currentMonth = () => {
  const now = new Date()
  return `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, "0")}`
}

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient
  const journals = yield* Journals

  const linesFor = (billId: number) =>
    store(sql<BillLineRow>`
      SELECT
        bl.id,
        bl.bill_id,
        bl.description,
        bl.quantity,
        bl.unit_price,
        bl.amount,
        bl.account_id,
        a.code AS account_code,
        a.name AS account_name
      FROM bill_lines bl
      JOIN accounts a ON a.id = bl.account_id
      WHERE bl.bill_id = ${billId}
      ORDER BY bl.id
    `).pipe(Effect.map((rows) => rows.map(fromLine)))

  const get: Bills["get"] = (id) =>
    Effect.gen(function*() {
      const rows = yield* store(sql<BillRow>`
        SELECT
          b.id,
          b.bill_number,
          b.contact_id,
          b.bill_date,
          b.due_date,
          b.status,
          b.subtotal,
          b.tax_amount,
          b.total,
          b.amount_paid,
          COALESCE(b.notes, '') AS notes,
          b.journal_id,
          b.created_by,
          b.created_at,
          b.updated_at,
          c.name AS contact_name
        FROM bills b
        JOIN contacts c ON c.id = b.contact_id
        WHERE b.id = ${id}
      `)
      const row = rows[0]
      if (row === undefined) {
        return yield* new BillNotFound()
      }
      return fromRow(row, yield* linesFor(id))
    })

  const generateNumber = Effect.gen(function*() {
    const prefix = `BILL-${currentMonth()}`
    const rows = yield* store(sql<{ readonly maximum: number }>`
      SELECT COALESCE(
        MAX(CAST(SUBSTR(bill_number, ${prefix.length + 2}) AS INTEGER)),
        0
      ) AS maximum
      FROM bills
      WHERE bill_number LIKE ${`${prefix}-%`}
    `)
    return `${prefix}-${String((rows[0]?.maximum ?? 0) + 1).padStart(4, "0")}`
  })

  const calculatedLines = (values: BillValues) =>
    values.lines.map((line) => ({
      ...line,
      amount: Math.trunc(line.quantity * line.unitPrice / 100)
    }))

  const insertLines = (
    billId: number,
    lines: ReturnType<typeof calculatedLines>
  ) => Effect.forEach(
    lines,
    (line) => sql`
      INSERT INTO bill_lines (
        bill_id,
        description,
        quantity,
        unit_price,
        amount,
        account_id
      )
      VALUES (
        ${billId},
        ${line.description},
        ${line.quantity},
        ${line.unitPrice},
        ${line.amount},
        ${line.accountId}
      )
    `,
    { discard: true }
  )

  const create: Bills["create"] = (values, createdBy) =>
    Effect.gen(function*() {
      const number = yield* generateNumber
      const lines = calculatedLines(values)
      const subtotal = lines.reduce((sum, line) => sum + line.amount, 0)
      const total = subtotal + values.taxAmount
      const id = yield* store(sql.withTransaction(
        Effect.gen(function*() {
          yield* sql`
            INSERT INTO bills (
              bill_number,
              contact_id,
              bill_date,
              due_date,
              status,
              subtotal,
              tax_amount,
              total,
              amount_paid,
              notes,
              created_by
            )
            VALUES (
              ${number},
              ${values.contactId},
              ${values.billDate},
              ${values.dueDate},
              'draft',
              ${subtotal},
              ${values.taxAmount},
              ${total},
              0,
              ${values.notes},
              ${createdBy}
            )
          `
          const ids = yield* sql<{ readonly id: number }>`
            SELECT last_insert_rowid() AS id
          `
          const billId = ids[0]?.id ?? 0
          yield* insertLines(billId, lines)
          return billId
        })
      ))
      return yield* get(id).pipe(
        Effect.catchTag(
          "BillNotFound",
          (cause) => new BillStoreError({ cause })
        )
      )
    })

  const requireDraft = (
    bill: Bill,
    action: "edit" | "delete" | "receive"
  ) => bill.status === "draft"
    ? Effect.succeed(bill)
    : Effect.fail(new BillConflict({
      message:
        `can only ${action} draft bills (current: ${bill.status})`
    }))

  const update: Bills["update"] = (id, values) =>
    Effect.gen(function*() {
      const before = yield* get(id)
      yield* requireDraft(before, "edit")
      const lines = calculatedLines(values)
      const subtotal = lines.reduce((sum, line) => sum + line.amount, 0)
      const total = subtotal + values.taxAmount
      yield* store(sql.withTransaction(
        Effect.gen(function*() {
          yield* sql`
            UPDATE bills
            SET
              contact_id = ${values.contactId},
              bill_date = ${values.billDate},
              due_date = ${values.dueDate},
              subtotal = ${subtotal},
              tax_amount = ${values.taxAmount},
              total = ${total},
              notes = ${values.notes},
              updated_at = datetime('now')
            WHERE id = ${id}
          `
          yield* sql`DELETE FROM bill_lines WHERE bill_id = ${id}`
          yield* insertLines(id, lines)
        })
      ))
      return { before, after: yield* get(id) }
    })

  const remove: Bills["remove"] = (id) =>
    Effect.gen(function*() {
      const existing = yield* get(id)
      yield* requireDraft(existing, "delete")
      yield* store(sql`DELETE FROM bills WHERE id = ${id}`)
      return existing
    })

  const accountId = (code: string) =>
    store(sql<{ readonly id: number }>`
      SELECT id FROM accounts WHERE code = ${code}
    `).pipe(Effect.map((rows) => rows[0]?.id ?? 0))

  const receive: Bills["receive"] = (id, userId) =>
    Effect.gen(function*() {
      const bill = yield* get(id)
      yield* requireDraft(bill, "receive")
      const apAccount = yield* accountId("2-1001")
      if (apAccount === 0) {
        return yield* new BillConflict({
          message: "accounts payable account not found"
        })
      }
      const lines = (bill.lines ?? []).map((line) => ({
        accountId: line.account_id,
        debit: line.amount,
        credit: 0,
        memo: line.description
      }))
      if (bill.tax_amount > 0) {
        const taxAccount = yield* accountId("2-1200")
        if (taxAccount > 0) {
          lines.push({
            accountId: taxAccount,
            debit: bill.tax_amount,
            credit: 0,
            memo: "Tax"
          })
        }
      }
      lines.push({
        accountId: apAccount,
        debit: 0,
        credit: bill.total,
        memo: bill.bill_number
      })
      const journal = yield* journals.create({
        entryDate: bill.bill_date,
        description:
          `Bill ${bill.bill_number} - ${bill.contact_name ?? ""}`,
        sourceType: "bill",
        isPosted: true,
        createdBy: userId,
        lines
      }).pipe(
        Effect.mapError((cause) =>
          new BillConflict({
            message: cause instanceof Error
              ? cause.message
              : String(cause)
          })
        )
      )
      yield* store(sql`
        UPDATE bills
        SET
          status = 'received',
          journal_id = ${journal.id},
          updated_at = datetime('now')
        WHERE id = ${id}
      `)
      return yield* get(id)
    })

  const recordPayment: Bills["recordPayment"] = (
    id,
    amount,
    paymentDate,
    paymentAccount,
    userId
  ) =>
    Effect.gen(function*() {
      const bill = yield* get(id)
      if (
        bill.status === "draft" ||
        bill.status === "cancelled" ||
        bill.status === "paid"
      ) {
        return yield* new BillConflict({
          message: `cannot record payment for ${bill.status} bill`
        })
      }
      const remaining = bill.total - bill.amount_paid
      if (amount > remaining) {
        return yield* new BillConflict({
          message:
            `payment amount (${amount}) exceeds remaining balance (${remaining})`
        })
      }
      const apAccount = yield* accountId("2-1001")
      const journal = yield* journals.create({
        entryDate: paymentDate,
        description: `Payment for ${bill.bill_number}`,
        sourceType: "bill",
        isPosted: true,
        createdBy: userId,
        lines: [
          {
            accountId: apAccount,
            debit: amount,
            credit: 0,
            memo: bill.bill_number
          },
          {
            accountId: paymentAccount,
            debit: 0,
            credit: amount,
            memo: "Payment"
          }
        ]
      }).pipe(
        Effect.mapError((cause) =>
          new BillConflict({
            message: cause instanceof Error
              ? cause.message
              : String(cause)
          })
        )
      )
      yield* store(sql`
        INSERT INTO payments (
          payment_date,
          amount,
          payment_type,
          reference_id,
          payment_method,
          account_id,
          journal_id,
          created_by
        )
        VALUES (
          ${paymentDate},
          ${amount},
          'bill',
          ${id},
          'bank_transfer',
          ${paymentAccount},
          ${journal.id},
          ${userId}
        )
      `).pipe(
        Effect.mapError((error) =>
          new BillConflict({
            message: error.cause instanceof Error
              ? error.cause.message
              : String(error.cause)
          })
        )
      )
      const amountPaid = bill.amount_paid + amount
      const status = amountPaid >= bill.total ? "paid" : "partial"
      yield* store(sql`
        UPDATE bills
        SET
          amount_paid = ${amountPaid},
          status = ${status},
          updated_at = datetime('now')
        WHERE id = ${id}
      `)
      return yield* get(id)
    })

  const list: Bills["list"] = (filter) => {
    let where = ""
    const params: Array<unknown> = []
    if (filter.status !== "") {
      where += " AND b.status = ?"
      params.push(filter.status)
    }
    if (filter.search !== "") {
      where += " AND (b.bill_number LIKE ? OR c.name LIKE ?)"
      params.push(`%${filter.search}%`, `%${filter.search}%`)
    }
    const countQuery = `
      SELECT COUNT(*) AS count
      FROM bills b
      JOIN contacts c ON c.id = b.contact_id
      WHERE 1 = 1 ${where}
    `
    let listQuery = `
      SELECT
        b.id,
        b.bill_number,
        b.contact_id,
        b.bill_date,
        b.due_date,
        b.status,
        b.subtotal,
        b.tax_amount,
        b.total,
        b.amount_paid,
        COALESCE(b.notes, '') AS notes,
        b.journal_id,
        b.created_by,
        b.created_at,
        b.updated_at,
        c.name AS contact_name
      FROM bills b
      JOIN contacts c ON c.id = b.contact_id
      WHERE 1 = 1 ${where}
      ORDER BY b.bill_date DESC, b.id DESC
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
      const rows = yield* store(sql.unsafe<BillRow>(
        listQuery,
        listParams
      ))
      return {
        bills: rows.map((row) => fromRow(row)),
        total: counts[0]?.count ?? 0
      }
    })
  }

  return Bills.of({
    list,
    get,
    create,
    update,
    remove,
    receive,
    recordPayment
  })
})

export const BillsLive = Layer.effect(Bills, make)

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

export type BillInputLine = {
  readonly description: string
  readonly quantity: string
  readonly unitPrice: string
  readonly accountId: number
}

export const validateBill = (input: {
  readonly contactId: number
  readonly billDate: string
  readonly dueDate: string
  readonly taxAmount: string
  readonly notes: string
  readonly lines: ReadonlyArray<BillInputLine>
}) => {
  const fields: Record<string, string> = {}
  if (input.contactId === 0) {
    fields.contact_id = "required"
  }
  if (input.billDate.trim() === "") {
    fields.bill_date = "required"
  }
  if (input.dueDate.trim() === "") {
    fields.due_date = "required"
  }
  const taxAmount = parseIdr(input.taxAmount)
  if (taxAmount === undefined) {
    fields.tax_amount = "invalid amount"
  }
  if (input.lines.length === 0) {
    fields.lines = "at least one line required"
  }
  const lines: Array<BillLineValues> = []
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

export const validateBillPayment = (input: {
  readonly amount: string
  readonly paymentDate: string
  readonly paymentAccount: number
}) => {
  const fields: Record<string, string> = {}
  const amount = parseIdr(input.amount)
  if (amount === undefined || amount <= 0) {
    fields.amount = "must be a positive integer-IDR string"
  }
  if (input.paymentDate.trim() === "") {
    fields.payment_date = "required"
  }
  if (input.paymentAccount === 0) {
    fields.payment_account = "required"
  }
  return Object.keys(fields).length > 0
    ? { fields }
    : { fields, amount: amount as number }
}
