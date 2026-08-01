import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { SqlClient } from "@effect/sql"
import { Effect } from "effect"
import { Invoices } from "../../domain/accounting/invoices.ts"
import { CompanyProfiles } from "../../domain/company/profile.ts"
import { renderInvoicePdf } from "../../domain/documents/invoice-pdf.ts"
import {
  RateLimiter,
  retryAfterSeconds
} from "../../domain/security/rate-limiter.ts"
import { pageTemplate } from "./template-assets.ts"
import {
  dashboardBasePath,
  uiPlainError
} from "./ui-auth.ts"
import { unspoofableClientIp } from "./request-metadata.ts"

const normalizePhone = (phone: string) => {
  const digits = [...phone]
    .filter((character) => character >= "0" && character <= "9")
    .join("")
  return digits.startsWith("62")
    ? digits
    : digits.startsWith("0")
    ? `62${digits.slice(1)}`
    : digits
}
const waLink = (phone: string, message: string) =>
  `https://wa.me/${normalizePhone(phone)}?${new URLSearchParams({
    text: message
  })}`
const companyData = (company: {
  readonly name: string
  readonly tagline: string
  readonly address: string
  readonly phone: string
  readonly email: string
  readonly npwp: string
  readonly bank_name: string
  readonly bank_account_number: string
  readonly bank_account_holder: string
  readonly invoice_footer: string
}) => ({
  Name: company.name,
  Tagline: company.tagline,
  Address: company.address,
  Phone: company.phone,
  Email: company.email,
  NPWP: company.npwp,
  BankName: company.bank_name,
  BankAccountNumber: company.bank_account_number,
  BankAccountHolder: company.bank_account_holder,
  InvoiceFooter: company.invoice_footer
})
const html = (
  page: "home/index" | "portal/index",
  title: string,
  data: unknown,
  status = 200,
  headers: Readonly<Record<string, string>> = {}
) => HttpServerResponse.text(
  pageTemplate(page).render("index.html", {
    Title: title,
    BasePath: dashboardBasePath,
    Data: data
  }),
  {
    status,
    headers: {
      "content-type": "text/html; charset=utf-8",
      ...headers
    }
  }
)
const privateResponse = (response: HttpServerResponse.HttpServerResponse) =>
  HttpServerResponse.setHeader(response, "cache-control", "private, no-store")
const homeRoute = HttpRouter.get(
  "/",
  Effect.gen(function*() {
    const profiles = yield* CompanyProfiles
    const company = yield* profiles.get
    return html("home/index", company.name, {
      Company: companyData(company),
      ContactWA: waLink(
        company.phone,
        "Halo, saya ingin bertanya soal layanan Latasya Trans."
      ),
      PhoneDisplay: `+${normalizePhone(company.phone)}`
    })
  }).pipe(Effect.catchAll(() =>
    Effect.succeed(uiPlainError(500, "Internal Server Error"))
  ))
)

const normalizedCode = (code: string) =>
  code.toLowerCase().replaceAll("-", "").replaceAll(" ", "")
type Family = {
  readonly code: string
  readonly contacts: ReadonlyArray<{
    readonly id: number
    readonly name: string
  }>
}
const familyByCode = (code: string) => Effect.gen(function*() {
  const normalized = normalizedCode(code)
  if (normalized === "") return undefined
  const sql = yield* SqlClient.SqlClient
  const origins = yield* sql<{
    readonly id: number
    readonly name: string
    readonly phone: string
    readonly portal_code: string
  }>`
    SELECT
      id,
      name,
      COALESCE(phone, '') AS phone,
      COALESCE(portal_code, '') AS portal_code
    FROM contacts
    WHERE
      portal_code IS NOT NULL
      AND portal_code <> ''
      AND LOWER(REPLACE(portal_code, '-', '')) = ${normalized}
  `
  const origin = origins[0]
  if (origin === undefined) return undefined
  if (origin.phone === "") {
    return {
      code: origin.portal_code,
      contacts: [{ id: origin.id, name: origin.name }]
    } satisfies Family
  }
  const rows = yield* sql<{
    readonly id: number
    readonly name: string
    readonly phone: string
  }>`
    SELECT id, name, phone
    FROM contacts
    WHERE phone IS NOT NULL AND phone <> ''
    ORDER BY id
  `
  const phone = normalizePhone(origin.phone)
  return {
    code: origin.portal_code,
    contacts: rows
      .filter((contact) => normalizePhone(contact.phone) === phone)
      .map((contact) => ({ id: contact.id, name: contact.name }))
  } satisfies Family
})
const monthNames = [
  "Januari", "Februari", "Maret", "April", "Mei", "Juni",
  "Juli", "Agustus", "September", "Oktober", "November", "Desember"
] as const
const remark = (name: string, date: string) => {
  const [year = "", rawMonth = ""] = date.slice(0, 7).split("-")
  return `${name} ${monthNames[Number(rawMonth) - 1] ?? ""} ${year}`
}
const rateLimited = <R>(
  operation: (
    request: HttpServerRequest.HttpServerRequest
  ) => Effect.Effect<HttpServerResponse.HttpServerResponse, never, R>
) => Effect.gen(function*() {
  const request = yield* HttpServerRequest.HttpServerRequest
  const limiter = yield* RateLimiter
  const key = unspoofableClientIp(request)
  if (!(yield* limiter.take("portal", key))) {
    return HttpServerResponse.setHeader(
      uiPlainError(429, "Too Many Requests"),
      "retry-after",
      String(retryAfterSeconds("portal"))
    )
  }
  const response = yield* operation(request)
  if (response.status >= 200 && response.status < 300) {
    yield* limiter.refund("portal", key)
  }
  return response
})
const portalRoute = (development: boolean) => HttpRouter.get(
  "/p/:code",
  rateLimited((request) =>
    Effect.gen(function*() {
      const code = (yield* HttpRouter.params).code ?? ""
      const family = yield* familyByCode(code)
      if (family === undefined) {
        return html(
          "portal/index",
          "Link Tidak Valid",
          { Invalid: true },
          404,
          { "cache-control": "private, no-store" }
        )
      }
      const sql = yield* SqlClient.SqlClient
      const ids = family.contacts.map((contact) => contact.id)
      const placeholders = ids.map(() => "?").join(",")
      const rows = yield* sql.unsafe<{
        readonly id: number
        readonly invoice_number: string
        readonly contact_id: number
        readonly invoice_date: string
        readonly due_date: string
        readonly status: string
        readonly total: number
        readonly amount_paid: number
        readonly amount_credited: number
        readonly paid_date: string
      }>(
        `SELECT
          id, invoice_number, contact_id, invoice_date, due_date, status,
          total, amount_paid, amount_credited,
          COALESCE((
            SELECT MAX(payment_date)
            FROM payments
            WHERE payment_type = 'invoice'
              AND reference_id = invoices.id
          ), updated_at) AS paid_date
        FROM invoices
        WHERE contact_id IN (${placeholders}) AND status <> 'draft'
        ORDER BY invoice_date DESC, id DESC`,
        ids
      )
      const profiles = yield* CompanyProfiles
      const company = yield* profiles.get
      const names = new Map(
        family.contacts.map((contact) => [contact.id, contact.name])
      )
      const current = (() => {
        const now = new Date()
        return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(
          2,
          "0"
        )}`
      })()
      let totalDue = 0
      let hasCurrentMonth = false
      const invoices = rows.map((invoice) => {
        const amountDue =
          invoice.total - invoice.amount_paid - invoice.amount_credited
        if (amountDue > 0) totalDue += amountDue
        if (invoice.invoice_date.startsWith(current)) hasCurrentMonth = true
        const child = names.get(invoice.contact_id) ?? ""
        const transferRemark = amountDue > 0
          ? remark(child, invoice.invoice_date)
          : ""
        return {
          ID: invoice.id,
          InvoiceNumber: invoice.invoice_number,
          ContactID: invoice.contact_id,
          InvoiceDate: invoice.invoice_date,
          DueDate: invoice.due_date,
          PaidDate: invoice.paid_date,
          Status: invoice.status,
          Total: invoice.total,
          AmountDue: amountDue,
          ChildName: child,
          Remark: transferRemark,
          ConfirmWA:
            amountDue > 0 && company.phone !== ""
              ? waLink(
                company.phone,
                `Halo, saya sudah transfer untuk ${transferRemark}. ` +
                "Mohon dicek, terima kasih 🙏"
              )
              : "",
          PDFPath: `/p/${family.code}/invoice/${invoice.id}/pdf`
        }
      })
      const url = new URL(request.originalUrl, "http://localhost")
      const scheme = development ? "http" : "https"
      return html(
        "portal/index",
        `Invoice ${family.contacts.map((contact) => contact.name).join(" & ")}`,
        {
          Invalid: false,
          FamilyLabel:
            family.contacts.map((contact) => contact.name).join(" & "),
          Invoices: invoices,
          HasCurrentMonth: hasCurrentMonth,
          TotalDue: totalDue,
          ShortURL: `${scheme}://${url.host}/p/${family.code}`,
          Company: companyData(company)
        },
        200,
        { "cache-control": "private, no-store" }
      )
    }).pipe(Effect.catchAll(() =>
      Effect.succeed(privateResponse(
        uiPlainError(500, "Internal Server Error")
      ))
    ))
  )
)
const portalPdfRoute = HttpRouter.get(
  "/p/:code/invoice/:id/pdf",
  rateLimited(() =>
    Effect.gen(function*() {
      const params = yield* HttpRouter.params
      const invoiceId = Number(params.id)
      if (!Number.isSafeInteger(invoiceId)) {
        return privateResponse(uiPlainError(404, "404 page not found"))
      }
      const family = yield* familyByCode(params.code ?? "")
      if (family === undefined) {
        return privateResponse(uiPlainError(404, "404 page not found"))
      }
      const invoices = yield* Invoices
      const invoice = yield* invoices.get(invoiceId).pipe(Effect.option)
      const found = invoice._tag === "Some" ? invoice.value : undefined
      if (
        found === undefined ||
        found.status === "draft" ||
        !family.contacts.some(
          (contact) => contact.id === found.contact_id
        )
      ) {
        return privateResponse(uiPlainError(404, "404 page not found"))
      }
      const profiles = yield* CompanyProfiles
      const body = renderInvoicePdf(found, yield* profiles.get)
      return HttpServerResponse.uint8Array(body, {
        headers: {
          "cache-control": "private, no-store",
          "content-type": "application/pdf",
          "content-disposition":
            `inline; filename="${found.invoice_number}.pdf"`,
          "content-length": String(body.byteLength)
        }
      })
    }).pipe(Effect.catchAll(() =>
      Effect.succeed(privateResponse(
        uiPlainError(500, "Internal Server Error")
      ))
    ))
  )
)

export const addPublicRoutes = (development: boolean) =>
  <E, R>(router: HttpRouter.HttpRouter<E, R>) =>
    router.pipe(homeRoute, portalRoute(development), portalPdfRoute)
