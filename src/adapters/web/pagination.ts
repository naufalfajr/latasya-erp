import type { HttpServerRequest } from "@effect/platform"

export type Page = {
  readonly page: number
  readonly perPage: number
}

const goInteger = (value: string | null) => {
  if (value === null || !/^[+-]?\d+$/.test(value)) {
    return undefined
  }
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : undefined
}

export const parsePage = (
  request: HttpServerRequest.HttpServerRequest
): Page => {
  const query = new URL(request.url, "http://localhost").searchParams
  const pageInput = goInteger(query.get("page"))
  const perPageInput = goInteger(query.get("per_page"))
  const page = pageInput !== undefined && pageInput >= 1 ? pageInput : 1
  const perPage = perPageInput === undefined || perPageInput < 1
    ? 50
    : Math.min(perPageInput, 200)
  return { page, perPage }
}

export const paginate = <A>(
  values: ReadonlyArray<A>,
  page: Page
) => {
  const start = Math.min((page.page - 1) * page.perPage, values.length)
  const data = values.slice(start, start + page.perPage)
  return {
    data,
    meta: {
      page: page.page,
      per_page: page.perPage,
      total: values.length,
      total_pages: values.length === 0
        ? 0
        : Math.ceil(values.length / page.perPage)
    }
  }
}
