import { HttpRouter, HttpServerResponse } from "@effect/platform"
import { Effect } from "effect"
import { Roles, type Role } from "../../domain/access/roles.ts"
import { Users, type User } from "../../domain/access/users.ts"
import { Audit, auditDiff } from "../../domain/audit/audit.ts"
import type { CookieAuthentication } from "../../domain/auth/authentication.ts"
import {
  allCapabilities,
  isCapability,
  type Capability
} from "../../domain/auth/capability.ts"
import {
  dashboardBasePath,
  protectedUiHandler,
  renderUiPage,
  uiFlashCookie,
  uiPlainError,
  uiRedirect
} from "./ui-auth.ts"
import { requestMetadata } from "./request-metadata.ts"

const actor = (auth: CookieAuthentication) => ({
  id: auth.user.id,
  username: auth.user.username
})
const userManage = (auth: CookieAuthentication) =>
  auth.user.role === "admin" ||
  auth.effectiveCapabilities.includes("users.manage")
const roleManage = (auth: CookieAuthentication) =>
  auth.user.role === "admin" ||
  auth.effectiveCapabilities.includes("roles.manage")
const parseId = (value: string | undefined) => {
  const parsed = Number(value)
  return value !== undefined && /^[+-]?\d+$/.test(value) &&
      Number.isSafeInteger(parsed)
    ? parsed
    : undefined
}
const notFound = () => uiPlainError(404, "404 page not found")
const internal = () => uiPlainError(500, "Internal Server Error")
const userData = (user: User) => ({
  ID: user.id,
  Username: user.username,
  FullName: user.full_name,
  Role: user.role,
  IsActive: user.is_active,
  MustChangePassword: user.must_change_password,
  CreatedAt: user.created_at,
  UpdatedAt: user.updated_at
})
const roleData = (role: Role) => ({
  Name: role.name,
  Description: role.description,
  IsSystem: role.is_system,
  Capabilities: role.capabilities,
  CreatedAt: role.created_at,
  UpdatedAt: role.updated_at
})
const renderUserForm = (
  auth: CookieAuthentication,
  request: Parameters<typeof renderUiPage>[0],
  user: ReturnType<typeof userData>,
  roles: ReadonlyArray<Role>,
  errors: Readonly<Record<string, string>>,
  isEdit: boolean
) => renderUiPage(request, "users/form", isEdit ? "Edit User" : "New User", {
  User: user,
  Roles: roles.map(roleData),
  Errors: errors,
  IsEdit: isEdit
}, auth)
const roleOptions = Effect.gen(function*() {
  const roles = yield* Roles
  return yield* roles.list
})
const userErrors = (
  username: string,
  fullName: string,
  role: string,
  password: string,
  editing: boolean,
  roleValid: boolean
) => {
  const errors: Record<string, string> = {}
  if (!editing && username === "") errors.username = "Username is required"
  if (fullName === "") errors.full_name = "Full name is required"
  if (!roleValid) errors.role = "Invalid role"
  if (!editing && password === "") errors.password = "Password is required"
  if (password !== "" && new TextEncoder().encode(password).length < 4) {
    errors.password = "Password must be at least 4 characters"
  }
  return errors
}

const usersList = HttpRouter.get(`${dashboardBasePath}/users`,
  protectedUiHandler((auth, request) => {
    if (!userManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const users = yield* Users
      return renderUiPage(
        request,
        "users/index",
        "Users",
        (yield* users.list).map(userData),
        auth
      )
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))
const usersNew = HttpRouter.get(`${dashboardBasePath}/users/new`,
  protectedUiHandler((auth, request) => {
    if (!userManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return roleOptions.pipe(Effect.map((roles) =>
      renderUserForm(auth, request, {
        ID: 0,
        Username: "",
        FullName: "",
        Role: "viewer",
        IsActive: true,
        MustChangePassword: true,
        CreatedAt: "",
        UpdatedAt: ""
      }, roles, {}, false)
    ), Effect.catchAll(() => Effect.succeed(internal())))
  }))
const usersCreate = HttpRouter.post(`${dashboardBasePath}/users`,
  protectedUiHandler((auth, request, form) => {
    if (!userManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    const username = form.get("username") ?? ""
    const fullName = form.get("full_name") ?? ""
    const role = form.get("role") ?? ""
    const password = form.get("password") ?? ""
    const isActive = form.get("is_active") === "on"
    return Effect.gen(function*() {
      const users = yield* Users
      const roles = yield* roleOptions
      const errors = userErrors(
        username,
        fullName,
        role,
        password,
        false,
        yield* users.roleExists(role)
      )
      const shaped = {
        ID: 0,
        Username: username,
        FullName: fullName,
        Role: role,
        IsActive: isActive,
        MustChangePassword: true,
        CreatedAt: "",
        UpdatedAt: ""
      }
      if (Object.keys(errors).length) {
        return renderUserForm(auth, request, shaped, roles, errors, false)
      }
      const created = yield* users.create({
        username,
        fullName,
        role,
        isActive,
        password
      }).pipe(Effect.either)
      if (created._tag === "Left") {
        return renderUserForm(auth, request, shaped, roles, {
          username: "Username already exists"
        }, false)
      }
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "user.create",
        actor: actor(auth),
        targetType: "user",
        targetId: created.right.id,
        targetLabel: username,
        metadata: { after: {
          username,
          full_name: fullName,
          role,
          is_active: isActive
        } }
      })
      return uiRedirect(`${dashboardBasePath}/users`, {
        "set-cookie": uiFlashCookie("User created successfully")
      })
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))
const usersEdit = HttpRouter.get(`${dashboardBasePath}/users/:id/edit`,
  protectedUiHandler((auth, request) => {
    if (!userManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const userId = parseId((yield* HttpRouter.params).id)
      if (userId === undefined) return notFound()
      const users = yield* Users
      const [user, roles] = yield* Effect.all([
        users.get(userId),
        roleOptions
      ])
      return renderUserForm(auth, request, userData(user), roles, {}, true)
    }).pipe(Effect.catchAll(() => Effect.succeed(notFound())))
  }))
const usersUpdate = HttpRouter.post(`${dashboardBasePath}/users/:id`,
  protectedUiHandler((auth, request, form) => {
    if (!userManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const userId = parseId((yield* HttpRouter.params).id)
      if (userId === undefined) return notFound()
      const users = yield* Users
      const existing = yield* users.get(userId)
      const roles = yield* roleOptions
      const fullName = form.get("full_name") ?? ""
      const role = form.get("role") ?? ""
      const password = form.get("password") ?? ""
      const isActive = auth.user.id === userId
        ? true
        : form.get("is_active") === "on"
      const errors = userErrors(
        existing.username,
        fullName,
        role,
        password,
        true,
        yield* users.roleExists(role)
      )
      const shaped = {
        ...userData(existing),
        FullName: fullName,
        Role: role,
        IsActive: isActive
      }
      if (Object.keys(errors).length) {
        return renderUserForm(auth, request, shaped, roles, errors, true)
      }
      const updated = yield* users.update(auth.user.id, userId, {
        fullName,
        role,
        isActive,
        ...(password === "" ? {} : { password })
      })
      const metadata = auditDiff(
        {
          full_name: existing.full_name,
          role: existing.role,
          is_active: existing.is_active
        },
        {
          full_name: updated.full_name,
          role: updated.role,
          is_active: updated.is_active
        },
        ["full_name", "role", "is_active"]
      )
      const audit = yield* Audit
      if (metadata !== undefined || password !== "") {
        yield* audit.log(requestMetadata(request), {
          action: "user.update",
          actor: actor(auth),
          targetType: "user",
          targetId: userId,
          targetLabel: existing.username,
          metadata: {
            ...(metadata ?? {}),
            ...(password === "" ? {} : { password_reset: true })
          }
        })
      }
      return uiRedirect(`${dashboardBasePath}/users`, {
        "set-cookie": uiFlashCookie("User updated successfully")
      })
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))
const usersDelete = HttpRouter.del(`${dashboardBasePath}/users/:id`,
  protectedUiHandler((auth, request) => {
    if (!userManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const userId = parseId((yield* HttpRouter.params).id)
      if (userId === undefined) return notFound()
      if (userId === auth.user.id) {
        return uiRedirect(`${dashboardBasePath}/users`, {
          "set-cookie": uiFlashCookie("Cannot delete your own account")
        })
      }
      const users = yield* Users
      const removed = yield* users.deactivate(auth.user.id, userId)
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "user.delete",
        actor: actor(auth),
        targetType: "user",
        targetId: userId,
        targetLabel: removed.username,
        metadata: {
          before: { is_active: removed.is_active },
          after: { is_active: false }
        }
      })
      return request.headers["hx-request"] === "true"
        ? HttpServerResponse.empty({ status: 200 })
        : uiRedirect(`${dashboardBasePath}/users`, {
          "set-cookie": uiFlashCookie("User deactivated")
        })
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))

const roleForm = (
  auth: CookieAuthentication,
  request: Parameters<typeof renderUiPage>[0],
  role: {
    readonly Name: string
    readonly Description: string
    readonly IsSystem: boolean
    readonly Capabilities: ReadonlyArray<string>
    readonly CreatedAt: string
    readonly UpdatedAt: string
  },
  errors: Readonly<Record<string, string>>,
  editing: boolean
) => renderUiPage(request, "roles/form", editing ? "Edit Role" : "New Role", {
  Role: role,
  AllCapabilities: allCapabilities,
  Errors: errors,
  IsEdit: editing
}, auth)
const readCapabilities = (form: URLSearchParams) =>
  form.getAll("capabilities")
const roleErrors = (
  name: string,
  capabilities: ReadonlyArray<string>,
  editing: boolean
) => {
  const errors: Record<string, string> = {}
  if (!editing) {
    if (name === "") errors.name = "Name is required"
    else if (!/^[a-z][a-z0-9_-]*$/.test(name)) {
      errors.name =
        "Use lowercase letters, digits, hyphens or underscores " +
        "(must start with a letter)"
    } else if (name === "admin") errors.name = "Reserved role name"
  }
  const unknown = capabilities.find((capability) => !isCapability(capability))
  if (unknown !== undefined) {
    errors.capabilities = `Unknown capability: ${unknown}`
  }
  return errors
}
const capabilities = (values: ReadonlyArray<string>) =>
  values.filter(isCapability) as ReadonlyArray<Capability>
const rolesList = HttpRouter.get(`${dashboardBasePath}/roles`,
  protectedUiHandler((auth, request) => {
    if (!roleManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const roles = yield* Roles
      return renderUiPage(
        request,
        "roles/index",
        "Roles",
        (yield* roles.list).map(roleData),
        auth
      )
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))
const rolesNew = HttpRouter.get(`${dashboardBasePath}/roles/new`,
  protectedUiHandler((auth, request) => {
    if (!roleManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.succeed(roleForm(auth, request, {
      Name: "",
      Description: "",
      IsSystem: false,
      Capabilities: [],
      CreatedAt: "",
      UpdatedAt: ""
    }, {}, false))
  }))
const rolesCreate = HttpRouter.post(`${dashboardBasePath}/roles`,
  protectedUiHandler((auth, request, form) => {
    if (!roleManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    const name = (form.get("name") ?? "").trim()
    const description = (form.get("description") ?? "").trim()
    const raw = readCapabilities(form)
    const errors = roleErrors(name, raw, false)
    const shaped = {
      Name: name,
      Description: description,
      IsSystem: false,
      Capabilities: raw,
      CreatedAt: "",
      UpdatedAt: ""
    }
    if (Object.keys(errors).length) {
      return Effect.succeed(roleForm(auth, request, shaped, errors, false))
    }
    return Effect.gen(function*() {
      const roles = yield* Roles
      const result = yield* roles.create({
        name,
        description,
        capabilities: capabilities(raw)
      }).pipe(Effect.either)
      if (result._tag === "Left") {
        return roleForm(auth, request, shaped, {
          name: "Role name already exists"
        }, false)
      }
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "role.create",
        actor: actor(auth),
        targetType: "role",
        targetLabel: name,
        metadata: { after: {
          name,
          description,
          capabilities: raw
        } }
      })
      return uiRedirect(`${dashboardBasePath}/roles`, {
        "set-cookie": uiFlashCookie("Role created successfully")
      })
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))
const rolesEdit = HttpRouter.get(`${dashboardBasePath}/roles/:name/edit`,
  protectedUiHandler((auth, request) => {
    if (!roleManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const name = (yield* HttpRouter.params).name ?? ""
      if (name === "admin") {
        return uiPlainError(403, "The admin role cannot be edited")
      }
      const roles = yield* Roles
      return roleForm(auth, request, roleData(yield* roles.get(name)), {}, true)
    }).pipe(Effect.catchAll(() => Effect.succeed(notFound())))
  }))
const rolesUpdate = HttpRouter.post(`${dashboardBasePath}/roles/:name`,
  protectedUiHandler((auth, request, form) => {
    if (!roleManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const name = (yield* HttpRouter.params).name ?? ""
      if (name === "admin") {
        return uiPlainError(403, "The admin role cannot be edited")
      }
      const roles = yield* Roles
      const existing = yield* roles.get(name)
      const description = (form.get("description") ?? "").trim()
      const raw = readCapabilities(form)
      const errors = roleErrors(name, raw, true)
      const shaped = {
        ...roleData(existing),
        Description: description,
        Capabilities: raw
      }
      if (Object.keys(errors).length) {
        return roleForm(auth, request, shaped, errors, true)
      }
      const updated = yield* roles.update(name, {
        description,
        capabilities: capabilities(raw)
      })
      const metadata = auditDiff(
        {
          description: existing.description,
          capabilities: [...existing.capabilities].sort()
        },
        {
          description: updated.description,
          capabilities: [...updated.capabilities].sort()
        },
        ["description", "capabilities"]
      )
      if (metadata) {
        const audit = yield* Audit
        yield* audit.log(requestMetadata(request), {
          action: "role.update",
          actor: actor(auth),
          targetType: "role",
          targetLabel: name,
          metadata
        })
      }
      return uiRedirect(`${dashboardBasePath}/roles`, {
        "set-cookie": uiFlashCookie("Role updated successfully")
      })
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))
const rolesDelete = HttpRouter.del(`${dashboardBasePath}/roles/:name`,
  protectedUiHandler((auth, request) => {
    if (!roleManage(auth)) return Effect.succeed(uiPlainError(403, "Forbidden"))
    return Effect.gen(function*() {
      const name = (yield* HttpRouter.params).name ?? ""
      const roles = yield* Roles
      const existing = yield* roles.get(name)
      if (existing.is_system) {
        return uiRedirect(`${dashboardBasePath}/roles`, {
          "set-cookie": uiFlashCookie("System roles cannot be deleted")
        })
      }
      const result = yield* roles.remove(name).pipe(Effect.either)
      if (result._tag === "Left") {
        return uiRedirect(`${dashboardBasePath}/roles`, {
          "set-cookie": uiFlashCookie(
            "Cannot delete role: still assigned to one or more users"
          )
        })
      }
      const audit = yield* Audit
      yield* audit.log(requestMetadata(request), {
        action: "role.delete",
        actor: actor(auth),
        targetType: "role",
        targetLabel: name,
        metadata: { before: {
          name,
          description: existing.description,
          capabilities: existing.capabilities
        } }
      })
      return request.headers["hx-request"] === "true"
        ? HttpServerResponse.empty({ status: 200 })
        : uiRedirect(`${dashboardBasePath}/roles`, {
          "set-cookie": uiFlashCookie("Role deleted")
        })
    }).pipe(Effect.catchAll(() => Effect.succeed(internal())))
  }))

export const addUiAccessRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(
  usersList,
  usersNew,
  usersCreate,
  usersEdit,
  usersUpdate,
  usersDelete,
  rolesList,
  rolesNew,
  rolesCreate,
  rolesEdit,
  rolesUpdate,
  rolesDelete
)
