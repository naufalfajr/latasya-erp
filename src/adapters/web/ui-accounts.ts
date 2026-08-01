import { HttpRouter, HttpServerResponse } from "@effect/platform"
import { Effect } from "effect"
import {
  type Account,
  type AccountValues,
  Accounts
} from "../../domain/accounting/accounts.ts"
import { Audit, auditDiff } from "../../domain/audit/audit.ts"
import type { CookieAuthentication } from "../../domain/auth/authentication.ts"
import {
  dashboardBasePath,
  protectedUiHandler,
  renderUiPage,
  uiFlashCookie,
  uiPlainError,
  uiRedirect
} from "./ui-auth.ts"
import { requestMetadata } from "./request-metadata.ts"

const hasCapability = (
  authenticated: CookieAuthentication,
  capability: string
) =>
  authenticated.user.role === "admin" ||
  authenticated.effectiveCapabilities.includes(capability as never)

const accountData = (account: Account | {
  readonly id?: number
  readonly code: string
  readonly name: string
  readonly account_type: string
  readonly normal_balance: string
  readonly is_system?: boolean
  readonly is_active: boolean
  readonly is_cash: boolean
  readonly description: string
}) => ({
  ID: account.id ?? 0,
  Code: account.code,
  Name: account.name,
  AccountType: account.account_type,
  NormalBalance: account.normal_balance,
  IsSystem: account.is_system ?? false,
  IsActive: account.is_active,
  IsCash: account.is_cash,
  Description: account.description
})

const valuesFromForm = (form: URLSearchParams): AccountValues => ({
  code: form.get("code") ?? "",
  name: form.get("name") ?? "",
  accountType: form.get("account_type") ?? "",
  normalBalance: form.get("normal_balance") ?? "",
  description: form.get("description") ?? "",
  isActive: form.get("is_active") === "on",
  isCash: form.get("is_cash") === "on"
})

const validate = (values: AccountValues) => {
  const errors: Record<string, string> = {}
  if (values.code === "") {
    errors.code = "Code is required"
  }
  if (values.name === "") {
    errors.name = "Name is required"
  }
  if (values.accountType === "") {
    errors.account_type = "Account type is required"
  }
  if (values.normalBalance === "") {
    errors.normal_balance = "Normal balance is required"
  }
  if (
    values.isCash &&
    (values.accountType !== "asset" || values.normalBalance !== "debit")
  ) {
    errors.is_cash = "cash accounts must be debit-normal assets"
  }
  return errors
}

const formAccount = (
  values: AccountValues,
  id = 0,
  isSystem = false
) => accountData({
  id,
  code: values.code,
  name: values.name,
  account_type: values.accountType,
  normal_balance: values.normalBalance,
  is_system: isSystem,
  is_active: values.isActive,
  is_cash: values.isCash,
  description: values.description
})

const formPage = (
  authenticated: CookieAuthentication,
  request: Parameters<typeof renderUiPage>[0],
  title: "New Account" | "Edit Account",
  account: ReturnType<typeof accountData>,
  errors: Readonly<Record<string, string>>,
  isEdit: boolean
) => renderUiPage(
  request,
  "accounts/form",
  title,
  { Account: account, Errors: errors, IsEdit: isEdit },
  authenticated
)

const parseId = (value: string | undefined) =>
  value !== undefined && /^[+-]?\d+$/.test(value)
    ? Number(value)
    : undefined

const notFound = () => uiPlainError(404, "404 page not found")
const internal = () => uiPlainError(500, "Internal Server Error")

const addListRoute = HttpRouter.get(
  "/dashboard/accounts",
  protectedUiHandler((authenticated, request) =>
    Effect.gen(function*() {
      const query = new URL(request.url, "http://localhost").searchParams
      const filter = query.get("type") ?? ""
      const search = query.get("search") ?? ""
      const accounts = yield* Accounts
      const [filtered, all] = yield* Effect.all([
        accounts.list({ type: filter, search }),
        accounts.list({ type: "", search: "" })
      ])
      const active = filtered.filter((account) => account.is_active)
      const allActive = all.filter((account) => account.is_active)
      const typeCounts: Record<string, number> = { all: allActive.length }
      for (const account of allActive) {
        typeCounts[account.account_type] =
          (typeCounts[account.account_type] ?? 0) + 1
      }
      return renderUiPage(
        request,
        "accounts/index",
        "Chart of Accounts",
        {
          Accounts: active.map(accountData),
          Filter: filter,
          Search: search,
          TypeCounts: typeCounts
        },
        authenticated
      )
    }).pipe(Effect.catchTag("AccountStoreError", () => Effect.succeed(internal())))
  )
)

const addNewRoute = HttpRouter.get(
  "/dashboard/accounts/new",
  protectedUiHandler((authenticated, request) =>
    Effect.succeed(formPage(
      authenticated,
      request,
      "New Account",
      formAccount({
        code: "",
        name: "",
        accountType: "",
        normalBalance: "",
        description: "",
        isActive: true,
        isCash: false
      }),
      {},
      false
    ))
  )
)

const addCreateRoute = HttpRouter.post(
  "/dashboard/accounts",
  protectedUiHandler((authenticated, request, form) => {
    if (!hasCapability(authenticated, "accounts.manage")) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    const values = valuesFromForm(form)
    const errors = validate(values)
    if (Object.keys(errors).length > 0) {
      return Effect.succeed(formPage(
        authenticated,
        request,
        "New Account",
        formAccount(values),
        errors,
        false
      ))
    }
    return Effect.gen(function*() {
      const accounts = yield* Accounts
      const account = yield* accounts.create(values)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "account.create",
        actor: {
          id: authenticated.user.id,
          username: authenticated.user.username
        },
        targetType: "account",
        targetId: account.id,
        targetLabel: account.code,
        metadata: {
          after: {
            code: account.code,
            name: account.name,
            account_type: account.account_type,
            normal_balance: account.normal_balance,
            is_active: account.is_active,
            is_cash: account.is_cash
          }
        }
      })
      return uiRedirect(`${dashboardBasePath}/accounts`, {
        "set-cookie": uiFlashCookie("Account created successfully")
      })
    }).pipe(
      Effect.catchTags({
        AccountConflict: () => Effect.succeed(formPage(
          authenticated,
          request,
          "New Account",
          formAccount(values),
          { code: "Account code already exists" },
          false
        )),
        AccountStoreError: () => Effect.succeed(formPage(
          authenticated,
          request,
          "New Account",
          formAccount(values),
          { code: "Account code already exists" },
          false
        ))
      })
    )
  })
)

const addEditRoute = HttpRouter.get(
  "/dashboard/accounts/:id/edit",
  protectedUiHandler((authenticated, request) =>
    Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const accounts = yield* Accounts
      const account = yield* accounts.get(id)
      return formPage(
        authenticated,
        request,
        "Edit Account",
        accountData(account),
        {},
        true
      )
    }).pipe(
      Effect.catchTags({
        AccountNotFound: () => Effect.succeed(notFound()),
        AccountStoreError: () => Effect.succeed(notFound())
      })
    )
  )
)

const addUpdateRoute = HttpRouter.post(
  "/dashboard/accounts/:id",
  protectedUiHandler((authenticated, request, form) => {
    if (!hasCapability(authenticated, "accounts.manage")) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    return Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const accounts = yield* Accounts
      const existing = yield* accounts.get(id)
      const values = valuesFromForm(form)
      const errors = validate(values)
      if (Object.keys(errors).length > 0) {
        return formPage(
          authenticated,
          request,
          "Edit Account",
          formAccount(values, id, existing.is_system),
          errors,
          true
        )
      }
      const updated = yield* accounts.update(id, values)
      const metadata = auditDiff(
        {
          code: existing.code,
          name: existing.name,
          account_type: existing.account_type,
          normal_balance: existing.normal_balance,
          description: existing.description,
          is_active: existing.is_active,
          is_cash: existing.is_cash
        },
        {
          code: updated.code,
          name: updated.name,
          account_type: updated.account_type,
          normal_balance: updated.normal_balance,
          description: updated.description,
          is_active: updated.is_active,
          is_cash: updated.is_cash
        },
        [
          "code",
          "name",
          "account_type",
          "normal_balance",
          "description",
          "is_active",
          "is_cash"
        ]
      )
      if (metadata !== undefined) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "account.update",
          actor: {
            id: authenticated.user.id,
            username: authenticated.user.username
          },
          targetType: "account",
          targetId: id,
          targetLabel: existing.code,
          metadata
        })
      }
      return uiRedirect(`${dashboardBasePath}/accounts`, {
        "set-cookie": uiFlashCookie("Account updated successfully")
      })
    }).pipe(
      Effect.catchTags({
        AccountNotFound: () => Effect.succeed(notFound()),
        AccountStoreError: () => Effect.succeed(internal())
      })
    )
  })
)

const addDeleteRoute = HttpRouter.del(
  "/dashboard/accounts/:id",
  protectedUiHandler((authenticated, request) => {
    if (!hasCapability(authenticated, "accounts.manage")) {
      return Effect.succeed(uiPlainError(403, "Forbidden"))
    }
    return Effect.gen(function*() {
      const id = parseId((yield* HttpRouter.params).id)
      if (id === undefined) {
        return notFound()
      }
      const accounts = yield* Accounts
      const existing = yield* accounts.get(id)
      if (existing.is_system) {
        return uiPlainError(403, "Cannot delete system account")
      }
      yield* accounts.remove(id)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "account.delete",
        actor: {
          id: authenticated.user.id,
          username: authenticated.user.username
        },
        targetType: "account",
        targetId: id,
        targetLabel: existing.code,
        metadata: {
          before: {
            code: existing.code,
            name: existing.name,
            account_type: existing.account_type,
            normal_balance: existing.normal_balance,
            is_cash: existing.is_cash
          }
        }
      })
      if (request.headers["hx-request"] === "true") {
        return HttpServerResponse.empty({ status: 200 })
      }
      return uiRedirect(`${dashboardBasePath}/accounts`, {
        "set-cookie": uiFlashCookie("Account deleted successfully")
      })
    }).pipe(
      Effect.catchTags({
        AccountNotFound: () => Effect.succeed(notFound()),
        AccountConflict: () => Effect.succeed(internal()),
        AccountStoreError: () => Effect.succeed(internal())
      })
    )
  })
)

export const addUiAccountRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) =>
  router.pipe(
    addListRoute,
    addNewRoute,
    addCreateRoute,
    addEditRoute,
    addUpdateRoute,
    addDeleteRoute
  )
