import { SqlClient } from "@effect/sql"
import { Context, Data, Effect, Layer } from "effect"
import { Journals } from "./journals.ts"

export type InvoiceLineValues = {
  readonly description: string
  readonly quantity: number
  readonly unitPrice: number
  readonly accountId: number
}

export type InvoiceLine = {
  readonly id: number
  readonly invoice_id: number
  readonly description: string
  readonly quantity: string
  readonly unit_price: string
  readonly amount: string
  readonly account_id: number
  readonly account_code?: string
  readonly account_name?: string
}

export type Invoice = {
  readonly id: number
  readonly invoice_number: string
  readonly contact_id: number
  readonly invoice_date: string
  readonly due_date: string
  readonly status: string
  readonly notes: string
  readonly journal_id: number | null
  readonly created_by: number
  readonly created_at: string
  readonly updated_at: string
  readonly paid_date?: string
  readonly contact_name?: string
  readonly lines?: ReadonlyArray<InvoiceLine>
  readonly subtotal: string
  readonly tax_amount: string
  readonly total: string
  readonly amount_paid: string
  readonly amount_credited: string
  readonly amount_due: string
}

export type CreditNoteSummary = {
  readonly id: number
  readonly cn_number: string
  readonly contact_id: number
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
}

export type InvoiceDetail = Invoice & {
  readonly credit_notes?: ReadonlyArray<CreditNoteSummary>
}

export type InvoiceValues = {
  readonly contactId: number
  readonly invoiceDate: string
  readonly dueDate: string
  readonly taxAmount: number
  readonly notes: string
  readonly lines: ReadonlyArray<InvoiceLineValues>
}

export type InvoiceFilter = {
  readonly status: string
  readonly search: string
  readonly limit: number
  readonly offset: number
}

export type DeletedInvoice = {
  readonly id: number
  readonly invoice_number: string
}

export type SentInvoice = {
  readonly id: number
  readonly invoice_number: string
  readonly journal_id?: number
}

export type FailedInvoice = {
  readonly id: number
  readonly invoice_number: string
  readonly error: string
}

export type GeneratedInvoice = {
  readonly contact_id: number
  readonly contact_name: string
  readonly invoice_id?: number
  readonly invoice_number?: string
  readonly result: string
  readonly error?: string
}

export type RecurringInvoiceResult = {
  readonly created: number
  readonly skipped: number
  readonly failed: number
  readonly effective_days: number
  readonly multiplier_percent: number
  readonly items: ReadonlyArray<GeneratedInvoice>
}

export class InvoiceNotFound extends Data.TaggedError("InvoiceNotFound") {}

export class InvoiceConflict extends Data.TaggedError("InvoiceConflict")<{
  readonly message: string
}> {}

export class InvoiceOverpayment extends Data.TaggedError(
  "InvoiceOverpayment"
)<{
  readonly message: string
}> {}

export class InvoiceStoreError extends Data.TaggedError("InvoiceStoreError")<{
  readonly cause: unknown
}> {}

export class NoDefaultRevenueAccount extends Data.TaggedError(
  "NoDefaultRevenueAccount"
)<{
  readonly message: string
}> {}

export interface Invoices {
  readonly list: (
    filter: InvoiceFilter
  ) => Effect.Effect<{
    readonly invoices: ReadonlyArray<Invoice>
    readonly total: number
  }, InvoiceStoreError>
  readonly get: (
    id: number
  ) => Effect.Effect<Invoice, InvoiceNotFound | InvoiceStoreError>
  readonly getDetail: (
    id: number
  ) => Effect.Effect<InvoiceDetail, InvoiceNotFound | InvoiceStoreError>
  readonly create: (
    values: InvoiceValues,
    createdBy: number
  ) => Effect.Effect<Invoice, InvoiceStoreError>
  readonly update: (
    id: number,
    values: InvoiceValues
  ) => Effect.Effect<
    { readonly before: Invoice; readonly after: Invoice },
    InvoiceConflict | InvoiceNotFound | InvoiceStoreError
  >
  readonly remove: (
    id: number
  ) => Effect.Effect<
    Invoice,
    InvoiceConflict | InvoiceNotFound | InvoiceStoreError
  >
  readonly send: (
    id: number,
    userId: number
  ) => Effect.Effect<
    Invoice,
    InvoiceConflict | InvoiceNotFound | InvoiceStoreError
  >
  readonly recordPayment: (
    id: number,
    amount: number,
    paymentDate: string,
    paymentAccount: number,
    userId: number
  ) => Effect.Effect<
    Invoice,
    InvoiceConflict | InvoiceNotFound | InvoiceOverpayment | InvoiceStoreError
  >
  readonly bulkDelete: (
    ids: ReadonlyArray<number>
  ) => Effect.Effect<{
    readonly deleted: ReadonlyArray<DeletedInvoice>
    readonly skipped: ReadonlyArray<number>
  }, InvoiceStoreError>
  readonly bulkSend: (
    ids: ReadonlyArray<number>,
    userId: number
  ) => Effect.Effect<{
    readonly sent: ReadonlyArray<SentInvoice>
    readonly skipped: ReadonlyArray<number>
    readonly failed: ReadonlyArray<FailedInvoice>
  }, InvoiceStoreError>
  readonly generateRecurring: (
    invoiceDate: string,
    dueDate: string,
    userId: number
  ) => Effect.Effect<
    RecurringInvoiceResult,
    NoDefaultRevenueAccount | InvoiceStoreError
  >
}

export const Invoices = Context.GenericTag<Invoices>("latasya/Invoices")

type InvoiceRow = {
  readonly id: number
  readonly invoice_number: string
  readonly contact_id: number
  readonly invoice_date: string
  readonly due_date: string
  readonly status: string
  readonly subtotal: number
  readonly tax_amount: number
  readonly total: number
  readonly amount_paid: number
  readonly amount_credited: number
  readonly notes: string
  readonly journal_id: number | null
  readonly created_by: number
  readonly created_at: string
  readonly updated_at: string
  readonly contact_name: string
  readonly paid_date?: string
}

type InvoiceLineRow = {
  readonly id: number
  readonly invoice_id: number
  readonly description: string
  readonly quantity: number
  readonly unit_price: number
  readonly amount: number
  readonly account_id: number
  readonly account_code: string
  readonly account_name: string
}

type CreditNoteRow = {
  readonly id: number
  readonly cn_number: string
  readonly cn_date: string
  readonly reason: string
  readonly status: string
  readonly total: number
  readonly journal_id: number | null
}

const store = <A, E, R>(effect: Effect.Effect<A, E, R>) =>
  effect.pipe(
    Effect.mapError((cause) => new InvoiceStoreError({ cause }))
  )

const quantityString = (quantity: number) =>
  `${Math.trunc(quantity / 100)}.${String(quantity % 100).padStart(2, "0")}`

const fromLine = (row: InvoiceLineRow): InvoiceLine => ({
  id: row.id,
  invoice_id: row.invoice_id,
  description: row.description,
  quantity: quantityString(row.quantity),
  unit_price: String(row.unit_price),
  amount: String(row.amount),
  account_id: row.account_id,
  ...(row.account_code === "" ? {} : { account_code: row.account_code }),
  ...(row.account_name === "" ? {} : { account_name: row.account_name })
})

const fromRow = (
  row: InvoiceRow,
  lines?: ReadonlyArray<InvoiceLine>
): Invoice => ({
  id: row.id,
  invoice_number: row.invoice_number,
  contact_id: row.contact_id,
  invoice_date: row.invoice_date,
  due_date: row.due_date,
  status: row.status,
  notes: row.notes,
  journal_id: row.journal_id,
  created_by: row.created_by,
  created_at: row.created_at,
  updated_at: row.updated_at,
  ...(row.paid_date === undefined || row.paid_date === ""
    ? {}
    : { paid_date: row.paid_date }),
  ...(row.contact_name === "" ? {} : { contact_name: row.contact_name }),
  ...(lines === undefined ? {} : { lines }),
  subtotal: String(row.subtotal),
  tax_amount: String(row.tax_amount),
  total: String(row.total),
  amount_paid: String(row.amount_paid),
  amount_credited: String(row.amount_credited),
  amount_due: String(row.total - row.amount_paid - row.amount_credited)
})

const currentMonth = () => {
  const now = new Date()
  return `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, "0")}`
}

const recurringMutex = (() => {
  let tail = Promise.resolve()
  return <A, E, R>(
    effect: Effect.Effect<A, E, R>
  ): Effect.Effect<A, E, R> =>
    Effect.acquireUseRelease(
      Effect.promise(async () => {
        const previous = tail
        let release = () => {}
        tail = previous.then(() =>
          new Promise<void>((resolve) => {
            release = resolve
          })
        )
        await previous
        return release
      }),
      () => effect,
      (release) => Effect.sync(release)
    )
})()

const monthNames = [
  "Januari",
  "Februari",
  "Maret",
  "April",
  "Mei",
  "Juni",
  "Juli",
  "Agustus",
  "September",
  "Oktober",
  "November",
  "Desember"
] as const

const contactPrice = (
  distance: number,
  siblingDiscount: boolean,
  returnOnly: boolean
) => {
  let price = distance < 4
    ? 350000
    : distance < 7
    ? 400000
    : distance < 10
    ? 450000
    : distance < 13
    ? 500000
    : 550000
  if (siblingDiscount) {
    price -= 50000
  }
  if (returnOnly) {
    price -= 50000
  }
  return price
}

const make = Effect.gen(function*() {
  const sql = yield* SqlClient.SqlClient
  const journals = yield* Journals

  const linesFor = (invoiceId: number) =>
    store(sql<InvoiceLineRow>`
      SELECT
        il.id,
        il.invoice_id,
        il.description,
        il.quantity,
        il.unit_price,
        il.amount,
        il.account_id,
        a.code AS account_code,
        a.name AS account_name
      FROM invoice_lines il
      JOIN accounts a ON a.id = il.account_id
      WHERE il.invoice_id = ${invoiceId}
      ORDER BY il.id
    `).pipe(Effect.map((rows) => rows.map(fromLine)))

  const get: Invoices["get"] = (id) =>
    Effect.gen(function*() {
      const rows = yield* store(sql<InvoiceRow>`
        SELECT
          i.id,
          i.invoice_number,
          i.contact_id,
          i.invoice_date,
          i.due_date,
          i.status,
          i.subtotal,
          i.tax_amount,
          i.total,
          i.amount_paid,
          i.amount_credited,
          COALESCE(i.notes, '') AS notes,
          i.journal_id,
          i.created_by,
          i.created_at,
          i.updated_at,
          c.name AS contact_name
        FROM invoices i
        JOIN contacts c ON c.id = i.contact_id
        WHERE i.id = ${id}
      `)
      const row = rows[0]
      if (row === undefined) {
        return yield* new InvoiceNotFound()
      }
      return fromRow(row, yield* linesFor(id))
    })

  const creditNotesFor = (invoiceId: number) =>
    store(sql<CreditNoteRow>`
      SELECT
        id,
        cn_number,
        cn_date,
        reason,
        status,
        total,
        journal_id
      FROM credit_notes
      WHERE invoice_id = ${invoiceId}
      ORDER BY cn_date DESC, id DESC
    `).pipe(
      Effect.map((rows): ReadonlyArray<CreditNoteSummary> =>
        rows.map((row) => ({
          id: row.id,
          cn_number: row.cn_number,
          contact_id: 0,
          cn_date: row.cn_date,
          reason: row.reason,
          status: row.status,
          subtotal: 0,
          tax_amount: 0,
          total: row.total,
          notes: "",
          ...(row.journal_id === null
            ? {}
            : { journal_id: row.journal_id }),
          created_by: 0,
          created_at: "",
          updated_at: ""
        }))
      )
    )

  const getDetail: Invoices["getDetail"] = (id) =>
    Effect.gen(function*() {
      const invoice = yield* get(id)
      const creditNotes = yield* creditNotesFor(id).pipe(
        Effect.catchAll(() => Effect.succeed([]))
      )
      return creditNotes.length === 0
        ? invoice
        : { ...invoice, credit_notes: creditNotes }
    })

  const generateNumber = Effect.gen(function*() {
    const prefix = `INV-${currentMonth()}`
    const rows = yield* store(sql<{ readonly maximum: number }>`
      SELECT COALESCE(
        MAX(CAST(SUBSTR(invoice_number, ${prefix.length + 2}) AS INTEGER)),
        0
      ) AS maximum
      FROM invoices
      WHERE invoice_number LIKE ${`${prefix}-%`}
    `)
    return `${prefix}-${String((rows[0]?.maximum ?? 0) + 1).padStart(4, "0")}`
  })

  const calculatedLines = (values: InvoiceValues) =>
    values.lines.map((line) => ({
      ...line,
      amount: Math.trunc(line.quantity * line.unitPrice / 100)
    }))

  const insertLines = (
    invoiceId: number,
    lines: ReturnType<typeof calculatedLines>
  ) => Effect.forEach(
    lines,
    (line) => sql`
      INSERT INTO invoice_lines (
        invoice_id,
        description,
        quantity,
        unit_price,
        amount,
        account_id
      )
      VALUES (
        ${invoiceId},
        ${line.description},
        ${line.quantity},
        ${line.unitPrice},
        ${line.amount},
        ${line.accountId}
      )
    `,
    { discard: true }
  )

  const create: Invoices["create"] = (values, createdBy) =>
    Effect.gen(function*() {
      const number = yield* generateNumber
      const lines = calculatedLines(values)
      const subtotal = lines.reduce((sum, line) => sum + line.amount, 0)
      const total = subtotal + values.taxAmount
      const id = yield* store(sql.withTransaction(
        Effect.gen(function*() {
          yield* sql`
            INSERT INTO invoices (
              invoice_number,
              contact_id,
              invoice_date,
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
              ${values.invoiceDate},
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
          const invoiceId = ids[0]?.id ?? 0
          yield* insertLines(invoiceId, lines)
          return invoiceId
        })
      ))
      return yield* get(id).pipe(
        Effect.catchTag(
          "InvoiceNotFound",
          (cause) => new InvoiceStoreError({ cause })
        )
      )
    })

  const requireDraft = (
    invoice: Invoice,
    action: "edit" | "delete" | "send"
  ) => invoice.status === "draft"
    ? Effect.succeed(invoice)
    : Effect.fail(new InvoiceConflict({
      message:
        `can only ${action} draft invoices (current: ${invoice.status})`
    }))

  const update: Invoices["update"] = (id, values) =>
    Effect.gen(function*() {
      const before = yield* get(id)
      yield* requireDraft(before, "edit")
      const lines = calculatedLines(values)
      const subtotal = lines.reduce((sum, line) => sum + line.amount, 0)
      const total = subtotal + values.taxAmount
      yield* store(sql.withTransaction(
        Effect.gen(function*() {
          yield* sql`
            UPDATE invoices
            SET
              contact_id = ${values.contactId},
              invoice_date = ${values.invoiceDate},
              due_date = ${values.dueDate},
              subtotal = ${subtotal},
              tax_amount = ${values.taxAmount},
              total = ${total},
              notes = ${values.notes},
              updated_at = datetime('now')
            WHERE id = ${id}
          `
          yield* sql`DELETE FROM invoice_lines WHERE invoice_id = ${id}`
          yield* insertLines(id, lines)
        })
      ))
      return { before, after: yield* get(id) }
    })

  const remove: Invoices["remove"] = (id) =>
    Effect.gen(function*() {
      const existing = yield* get(id)
      yield* requireDraft(existing, "delete")
      yield* store(sql`DELETE FROM invoices WHERE id = ${id}`)
      return existing
    })

  const accountId = (code: string) =>
    store(sql<{ readonly id: number }>`
      SELECT id FROM accounts WHERE code = ${code}
    `).pipe(Effect.map((rows) => rows[0]?.id ?? 0))

  const send: Invoices["send"] = (id, userId) =>
    Effect.gen(function*() {
      const invoice = yield* get(id)
      yield* requireDraft(invoice, "send")
      const arAccount = yield* accountId("1-1100")
      if (arAccount === 0) {
        return yield* new InvoiceStoreError({
          cause: new Error("accounts receivable account not found")
        })
      }
      const lines = [
        {
          accountId: arAccount,
          debit: Number(invoice.total),
          credit: 0,
          memo: invoice.invoice_number
        },
        ...(invoice.lines ?? []).map((line) => ({
          accountId: line.account_id,
          debit: 0,
          credit: Number(line.amount),
          memo: line.description
        }))
      ]
      if (Number(invoice.tax_amount) > 0) {
        const taxAccount = yield* accountId("2-1200")
        if (taxAccount > 0) {
          lines.push({
            accountId: taxAccount,
            debit: 0,
            credit: Number(invoice.tax_amount),
            memo: "Tax"
          })
        }
      }
      const journal = yield* journals.create({
        entryDate: invoice.invoice_date,
        description:
          `Invoice ${invoice.invoice_number} - ${invoice.contact_name ?? ""}`,
        sourceType: "invoice",
        isPosted: true,
        createdBy: userId,
        lines
      }).pipe(
        Effect.mapError((cause) => new InvoiceStoreError({ cause }))
      )
      yield* store(sql`
        UPDATE invoices
        SET
          status = 'sent',
          journal_id = ${journal.id},
          updated_at = datetime('now')
        WHERE id = ${id}
      `)
      return yield* get(id)
    })

  const recordPayment: Invoices["recordPayment"] = (
    id,
    amount,
    paymentDate,
    paymentAccount,
    userId
  ) =>
    Effect.gen(function*() {
      const invoice = yield* get(id)
      if (
        invoice.status === "draft" ||
        invoice.status === "cancelled" ||
        invoice.status === "paid"
      ) {
        return yield* new InvoiceConflict({
          message:
            `cannot record payment for ${invoice.status} invoice`
        })
      }
      const remaining = Number(invoice.amount_due)
      if (amount > remaining) {
        return yield* new InvoiceOverpayment({
          message:
            `payment amount (${amount}) exceeds remaining balance (${remaining})`
        })
      }
      const arAccount = yield* accountId("1-1100")
      const journal = yield* journals.create({
        entryDate: paymentDate,
        description: `Payment for ${invoice.invoice_number}`,
        sourceType: "invoice",
        isPosted: true,
        createdBy: userId,
        lines: [
          {
            accountId: paymentAccount,
            debit: amount,
            credit: 0,
            memo: "Payment received"
          },
          {
            accountId: arAccount,
            debit: 0,
            credit: amount,
            memo: invoice.invoice_number
          }
        ]
      }).pipe(
        Effect.mapError((cause) => new InvoiceStoreError({ cause }))
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
          'invoice',
          ${id},
          'bank_transfer',
          ${paymentAccount},
          ${journal.id},
          ${userId}
        )
      `)
      const amountPaid = Number(invoice.amount_paid) + amount
      const status =
        amountPaid + Number(invoice.amount_credited) >= Number(invoice.total)
          ? "paid"
          : "partial"
      yield* store(sql`
        UPDATE invoices
        SET
          amount_paid = ${amountPaid},
          status = ${status},
          updated_at = datetime('now')
        WHERE id = ${id}
      `)
      return yield* get(id)
    })

  const bulkDelete: Invoices["bulkDelete"] = (ids) =>
    store(sql.withTransaction(
      Effect.gen(function*() {
        const deleted: Array<DeletedInvoice> = []
        const skipped: Array<number> = []
        for (const id of ids) {
          const rows = yield* sql<{
            readonly status: string
            readonly invoice_number: string
          }>`
            SELECT status, invoice_number
            FROM invoices
            WHERE id = ${id}
          `
          const row = rows[0]
          if (row === undefined || row.status !== "draft") {
            skipped.push(id)
            continue
          }
          yield* sql`DELETE FROM invoices WHERE id = ${id}`
          deleted.push({
            id,
            invoice_number: row.invoice_number
          })
        }
        return { deleted, skipped }
      })
    ))

  const bulkSend: Invoices["bulkSend"] = (ids, userId) =>
    Effect.gen(function*() {
      const sent: Array<SentInvoice> = []
      const skipped: Array<number> = []
      const failed: Array<FailedInvoice> = []
      for (const id of ids) {
        const rows = yield* store(sql<{
          readonly status: string
          readonly invoice_number: string
        }>`
          SELECT status, invoice_number
          FROM invoices
          WHERE id = ${id}
        `)
        const row = rows[0]
        if (row === undefined || row.status !== "draft") {
          skipped.push(id)
          continue
        }
        const result = yield* Effect.either(send(id, userId))
        if (result._tag === "Left") {
          failed.push({
            id,
            invoice_number: row.invoice_number,
            error: result.left instanceof Error
              ? result.left.message
              : String(result.left)
          })
          continue
        }
        sent.push({
          id,
          invoice_number: row.invoice_number,
          ...(result.right.journal_id === null
            ? {}
            : { journal_id: result.right.journal_id })
        })
      }
      return { sent, skipped, failed }
    })

  const generateRecurring: Invoices["generateRecurring"] = (
    invoiceDate,
    dueDate,
    userId
  ) => {
    const operation = Effect.gen(function*() {
      const monthPrefix = invoiceDate.slice(0, 7)
      const match = /^(\d{4})-(\d{2})/.exec(monthPrefix)
      if (invoiceDate.length < 7 || match === null) {
        return yield* new InvoiceStoreError({
          cause: new Error(`invalid invoice date: ${JSON.stringify(invoiceDate)}`)
        })
      }
      const year = Number(match[1])
      const month = Number(match[2])
      if (month < 1 || month > 12) {
        return yield* new InvoiceStoreError({
          cause: new Error(`invalid invoice date ${JSON.stringify(invoiceDate)}`)
        })
      }
      const profiles = yield* store(sql<{
        readonly default_revenue_account_id: number
        readonly recurring_description_template: string
      }>`
        SELECT
          COALESCE(default_revenue_account_id, 0)
            AS default_revenue_account_id,
          recurring_description_template
        FROM company_profile
        WHERE id = 1
      `)
      const profile = profiles[0]
      if (profile === undefined) {
        return yield* new InvoiceStoreError({
          cause: new Error("load company profile")
        })
      }
      if (profile.default_revenue_account_id === 0) {
        return yield* new NoDefaultRevenueAccount({
          message:
            "set a default revenue account in Company Profile before generating recurring invoices"
        })
      }

      const lastDay = new Date(Date.UTC(year, month, 0)).getUTCDate()
      const schoolDays = new Set<string>()
      for (let day = 1; day <= lastDay; day += 1) {
        const date = new Date(Date.UTC(year, month - 1, day))
        if (date.getUTCDay() !== 0) {
          schoolDays.add(
            `${monthPrefix}-${String(day).padStart(2, "0")}`
          )
        }
      }
      const closures = yield* store(sql<{
        readonly start_date: string
        readonly end_date: string
      }>`
        SELECT start_date, end_date
        FROM school_closures
        WHERE start_date <= ${`${monthPrefix}-${lastDay}`}
          AND end_date >= ${`${monthPrefix}-01`}
      `)
      for (const closure of closures) {
        for (const date of [...schoolDays]) {
          if (
            date >= closure.start_date &&
            date <= closure.end_date
          ) {
            schoolDays.delete(date)
          }
        }
      }
      const effectiveDays = schoolDays.size
      const multiplier = effectiveDays < 14
        ? 75
        : effectiveDays < 20
        ? 85
        : 100
      const template = profile.recurring_description_template === ""
        ? "Antar jemput {month} {year}"
        : profile.recurring_description_template
      const description = template
        .replaceAll("{month}", monthNames[month - 1] ?? "")
        .replaceAll("{year}", String(year))
      const customers = yield* store(sql<{
        readonly id: number
        readonly name: string
        readonly distance_km: number
        readonly has_sibling_discount: number
        readonly is_return_only: number
      }>`
        SELECT
          id,
          name,
          distance_km,
          has_sibling_discount,
          is_return_only
        FROM contacts
        WHERE (contact_type = 'customer' OR contact_type = 'both')
          AND is_active = 1
        ORDER BY name
      `)
      let created = 0
      let skipped = 0
      let failed = 0
      const items: Array<GeneratedInvoice> = []
      for (const customer of customers) {
        const counts = yield* store(sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM invoices
          WHERE contact_id = ${customer.id}
            AND substr(invoice_date, 1, 7) = ${monthPrefix}
        `)
        if ((counts[0]?.count ?? 0) > 0) {
          skipped += 1
          items.push({
            contact_id: customer.id,
            contact_name: customer.name,
            result: "skipped_already_invoiced"
          })
          continue
        }
        const basePrice = contactPrice(
          customer.distance_km,
          customer.has_sibling_discount !== 0,
          customer.is_return_only !== 0
        )
        const unitPrice = Math.trunc(basePrice * multiplier / 100)
        const outcome = yield* Effect.either(create({
          contactId: customer.id,
          invoiceDate,
          dueDate,
          taxAmount: 0,
          notes: "",
          lines: [{
            description,
            quantity: 100,
            unitPrice,
            accountId: profile.default_revenue_account_id
          }]
        }, userId))
        if (outcome._tag === "Left") {
          failed += 1
          items.push({
            contact_id: customer.id,
            contact_name: customer.name,
            result: "failed",
            error: outcome.left.cause instanceof Error
              ? outcome.left.cause.message
              : String(outcome.left.cause)
          })
          continue
        }
        created += 1
        items.push({
          contact_id: customer.id,
          contact_name: customer.name,
          invoice_id: outcome.right.id,
          invoice_number: outcome.right.invoice_number,
          result: "created"
        })
      }
      return {
        created,
        skipped,
        failed,
        effective_days: effectiveDays,
        multiplier_percent: multiplier,
        items
      }
    })
    return recurringMutex(operation)
  }

  return Invoices.of({
    list: (filter) => {
      let where = ""
      const params: Array<unknown> = []
      if (filter.status !== "") {
        where += " AND i.status = ?"
        params.push(filter.status)
      }
      if (filter.search !== "") {
        where +=
          " AND (i.invoice_number LIKE ? OR c.name LIKE ?)"
        params.push(`%${filter.search}%`, `%${filter.search}%`)
      }
      const countQuery = `
        SELECT COUNT(*) AS count
        FROM invoices i
        JOIN contacts c ON c.id = i.contact_id
        WHERE 1 = 1 ${where}
      `
      let listQuery = `
        SELECT
          i.id,
          i.invoice_number,
          i.contact_id,
          i.invoice_date,
          i.due_date,
          i.status,
          i.subtotal,
          i.tax_amount,
          i.total,
          i.amount_paid,
          i.amount_credited,
          COALESCE(i.notes, '') AS notes,
          i.journal_id,
          i.created_by,
          i.created_at,
          i.updated_at,
          c.name AS contact_name,
          COALESCE((
            SELECT MAX(payment_date)
            FROM payments
            WHERE payment_type = 'invoice'
              AND reference_id = i.id
          ), i.updated_at) AS paid_date
        FROM invoices i
        JOIN contacts c ON c.id = i.contact_id
        WHERE 1 = 1 ${where}
        ORDER BY i.invoice_date DESC, i.id DESC
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
        const rows = yield* store(sql.unsafe<InvoiceRow>(
          listQuery,
          listParams
        ))
        return {
          invoices: rows.map((row) => fromRow(row)),
          total: counts[0]?.count ?? 0
        }
      })
    },
    get,
    getDetail,
    create,
    update,
    remove,
    send,
    recordPayment,
    bulkDelete,
    bulkSend,
    generateRecurring
  })
})

export const InvoicesLive = Layer.effect(Invoices, make)

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
  let fractionText = separator === -1
    ? undefined
    : trimmed.slice(separator + 1)
  if (fractionText === undefined) {
    fractionText = "0"
  }
  fractionText = fractionText.slice(0, 2).padEnd(2, "0")
  if (!/^\d+$/.test(fractionText)) {
    return undefined
  }
  const fraction = Number(fractionText)
  const quantity = whole * 100 + fraction
  return Number.isSafeInteger(quantity) ? quantity : undefined
}

export type InvoiceInputLine = {
  readonly description: string
  readonly quantity: string
  readonly unitPrice: string
  readonly accountId: number
}

export const validateInvoice = (input: {
  readonly contactId: number
  readonly invoiceDate: string
  readonly dueDate: string
  readonly taxAmount: string
  readonly notes: string
  readonly lines: ReadonlyArray<InvoiceInputLine>
}) => {
  const fields: Record<string, string> = {}
  if (input.contactId <= 0) {
    fields.contact_id = "required"
  }
  if (input.invoiceDate.trim() === "") {
    fields.invoice_date = "required"
  }
  if (input.dueDate.trim() === "") {
    fields.due_date = "required"
  }
  if (input.lines.length === 0) {
    fields.lines = "at least one line required"
  }
  const taxAmount = parseIdr(input.taxAmount)
  if (taxAmount === undefined) {
    fields.tax_amount = "invalid amount"
  }
  const lines: Array<InvoiceLineValues> = []
  input.lines.forEach((line, index) => {
    if (line.description.trim() === "") {
      fields[`lines[${index}].description`] = "required"
    }
    const quantity = parseQuantity(line.quantity)
    if (quantity === undefined || quantity <= 0) {
      fields[`lines[${index}].quantity`] = "must be positive"
      return
    }
    const unitPrice = parseIdr(line.unitPrice)
    if (unitPrice === undefined || unitPrice <= 0) {
      fields[`lines[${index}].unit_price`] = "must be positive"
      return
    }
    if (line.accountId <= 0) {
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

export const validateInvoicePayment = (input: {
  readonly amount: string
  readonly paymentDate: string
  readonly paymentAccount: number
}) => {
  const fields: Record<string, string> = {}
  const amount = parseIdr(input.amount)
  if (amount === undefined || amount <= 0) {
    fields.amount = "must be positive"
  }
  if (input.paymentDate.trim() === "") {
    fields.payment_date = "required"
  }
  if (input.paymentAccount <= 0) {
    fields.payment_account = "required"
  }
  return Object.keys(fields).length > 0
    ? { fields }
    : { fields, amount: amount as number }
}
