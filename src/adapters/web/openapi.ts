import { HttpRouter, HttpServerResponse } from "@effect/platform"
import { Effect } from "effect"
import openApi from "../../../api/openapi.yaml" with { type: "file" }
import { protectedApiHandler } from "./auth-api.ts"

const addOpenApiRoute = HttpRouter.get(
  "/api/v1/openapi.yaml",
  protectedApiHandler(() =>
    Effect.gen(function*() {
      const response = yield* HttpServerResponse.file(openApi)
      return HttpServerResponse.setHeader(
        response,
        "content-type",
        "application/yaml; charset=utf-8"
      )
    }).pipe(
      Effect.catchAll(() =>
        Effect.succeed(HttpServerResponse.text(
          "openapi spec not found\n",
          {
            status: 500,
            headers: {
              "content-type": "text/plain; charset=utf-8"
            }
          }
        ))
      )
    )
  )
)

export const addOpenApiRoutes = <E, R>(
  router: HttpRouter.HttpRouter<E, R>
) => router.pipe(addOpenApiRoute)
