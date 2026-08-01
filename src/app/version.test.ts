import { expect, test } from "bun:test"
import { buildVersion } from "./version.ts"

test("defaults the source runtime build version to dev", () => {
  expect(buildVersion).toBe("dev")
})
