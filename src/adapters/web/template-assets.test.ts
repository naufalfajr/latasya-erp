import { describe, expect, test } from "bun:test"
import { pageTemplate, templatePageNames } from "./template-assets.ts"

describe("embedded page templates", () => {
  test("all repository pages parse with the compatibility renderer", () => {
    for (const page of templatePageNames) {
      expect(() => pageTemplate(page)).not.toThrow()
    }
  })

  test("renders the unchanged login page through the shared base", () => {
    const html = pageTemplate("auth/login").render("base", {
      User: null,
      Title: "Login",
      Flash: "",
      Path: "/login",
      CSRFToken: "",
      BasePath: "/dashboard",
      Data: { Username: `admin"><script>` }
    })

    expect(html).toContain("<title>Login — Latasya ERP</title>")
    expect(html).toContain('action="/dashboard/login"')
    expect(html).toContain("admin&#34;&gt;&lt;script&gt;")
  })
})
