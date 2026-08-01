import { describe, expect, test } from "bun:test"
import { validateExpense } from "./expenses.ts"

describe("validateExpense", () => {
  test("matches integer-IDR and required-field rules", () => {
    expect(validateExpense({
      entryDate: "",
      description: "",
      amount: "",
      expenseAccount: 0,
      paymentAccount: 0
    }).fields).toEqual({
      entry_date: "required",
      description: "required",
      amount: "required",
      expense_account: "required",
      payment_account: "required"
    })
    expect(validateExpense({
      entryDate: "2026-05-10",
      description: "Fuel",
      amount: "+50000",
      expenseAccount: 1,
      paymentAccount: 2
    }).amount).toBe(50000)
    expect(validateExpense({
      entryDate: "2026-05-10",
      description: "Fuel",
      amount: "-1",
      expenseAccount: 1,
      paymentAccount: 2
    }).fields.amount).toBe("must be a positive integer")
  })
})
