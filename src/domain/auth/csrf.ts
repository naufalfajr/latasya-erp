import { timingSafeEqual } from "node:crypto"

export type CsrfCheck = "allowed" | "missing" | "invalid"

const safeMethods = new Set(["GET", "HEAD", "OPTIONS"])

export const checkCsrf = (input: {
  readonly method: string
  readonly bearer: boolean
  readonly expected: string
  readonly headerToken?: string
  readonly formToken?: string
}): CsrfCheck => {
  if (safeMethods.has(input.method) || input.bearer) {
    return "allowed"
  }
  if (input.expected === "") {
    return "missing"
  }

  const supplied = input.headerToken || input.formToken || ""
  const actualBytes = Buffer.from(supplied)
  const expectedBytes = Buffer.from(input.expected)
  if (actualBytes.byteLength !== expectedBytes.byteLength) {
    return "invalid"
  }
  return timingSafeEqual(actualBytes, expectedBytes) ? "allowed" : "invalid"
}
