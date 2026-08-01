import { mkdir } from "node:fs/promises"

const linux = process.argv.includes("--linux")
const version = process.env.VERSION ?? "dev"

await mkdir("dist", { recursive: true })

const result = await Bun.build({
  entrypoints: ["src/main.ts"],
  compile: {
    outfile: "dist/latasya-erp",
    ...(linux ? { target: "bun-linux-x64-baseline" as const } : {})
  },
  define: {
    LATASYA_BUILD_VERSION: JSON.stringify(version)
  },
  minify: true,
  sourcemap: "linked",
  bytecode: true
})

if (!result.success) {
  for (const log of result.logs) {
    console.error(log)
  }
  process.exit(1)
}
