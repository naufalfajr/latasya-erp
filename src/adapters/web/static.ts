import {
  HttpRouter,
  HttpServerRequest,
  HttpServerResponse
} from "@effect/platform"
import { Effect } from "effect"
import appCss from "../../../static/css/app.css" with { type: "file" }
import inputCss from "../../../static/css/input.css" with { type: "file" }
import financialCharts from "../../../static/js/financial-charts.js" with { type: "file" }
import htmx from "../../../static/js/htmx.min.js" with { type: "file" }

interface StaticAsset {
  readonly path: string
  readonly contentType: string
}

const assets: Readonly<Record<string, StaticAsset>> = {
  "/static/css/app.css": {
    path: appCss,
    contentType: "text/css; charset=utf-8"
  },
  "/static/css/input.css": {
    path: inputCss,
    contentType: "text/css; charset=utf-8"
  },
  "/static/js/financial-charts.js": {
    path: financialCharts,
    contentType: "text/javascript; charset=utf-8"
  },
  "/static/js/htmx.min.js": {
    path: htmx,
    contentType: "text/javascript; charset=utf-8"
  }
}

const notFound = HttpServerResponse.text("404 page not found\n", {
  status: 404,
  headers: {
    "content-type": "text/plain; charset=utf-8"
  }
})

export const addStaticRoutes = HttpRouter.get(
  "/static/*",
  Effect.gen(function*() {
    const request = yield* HttpServerRequest.HttpServerRequest
    const path = new URL(request.url, "http://localhost").pathname
    const asset = assets[path]
    if (asset === undefined) {
      return notFound
    }

    const file = Bun.file(asset.path)
    return yield* HttpServerResponse.file(asset.path).pipe(
      Effect.map(
        HttpServerResponse.setHeaders({
          "accept-ranges": "bytes",
          "content-length": String(file.size),
          "content-type": asset.contentType
        })
      )
    )
  })
)
