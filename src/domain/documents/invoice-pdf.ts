import type { Invoice } from "../accounting/invoices.ts"
import type { CompanyProfile } from "../company/profile.ts"

const pageWidth = 595.28
const pageHeight = 841.89
const marginLeft = 50
const marginRight = pageWidth - 50

const widths: Readonly<Record<string, number>> = {
  " ": 278,
  "!": 278,
  "\"": 355,
  "#": 556,
  "$": 556,
  "%": 889,
  "&": 667,
  "'": 191,
  "(": 333,
  ")": 333,
  "*": 389,
  "+": 584,
  ",": 278,
  "-": 333,
  ".": 278,
  "/": 278,
  "0": 556,
  "1": 556,
  "2": 556,
  "3": 556,
  "4": 556,
  "5": 556,
  "6": 556,
  "7": 556,
  "8": 556,
  "9": 556,
  ":": 278,
  ";": 278,
  "<": 584,
  "=": 584,
  ">": 584,
  "?": 556,
  "@": 1015,
  "A": 667,
  "B": 667,
  "C": 722,
  "D": 722,
  "E": 667,
  "F": 611,
  "G": 778,
  "H": 722,
  "I": 278,
  "J": 500,
  "K": 667,
  "L": 556,
  "M": 833,
  "N": 722,
  "O": 778,
  "P": 667,
  "Q": 778,
  "R": 722,
  "S": 667,
  "T": 611,
  "U": 722,
  "V": 667,
  "W": 944,
  "X": 667,
  "Y": 667,
  "Z": 611,
  "[": 278,
  "\\": 278,
  "]": 278,
  "^": 469,
  "_": 556,
  "`": 333,
  "a": 556,
  "b": 556,
  "c": 500,
  "d": 556,
  "e": 556,
  "f": 278,
  "g": 556,
  "h": 556,
  "i": 222,
  "j": 222,
  "k": 500,
  "l": 222,
  "m": 833,
  "n": 556,
  "o": 556,
  "p": 556,
  "q": 556,
  "r": 333,
  "s": 500,
  "t": 278,
  "u": 556,
  "v": 500,
  "w": 722,
  "x": 500,
  "y": 500,
  "z": 500,
  "{": 334,
  "|": 260,
  "}": 334,
  "~": 584
}

const number = (value: number) => value.toFixed(2)

const ascii = (value: string) =>
  Array.from(value, (character) => {
    if (character === "\t") {
      return " "
    }
    const code = character.codePointAt(0) ?? 0
    return code >= 32 && code < 127 ? character : "?"
  }).join("")

const escapeString = (value: string) =>
  value
    .replaceAll("\\", "\\\\")
    .replaceAll("(", "\\(")
    .replaceAll(")", "\\)")

const stringWidth = (value: string, size: number) =>
  Array.from(value).reduce(
    (sum, character) => sum + (widths[character] ?? 556),
    0
  ) * size / 1000

class PdfDocument {
  private content = ""

  text(
    x: number,
    y: number,
    size: number,
    bold: boolean,
    value: string
  ) {
    const font = bold ? "F2" : "F1"
    this.content +=
      `BT /${font} ${number(size)} Tf ${number(x)} ${number(y)} Td (` +
      `${escapeString(ascii(value))}) Tj ET\n`
  }

  textRight(
    right: number,
    y: number,
    size: number,
    bold: boolean,
    value: string
  ) {
    this.text(
      right - stringWidth(value, size),
      y,
      size,
      bold,
      value
    )
  }

  textCenter(
    center: number,
    y: number,
    size: number,
    bold: boolean,
    value: string
  ) {
    this.text(
      center - stringWidth(value, size) / 2,
      y,
      size,
      bold,
      value
    )
  }

  line(
    x1: number,
    y1: number,
    x2: number,
    y2: number,
    width: number
  ) {
    this.content +=
      `${number(width)} w ${number(x1)} ${number(y1)} m ` +
      `${number(x2)} ${number(y2)} l S\n`
  }

  fillRect(
    x: number,
    y: number,
    width: number,
    height: number,
    gray: number
  ) {
    this.content +=
      `${number(gray)} g ${number(x)} ${number(y)} ` +
      `${number(width)} ${number(height)} re f 0 g\n`
  }

  render() {
    let output = "%PDF-1.4\n"
    const offsets: Array<number> = []
    const object = (value: string) => {
      offsets.push(output.length)
      output += value
    }
    object("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
    object(
      "2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n"
    )
    object(
      "3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 " +
      `${number(pageWidth)} ${number(pageHeight)}] ` +
      "/Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> " +
      "/Contents 4 0 R >>\nendobj\n"
    )
    object(
      `4 0 obj\n<< /Length ${this.content.length} >>\nstream\n`
    )
    output += this.content
    output += "endstream\nendobj\n"
    object(
      "5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica " +
      "/Encoding /WinAnsiEncoding >>\nendobj\n"
    )
    object(
      "6 0 obj\n<< /Type /Font /Subtype /Type1 " +
      "/BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>\nendobj\n"
    )
    const xref = output.length
    output +=
      `xref\n0 ${offsets.length + 1}\n0000000000 65535 f \n`
    for (const offset of offsets) {
      output += `${String(offset).padStart(10, "0")} 00000 n \n`
    }
    output +=
      `trailer\n<< /Size ${offsets.length + 1} /Root 1 0 R >>\n` +
      `startxref\n${xref}\n%%EOF\n`
    return new TextEncoder().encode(output)
  }
}

const splitLines = (value: string) => {
  if (value.trim() === "") {
    return []
  }
  return value
    .replaceAll("\r\n", "\n")
    .split("\n")
    .map((line) => line.replace(/[ \r]+$/u, ""))
}

const joinNonEmpty = (
  separator: string,
  ...parts: ReadonlyArray<string>
) => parts.filter((part) => part.trim() !== "").join(separator)

const truncate = (value: string, maximum: number) => {
  const characters = Array.from(value)
  if (characters.length <= maximum) {
    return value
  }
  return maximum <= 3
    ? characters.slice(0, maximum).join("")
    : `${characters.slice(0, maximum - 3).join("")}...`
}

const wrap = (value: string, maximum: number) => {
  const words = value.trim() === "" ? [] : value.trim().split(/\s+/u)
  const lines: Array<string> = []
  let current = ""
  for (const word of words) {
    if (current === "") {
      current = word
    } else if (current.length + 1 + word.length <= maximum) {
      current += ` ${word}`
    } else {
      lines.push(current)
      current = word
    }
  }
  if (current !== "") {
    lines.push(current)
  }
  return lines
}

const formatIdr = (rawAmount: string) => {
  const amount = Number(rawAmount)
  const negative = amount < 0
  const digits = String(Math.abs(amount))
  const formatted = digits.replace(/\B(?=(\d{3})+(?!\d))/g, ".")
  return `${negative ? "-" : ""}Rp ${formatted}`
}

const formatQuantity = (value: string) => {
  const [whole = "0", fraction = "00"] = value.split(".")
  if (fraction === "00") {
    return whole
  }
  if (fraction.endsWith("0")) {
    return `${whole}.${fraction[0] ?? "0"}`
  }
  return `${whole}.${fraction}`
}

const months = [
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

const formatDate = (value: string) => {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (match === null) {
    return value
  }
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const date = new Date(Date.UTC(year, month - 1, day))
  if (
    date.getUTCFullYear() !== year ||
    date.getUTCMonth() !== month - 1 ||
    date.getUTCDate() !== day
  ) {
    return value
  }
  return `${day} ${months[month - 1] ?? ""} ${year}`
}

export const renderInvoicePdf = (
  invoice: Invoice,
  company: CompanyProfile
) => {
  const document = new PdfDocument()
  const top = pageHeight - 50
  let leftY = top
  if (company.name !== "") {
    document.text(marginLeft, leftY, 18, true, company.name)
    leftY -= 20
  }
  if (company.tagline !== "") {
    document.text(marginLeft, leftY, 10, false, company.tagline)
    leftY -= 13
  }
  for (const line of splitLines(company.address)) {
    document.text(marginLeft, leftY, 9, false, line)
    leftY -= 11
  }
  const contact = joinNonEmpty(" | ", company.phone, company.email)
  if (contact !== "") {
    document.text(marginLeft, leftY, 9, false, contact)
    leftY -= 11
  }
  if (company.npwp !== "") {
    document.text(marginLeft, leftY, 9, false, `NPWP: ${company.npwp}`)
    leftY -= 11
  }

  let rightY = top
  document.textRight(marginRight, rightY, 20, true, "FAKTUR")
  rightY -= 22
  document.textRight(
    marginRight,
    rightY,
    10,
    false,
    `No: ${invoice.invoice_number}`
  )
  rightY -= 13
  document.textRight(
    marginRight,
    rightY,
    10,
    false,
    `Tanggal: ${formatDate(invoice.invoice_date)}`
  )
  rightY -= 13
  document.textRight(
    marginRight,
    rightY,
    10,
    false,
    `Jatuh Tempo: ${formatDate(invoice.due_date)}`
  )
  rightY -= 13

  let y = Math.min(leftY, rightY) - 8
  document.line(marginLeft, y, marginRight, y, 1)
  y -= 22
  document.text(marginLeft, y, 9, false, "Kepada Yth:")
  y -= 14
  document.text(
    marginLeft,
    y,
    12,
    true,
    invoice.contact_name ?? ""
  )
  y -= 26

  const descriptionX = marginLeft + 4
  const quantityX = 360
  const priceX = 470
  const amountX = marginRight - 4
  document.fillRect(marginLeft, y - 4, marginRight - marginLeft, 16, 0.92)
  document.text(descriptionX, y, 9, true, "Deskripsi")
  document.textRight(quantityX, y, 9, true, "Qty")
  document.textRight(priceX, y, 9, true, "Harga Satuan")
  document.textRight(amountX, y, 9, true, "Jumlah")
  y -= 18

  const lines = invoice.lines ?? []
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index]
    if (line === undefined) {
      continue
    }
    if (y < 120) {
      document.text(
        descriptionX,
        y,
        9,
        false,
        `... ${lines.length - index} item lainnya`
      )
      y -= 15
      break
    }
    document.text(
      descriptionX,
      y,
      9,
      false,
      truncate(line.description, 46)
    )
    document.textRight(
      quantityX,
      y,
      9,
      false,
      formatQuantity(line.quantity)
    )
    document.textRight(
      priceX,
      y,
      9,
      false,
      formatIdr(line.unit_price)
    )
    document.textRight(
      amountX,
      y,
      9,
      false,
      formatIdr(line.amount)
    )
    y -= 15
  }

  y -= 2
  document.line(marginLeft, y, marginRight, y, 0.5)
  y -= 16
  const labelX = 440
  const valueX = marginRight - 4
  const totalRow = (label: string, value: string, bold: boolean) => {
    document.textRight(labelX, y, 10, bold, label)
    document.textRight(valueX, y, 10, bold, value)
    y -= 15
  }
  totalRow("Subtotal", formatIdr(invoice.subtotal), false)
  if (Number(invoice.tax_amount) > 0) {
    totalRow("Pajak", formatIdr(invoice.tax_amount), false)
  }
  totalRow("Total", formatIdr(invoice.total), true)
  if (Number(invoice.amount_paid) > 0) {
    totalRow("Dibayar", formatIdr(invoice.amount_paid), false)
  }
  if (Number(invoice.amount_credited) > 0) {
    totalRow("Kredit", formatIdr(invoice.amount_credited), false)
  }
  if (
    Number(invoice.amount_paid) > 0 ||
    Number(invoice.amount_credited) > 0
  ) {
    totalRow("Sisa Tagihan", formatIdr(invoice.amount_due), true)
  }
  y -= 10

  if (invoice.notes !== "") {
    document.text(marginLeft, y, 9, true, "Catatan:")
    y -= 13
    for (const line of wrap(invoice.notes, 95)) {
      document.text(marginLeft, y, 9, false, line)
      y -= 12
    }
    y -= 8
  }
  if (
    company.bank_name !== "" ||
    company.bank_account_number !== ""
  ) {
    document.text(marginLeft, y, 9, true, "Pembayaran:")
    y -= 13
    let payment = joinNonEmpty(
      " ",
      company.bank_name,
      company.bank_account_number
    )
    if (company.bank_account_holder !== "") {
      payment += ` (a.n. ${company.bank_account_holder})`
    }
    document.text(marginLeft, y, 9, false, payment)
  }

  let footerY = 56
  for (const line of splitLines(company.invoice_footer)) {
    document.textCenter(pageWidth / 2, footerY, 9, false, line)
    footerY -= 12
  }
  return document.render()
}
