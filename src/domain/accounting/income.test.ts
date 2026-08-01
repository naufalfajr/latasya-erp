import { describe, expect, test } from "bun:test"
import { validateIncome } from "./income.ts"

describe("validateIncome", () => {
  test("matches integer-IDR and required-field rules", () => {
    expect(validateIncome({
      entryDate: "",
      description: "",
      amount: "",
      revenueAccount: 0,
      depositAccount: 0
    }).fields).toEqual({
      entry_date: "required",
      description: "required",
      amount: "required",
      revenue_account: "required",
      deposit_account: "required"
    })
    expect(validateIncome({
      entryDate: "2026-05-10",
      description: "Payment",
      amount: "+1000",
      revenueAccount: 1,
      depositAccount: 2
    }).amount).toBe(1000)
    expect(validateIncome({
      entryDate: "2026-05-10",
      description: "Payment",
      amount: " 1000 ",
      revenueAccount: 1,
      depositAccount: 2
    }).fields.amount).toBe("must be a positive integer")
  })
})
