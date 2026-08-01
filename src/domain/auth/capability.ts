export const allCapabilities = [
  "accounts.manage",
  "users.manage",
  "roles.manage",
  "contacts.manage",
  "journals.manage",
  "income.manage",
  "expenses.manage",
  "invoices.manage",
  "bills.manage",
  "reports.view",
  "audit.view"
] as const

export type Capability = (typeof allCapabilities)[number]

const knownCapabilities = new Set<string>(allCapabilities)

export const isCapability = (value: string): value is Capability =>
  knownCapabilities.has(value)

export const parseCapabilities = (encoded: string): ReadonlyArray<Capability> => {
  const decoded: unknown = JSON.parse(encoded)
  if (!Array.isArray(decoded) || !decoded.every(
    (value) => typeof value === "string" && isCapability(value)
  )) {
    throw new Error("invalid capability list")
  }
  return decoded
}

export const intersectCapabilities = (
  scopes: ReadonlyArray<Capability>,
  roleCapabilities: ReadonlyArray<Capability>,
  isAdmin: boolean
): ReadonlyArray<Capability> => {
  if (isAdmin) {
    return scopes
  }
  const allowed = new Set(roleCapabilities)
  return scopes.filter((scope) => allowed.has(scope))
}
