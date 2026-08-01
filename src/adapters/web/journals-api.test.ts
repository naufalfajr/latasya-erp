import { afterEach, describe, expect, test } from "bun:test"
import { HttpApp } from "@effect/platform"
import { SqlClient } from "@effect/sql"
import { mkdtempSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Effect } from "effect"
import { runtimeLayer } from "../../app/runtime-layer.ts"
import { seedDefaultAdmin } from "../../infrastructure/bootstrap/default-admin.ts"
import { migrateDatabase } from "../../infrastructure/migrations/migrate.ts"
import { makeRouter } from "./router.ts"

const temporaryDirectories: Array<string> = []
const disposers: Array<() => Promise<void>> = []

const setup = async () => {
  const directory = mkdtempSync(join(tmpdir(), "latasya-journals-api-"))
  temporaryDirectories.push(directory)
  const databasePath = join(directory, "latasya.db")
  await Effect.runPromise(migrateDatabase(databasePath))
  await Effect.runPromise(
    seedDefaultAdmin.pipe(Effect.provide(runtimeLayer(databasePath)))
  )
  const web = HttpApp.toWebHandlerLayer(
    makeRouter("test", true),
    runtimeLayer(databasePath)
  )
  disposers.push(web.dispose)
  const login = await web.handler(new Request(
    "http://localhost/api/v1/auth/login",
    {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ username: "admin", password: "admin" })
    }
  ))
  const accountIds = await Effect.runPromise(
    Effect.gen(function*() {
      const sql = yield* SqlClient.SqlClient
      const rows = yield* sql<{ readonly id: number }>`
        SELECT id FROM accounts ORDER BY id LIMIT 2
      `
      return [rows[0]?.id ?? 0, rows[1]?.id ?? 0] as const
    }).pipe(Effect.provide(runtimeLayer(databasePath)))
  )
  return {
    accountIds,
    databasePath,
    cookie: login.headers.get("set-cookie")?.split(";")[0] ?? "",
    handler: web.handler
  }
}

const journalBody = (
  debitAccount: number,
  creditAccount: number,
  amount = "100000",
  description = "Test entry"
) => ({
  entry_date: "2026-05-10",
  description,
  lines: [
    {
      account_id: debitAccount,
      debit: amount,
      credit: "0",
      memo: "Dr"
    },
    {
      account_id: creditAccount,
      debit: "0",
      credit: amount,
      memo: "Cr"
    }
  ]
})

const request = (
  handler: (request: Request) => Promise<Response>,
  cookie: string,
  method: string,
  path: string,
  body?: unknown,
  idempotencyKey?: string
) => handler(new Request(`http://localhost${path}`, {
  method,
  headers: {
    cookie,
    ...(body === undefined ? {} : { "content-type": "application/json" }),
    ...(idempotencyKey === undefined
      ? {}
      : { "idempotency-key": idempotencyKey })
  },
  ...(body === undefined ? {} : { body: JSON.stringify(body) })
}))

afterEach(async () => {
  await Promise.all(disposers.splice(0).map((dispose) => dispose()))
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true })
  }
})

describe("journals API", () => {
  test("creates, paginates, gets, updates, audits, and deletes", async () => {
    const {
      accountIds: [debitAccount, creditAccount],
      databasePath,
      cookie,
      handler
    } = await setup()
    const first = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/journals",
      journalBody(debitAccount, creditAccount)
    )
    await request(
      handler,
      cookie,
      "POST",
      "/api/v1/journals",
      journalBody(
        debitAccount,
        creditAccount,
        "200000",
        "Second entry"
      )
    )
    const createdBody = await first.json() as {
      readonly data: {
        readonly id: number
        readonly reference: string
        readonly lines: ReadonlyArray<{ readonly debit: string }>
      }
    }
    const id = createdBody.data.id
    const list = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/journals?source=manual&search=entry&per_page=1&page=2"
    )
    const listBody = await list.json() as {
      readonly data: ReadonlyArray<{
        readonly lines: null
        readonly total_debit: string
      }>
      readonly meta: {
        readonly total: number
        readonly total_pages: number
      }
    }
    const get = await request(
      handler,
      cookie,
      "GET",
      `/api/v1/journals/${id}`
    )
    const updated = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/journals/${id}`,
      {
        ...journalBody(
          debitAccount,
          creditAccount,
          "300000",
          "Updated entry"
        ),
        entry_date: "2026-05-11"
      }
    )
    const removed = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/journals/${id}`
    )
    const auditActions = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly action: string }>`
          SELECT action
          FROM audit_log
          WHERE target_type = 'journal_entry' AND target_id = ${id}
          ORDER BY id
        `
        return rows.map((row) => row.action)
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(first.status).toBe(201)
    expect(createdBody.data.reference).toMatch(/^JE-\d{6}-\d{4}$/)
    expect(createdBody.data.lines[0]?.debit).toBe("100000")
    expect(list.status).toBe(200)
    expect(listBody.data).toHaveLength(1)
    expect(listBody.data[0]?.lines).toBeNull()
    expect(listBody.meta).toMatchObject({ total: 2, total_pages: 2 })
    expect(get.status).toBe(200)
    expect(updated.status).toBe(200)
    expect((await updated.json() as {
      readonly data: { readonly total_debit: string }
    }).data.total_debit).toBe("300000")
    expect(removed.status).toBe(204)
    expect(auditActions).toEqual([
      "journal.create",
      "journal.update",
      "journal.delete"
    ])
  })

  test("matches authentication, strict JSON, validation, and not-found errors", async () => {
    const {
      accountIds: [debitAccount, creditAccount],
      cookie,
      handler
    } = await setup()
    const anonymous = await request(
      handler,
      "",
      "GET",
      "/api/v1/journals"
    )
    const unbalanced = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/journals",
      {
        ...journalBody(debitAccount, creditAccount),
        lines: [
          {
            account_id: debitAccount,
            debit: "100000",
            credit: "0"
          },
          {
            account_id: creditAccount,
            debit: "0",
            credit: "50000"
          }
        ]
      }
    )
    const unknownLineField = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/journals",
      {
        ...journalBody(debitAccount, creditAccount),
        lines: [
          {
            account_id: debitAccount,
            debit: "100000",
            credit: "0",
            extra: true
          },
          {
            account_id: creditAccount,
            debit: "0",
            credit: "100000"
          }
        ]
      }
    )
    const missing = await request(
      handler,
      cookie,
      "GET",
      "/api/v1/journals/not-a-number"
    )
    const unbalancedBody = await unbalanced.json() as {
      readonly code: string
      readonly fields: Readonly<Record<string, string>>
    }

    expect(anonymous.status).toBe(401)
    expect(unbalanced.status).toBe(422)
    expect(unbalancedBody.code).toBe("validation_failed")
    expect(unbalancedBody.fields.lines).toBe("debits must equal credits")
    expect(unknownLineField.status).toBe(400)
    expect(missing.status).toBe(404)
  })

  test("replays successful creates and rejects changed idempotent requests", async () => {
    const {
      accountIds: [debitAccount, creditAccount],
      databasePath,
      cookie,
      handler
    } = await setup()
    const key = "journal-create-key"
    const body = journalBody(
      debitAccount,
      creditAccount,
      "75000",
      "Idempotent entry"
    )
    const first = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/journals",
      body,
      key
    )
    const firstText = await first.text()
    const replay = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/journals",
      body,
      key
    )
    const replayText = await replay.text()
    const conflict = await request(
      handler,
      cookie,
      "POST",
      "/api/v1/journals",
      journalBody(
        debitAccount,
        creditAccount,
        "76000",
        "Idempotent entry"
      ),
      key
    )
    const count = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        const rows = yield* sql<{ readonly count: number }>`
          SELECT COUNT(*) AS count
          FROM journal_entries
          WHERE description = 'Idempotent entry'
        `
        return rows[0]?.count ?? 0
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )

    expect(first.status).toBe(201)
    expect(replay.status).toBe(201)
    expect(replayText).toBe(firstText)
    expect(conflict.status).toBe(409)
    expect((await conflict.json() as { readonly code: string }).code)
      .toBe("idempotency_conflict")
    expect(count).toBe(1)
  })

  test("protects auto-generated entries from update and delete", async () => {
    const {
      accountIds: [debitAccount, creditAccount],
      databasePath,
      cookie,
      handler
    } = await setup()
    const id = await Effect.runPromise(
      Effect.gen(function*() {
        const sql = yield* SqlClient.SqlClient
        yield* sql`
          INSERT INTO journal_entries (
            entry_date, reference, description, source_type,
            is_posted, created_by
          )
          VALUES (
            '2026-05-09', 'AUTO-1', 'Auto entry', 'income', 1, 1
          )
        `
        const ids = yield* sql<{ readonly id: number }>`
          SELECT last_insert_rowid() AS id
        `
        const entryId = ids[0]?.id ?? 0
        yield* sql`
          INSERT INTO journal_lines (
            entry_id, account_id, debit, credit, memo
          )
          VALUES
            (${entryId}, ${debitAccount}, 5000, 0, ''),
            (${entryId}, ${creditAccount}, 0, 5000, '')
        `
        return entryId
      }).pipe(Effect.provide(runtimeLayer(databasePath)))
    )
    const update = await request(
      handler,
      cookie,
      "PUT",
      `/api/v1/journals/${id}`,
      journalBody(debitAccount, creditAccount)
    )
    const remove = await request(
      handler,
      cookie,
      "DELETE",
      `/api/v1/journals/${id}`
    )

    expect(update.status).toBe(409)
    expect(await update.text()).toContain(
      "cannot edit auto-generated journal entry"
    )
    expect(remove.status).toBe(409)
    expect(await remove.text()).toContain(
      "cannot delete auto-generated journal entry (source: income)"
    )
  })
})
