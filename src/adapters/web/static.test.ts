import { afterAll, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { makeRouter } from "./router.ts"

const web = HttpApp.toWebHandlerLayer(
  makeRouter("test"),
  runtimeLayer(":memory:")
)

afterAll(web.dispose)

describe("GET /static/*", () => {
  test("serves embedded assets at the current paths", async () => {
    const response = await web.handler(
      new Request("http://localhost/static/js/financial-charts.js")
    )

    expect(response.status).toBe(200)
    expect(response.headers.get("content-type")).toBe(
      "text/javascript; charset=utf-8"
    )
    expect(response.headers.get("accept-ranges")).toBe("bytes")
    expect(response.headers.get("content-length")).toBe("16012")
    expect(await response.text()).toContain("FinancialCharts")
  })

  test("preserves static metadata for HEAD requests", async () => {
    const response = await web.handler(
      new Request("http://localhost/static/css/app.css", {
        method: "HEAD"
      })
    )

    expect(response.status).toBe(200)
    expect(response.headers.get("content-type")).toBe(
      "text/css; charset=utf-8"
    )
    expect(response.headers.get("accept-ranges")).toBe("bytes")
    expect(response.headers.get("content-length")).toBe("96894")
    expect(await response.text()).toBe("")
  })

  test("matches the current missing-file response", async () => {
    const response = await web.handler(
      new Request("http://localhost/static/not-found.txt")
    )

    expect(response.status).toBe(404)
    expect(response.headers.get("content-type")).toBe(
      "text/plain; charset=utf-8"
    )
    expect(await response.text()).toBe("404 page not found\n")
  })
})
