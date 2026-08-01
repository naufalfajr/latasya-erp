import { describe, expect, test } from "bun:test"
import { GoTemplateSet } from "./go-template.ts"

describe("GoTemplateSet", () => {
  test("renders definitions, blocks, variables, conditions, and escaping", () => {
    const templates = new GoTemplateSet([
      `{{define "base"}}<title>{{if .Title}}{{.Title}} — {{end}}ERP</title>{{block "content" .}}fallback{{end}}{{end}}`,
      `{{define "content"}}{{$data := .Data}}{{if $data.Visible}}<p>{{$data.Value}}</p>{{else}}hidden{{end}}{{end}}`
    ])

    expect(
      templates.render("base", {
        Title: "Income & Expenses",
        Data: { Visible: true, Value: `<unsafe "value">` }
      })
    ).toBe(
      "<title>Income &amp; Expenses — ERP</title><p>&lt;unsafe &#34;value&#34;&gt;</p>"
    )
  })

  test("renders ranges with scoped variables and else branches", () => {
    const templates = new GoTemplateSet([
      `{{define "list"}}{{range $i, $item := .Items}}{{$i}}={{$item.Name}};{{else}}empty{{end}}{{end}}`
    ])

    expect(
      templates.render("list", {
        Items: [{ Name: "one" }, { Name: "two" }]
      })
    ).toBe("0=one;1=two;")
    expect(templates.render("list", { Items: [] })).toBe("empty")
  })

  test("supports repository functions, methods, and nested expressions", () => {
    const templates = new GoTemplateSet([
      `{{define "value"}}{{if .User.HasCapability "users.manage"}}{{formatIDR (add .Amount 500)}}|{{formatQty .Quantity}}|{{formatDate .Date}}|{{toJSON .JSON}}{{end}}{{end}}`
    ])

    expect(
      templates.render("value", {
        User: { Role: "admin" },
        Amount: 1_500,
        Quantity: 125,
        Date: "2026-07-26",
        JSON: { html: "</script>&" }
      })
    ).toBe(
      `Rp 2.000|1.25|26 Jul 2026|{"html":"\\u003c/script\\u003e\\u0026"}`
    )
  })

  test("passes dict data into partial templates", () => {
    const templates = new GoTemplateSet([
      `{{define "row"}}{{.Index}}:{{.Line.Description}}/{{range .Accounts}}{{.Code}}{{end}}{{end}}`,
      `{{define "form"}}{{range $i, $line := .Lines}}{{template "row" (dict "Index" $i "Line" $line "Accounts" $.Accounts)}}{{end}}{{end}}`
    ])

    expect(
      templates.render("form", {
        Lines: [{ Description: "Tuition" }],
        Accounts: [{ Code: "4100" }]
      })
    ).toBe("0:Tuition/4100")
  })
})
