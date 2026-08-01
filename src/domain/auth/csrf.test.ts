import { describe, expect, test } from "bun:test"
import { checkCsrf } from "./csrf.ts"

describe("checkCsrf", () => {
  test("allows safe methods and bearer authentication", () => {
    expect(checkCsrf({
      method: "GET",
      bearer: false,
      expected: ""
    })).toBe("allowed")
    expect(checkCsrf({
      method: "POST",
      bearer: true,
      expected: ""
    })).toBe("allowed")
  })

  test("prefers the header and compares the complete token", () => {
    const token = "a".repeat(64)
    expect(checkCsrf({
      method: "POST",
      bearer: false,
      expected: token,
      headerToken: token,
      formToken: "wrong"
    })).toBe("allowed")
    expect(checkCsrf({
      method: "POST",
      bearer: false,
      expected: token,
      headerToken: "wrong",
      formToken: token
    })).toBe("invalid")
  })

  test("distinguishes a missing session token from an invalid token", () => {
    expect(checkCsrf({
      method: "DELETE",
      bearer: false,
      expected: ""
    })).toBe("missing")
    expect(checkCsrf({
      method: "DELETE",
      bearer: false,
      expected: "expected",
      formToken: "wrong"
    })).toBe("invalid")
  })
})
