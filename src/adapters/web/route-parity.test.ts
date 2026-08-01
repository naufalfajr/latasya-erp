import { afterAll, beforeAll, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { makeRouter } from "./router.ts"

type Route = readonly [method: string, path: string]

const apiRoutes: ReadonlyArray<Route> = [
  ["POST", "/api/v1/auth/login"],
  ["POST", "/api/v1/auth/logout"],
  ["GET", "/api/v1/auth/me"],
  ["GET", "/api/v1/auth/csrf"],
  ["POST", "/api/v1/auth/password/change"],
  ["GET", "/api/v1/openapi.yaml"],
  ...["accounts", "contacts"].flatMap((resource): Array<Route> => [
    ["GET", `/api/v1/${resource}`],
    ["GET", `/api/v1/${resource}/1`],
    ["POST", `/api/v1/${resource}`],
    ["PUT", `/api/v1/${resource}/1`],
    ["DELETE", `/api/v1/${resource}/1`]
  ]),
  ...["income", "expenses", "journals"].flatMap(
    (resource): Array<Route> => [
      ["GET", `/api/v1/${resource}`],
      ["GET", `/api/v1/${resource}/1`],
      ["POST", `/api/v1/${resource}`],
      ["PUT", `/api/v1/${resource}/1`],
      ["DELETE", `/api/v1/${resource}/1`]
    ]
  ),
  ["GET", "/api/v1/invoices"],
  ["GET", "/api/v1/invoices/1"],
  ["GET", "/api/v1/invoices/1/pdf"],
  ["POST", "/api/v1/invoices"],
  ["PUT", "/api/v1/invoices/1"],
  ["DELETE", "/api/v1/invoices/1"],
  ["POST", "/api/v1/invoices/1/send"],
  ["POST", "/api/v1/invoices/1/payment"],
  ["POST", "/api/v1/invoices/generate-recurring"],
  ["POST", "/api/v1/invoices/bulk-delete"],
  ["POST", "/api/v1/invoices/bulk-send"],
  ["GET", "/api/v1/api-tokens"],
  ["POST", "/api/v1/api-tokens"],
  ["DELETE", "/api/v1/api-tokens/1"],
  ...["bills", "credit-notes"].flatMap(
    (resource): Array<Route> => [
      ["GET", `/api/v1/${resource}`],
      ["GET", `/api/v1/${resource}/1`],
      ["POST", `/api/v1/${resource}`],
      ["PUT", `/api/v1/${resource}/1`],
      ["DELETE", `/api/v1/${resource}/1`]
    ]
  ),
  ["POST", "/api/v1/bills/1/receive"],
  ["POST", "/api/v1/bills/1/payment"],
  ["POST", "/api/v1/credit-notes/1/issue"],
  ["POST", "/api/v1/credit-notes/1/void"],
  ...[
    "trial-balance",
    "profit-loss",
    "balance-sheet",
    "cash-flow",
    "general-ledger"
  ].map((report): Route => ["GET", `/api/v1/reports/${report}`]),
  ["GET", "/api/v1/users"],
  ["GET", "/api/v1/users/1"],
  ["POST", "/api/v1/users"],
  ["PUT", "/api/v1/users/1"],
  ["DELETE", "/api/v1/users/1"],
  ["GET", "/api/v1/roles"],
  ["GET", "/api/v1/roles/capabilities"],
  ["GET", "/api/v1/roles/custom"],
  ["POST", "/api/v1/roles"],
  ["PUT", "/api/v1/roles/custom"],
  ["DELETE", "/api/v1/roles/custom"],
  ["GET", "/api/v1/audit"],
  ["GET", "/api/v1/dashboard"],
  ["GET", "/api/v1/school-calendar/closures"],
  ["POST", "/api/v1/school-calendar/closures"],
  ["DELETE", "/api/v1/school-calendar/closures/1"],
  ["GET", "/api/v1/school-calendar/effective-days"],
  ["POST", "/api/v1/integrations/google-calendar/sync"]
]

const uiCrudRoutes = (
  resource: string,
  detail = true
): Array<Route> => [
  ["GET", `/dashboard/${resource}`],
  ["GET", `/dashboard/${resource}/new`],
  ["POST", `/dashboard/${resource}`],
  ...(detail ? [["GET", `/dashboard/${resource}/1`]] as Array<Route> : []),
  ["GET", `/dashboard/${resource}/1/edit`],
  ["POST", `/dashboard/${resource}/1`],
  ["DELETE", `/dashboard/${resource}/1`]
]
const uiRoutes: ReadonlyArray<Route> = [
  ["GET", "/dashboard/"],
  ...uiCrudRoutes("accounts", false),
  ...uiCrudRoutes("contacts", false),
  ["POST", "/dashboard/contacts/1/portal-code"],
  ...uiCrudRoutes("journals"),
  ...uiCrudRoutes("income", false),
  ...uiCrudRoutes("expenses", false),
  ...uiCrudRoutes("invoices"),
  ["POST", "/dashboard/invoices/generate-recurring"],
  ["POST", "/dashboard/invoices/bulk-delete"],
  ["POST", "/dashboard/invoices/bulk-send"],
  ["POST", "/dashboard/invoices/1/send"],
  ["POST", "/dashboard/invoices/1/payment"],
  ["GET", "/dashboard/invoices/1/print"],
  ["GET", "/dashboard/invoices/1/pdf"],
  ["GET", "/dashboard/invoices/1/whatsapp"],
  ...uiCrudRoutes("credit-notes"),
  ["POST", "/dashboard/credit-notes/1/issue"],
  ["POST", "/dashboard/credit-notes/1/void"],
  ...uiCrudRoutes("bills"),
  ["POST", "/dashboard/bills/1/receive"],
  ["POST", "/dashboard/bills/1/payment"],
  ...[
    "trial-balance",
    "profit-loss",
    "balance-sheet",
    "cash-flow",
    "general-ledger"
  ].map((report): Route => ["GET", `/dashboard/reports/${report}`]),
  ...uiCrudRoutes("users", false),
  ["GET", "/dashboard/roles"],
  ["GET", "/dashboard/roles/new"],
  ["POST", "/dashboard/roles"],
  ["GET", "/dashboard/roles/custom/edit"],
  ["POST", "/dashboard/roles/custom"],
  ["DELETE", "/dashboard/roles/custom"],
  ["GET", "/dashboard/htmx/journal-line"],
  ["GET", "/dashboard/htmx/invoice-line"],
  ["GET", "/dashboard/htmx/bill-line"],
  ["GET", "/dashboard/htmx/credit-note-line"],
  ["GET", "/dashboard/password/change"],
  ["POST", "/dashboard/password/change"],
  ["POST", "/dashboard/logout"],
  ["GET", "/dashboard/settings/api-tokens"],
  ["GET", "/dashboard/settings/api-tokens/new"],
  ["GET", "/dashboard/settings/api-tokens/created"],
  ["POST", "/dashboard/settings/api-tokens"],
  ["POST", "/dashboard/settings/api-tokens/1/revoke"],
  ["GET", "/dashboard/settings/company"],
  ["POST", "/dashboard/settings/company"],
  ["GET", "/dashboard/settings/school-calendar"],
  ["POST", "/dashboard/settings/school-calendar/closures"],
  ["POST", "/dashboard/settings/school-calendar/closures/1/delete"],
  ["POST", "/dashboard/settings/school-calendar/google-calendar-id"],
  ["POST", "/dashboard/integrations/google-calendar/connect"],
  ["GET", "/dashboard/integrations/google-calendar/callback"],
  ["POST", "/dashboard/integrations/google-calendar/sync"],
  ["POST", "/dashboard/integrations/google-calendar/disconnect"],
  ["GET", "/dashboard/audit"]
]

let directory = ""
let handler: (request: Request) => Promise<Response>
let dispose: () => Promise<void>

beforeAll(async () => {
  directory = mkdtempSync(join(tmpdir(), "latasya-route-parity-"))
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  const web = HttpApp.toWebHandlerLayer(
    makeRouter("route-parity", true),
    runtimeLayer(databasePath)
  )
  handler = web.handler
  dispose = web.dispose
})
afterAll(async () => {
  await dispose()
  rmSync(directory, { recursive: true, force: true })
})

describe("legacy route manifest", () => {
  test("keeps every JSON API method registered", async () => {
    for (const [method, path] of apiRoutes) {
      const response = await handler(new Request(`http://localhost${path}`, {
        method
      }))
      expect(response.status, `${method} ${path}`).not.toBe(404)
      expect(response.status, `${method} ${path}`).not.toBe(500)
    }
  })

  test("keeps every authenticated HTML method registered", async () => {
    for (const [method, path] of uiRoutes) {
      const response = await handler(new Request(`http://localhost${path}`, {
        method
      }))
      expect(response.status, `${method} ${path}`).toBe(303)
      expect(response.headers.get("location"), `${method} ${path}`).toBe(
        "/dashboard/login"
      )
    }
  })

  test("keeps operational and public entry routes registered", async () => {
    const health = await handler(new Request("http://localhost/healthz"))
    const home = await handler(new Request("http://localhost/"))
    const login = await handler(new Request(
      "http://localhost/dashboard/login"
    ))
    const staticAsset = await handler(new Request(
      "http://localhost/static/css/app.css"
    ))
    const slash = await handler(new Request("http://localhost/dashboard"))
    expect(health.status).toBe(200)
    expect(home.status).toBe(200)
    expect(login.status).toBe(200)
    expect(staticAsset.status).toBe(200)
    expect(slash.status).toBe(301)
    expect(slash.headers.get("location")).toBe("/dashboard/")
  })
})
