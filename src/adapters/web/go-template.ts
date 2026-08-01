type TemplateValue = unknown

interface RenderScope {
  readonly root: TemplateValue
  readonly dot: TemplateValue
  readonly variables: Map<string, TemplateValue>
}

type TemplateNode =
  | { readonly kind: "text"; readonly value: string }
  | { readonly kind: "output"; readonly expression: string }
  | { readonly kind: "declare"; readonly name: string; readonly expression: string }
  | {
      readonly kind: "if"
      readonly expression: string
      readonly thenNodes: ReadonlyArray<TemplateNode>
      readonly elseNodes: ReadonlyArray<TemplateNode>
    }
  | {
      readonly kind: "range"
      readonly expression: string
      readonly indexName?: string
      readonly valueName?: string
      readonly body: ReadonlyArray<TemplateNode>
      readonly elseNodes: ReadonlyArray<TemplateNode>
    }
  | {
      readonly kind: "with"
      readonly expression: string
      readonly body: ReadonlyArray<TemplateNode>
      readonly elseNodes: ReadonlyArray<TemplateNode>
    }
  | {
      readonly kind: "template"
      readonly name: string
      readonly expression: string
    }
  | {
      readonly kind: "block"
      readonly name: string
      readonly expression: string
      readonly fallback: ReadonlyArray<TemplateNode>
    }

interface TemplateToken {
  readonly kind: "text" | "action"
  readonly value: string
}

interface ParseStop {
  readonly action: string
}

interface ParsedSequence {
  readonly nodes: ReadonlyArray<TemplateNode>
  readonly stop?: ParseStop
}

interface ExpressionGroup {
  readonly kind: "group"
  readonly items: ReadonlyArray<ExpressionPart>
}

type ExpressionPart = string | ExpressionGroup

class SafeTemplateString {
  constructor(readonly value: string) {}
}

const monthNames = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec"
]

const htmlEscape = (value: string): string =>
  value.replace(
    /[&<>'"]/g,
    (character) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        "'": "&#39;",
        '"': "&#34;"
      })[character] ?? character
  )

const stringify = (value: TemplateValue): string => {
  if (value === undefined || value === null) {
    return ""
  }
  if (value instanceof SafeTemplateString) {
    return value.value
  }
  if (typeof value === "boolean") {
    return value ? "true" : "false"
  }
  return htmlEscape(String(value))
}

const isTruthy = (value: TemplateValue): boolean => {
  if (value === undefined || value === null || value === false) {
    return false
  }
  if (typeof value === "number" || typeof value === "bigint") {
    return value !== 0
  }
  if (typeof value === "string" || Array.isArray(value)) {
    return value.length > 0
  }
  if (value instanceof Map || value instanceof Set) {
    return value.size > 0
  }
  return true
}

const asRecord = (
  value: TemplateValue
): Readonly<Record<string, TemplateValue>> | undefined =>
  typeof value === "object" && value !== null
    ? (value as Readonly<Record<string, TemplateValue>>)
    : undefined

const getField = (value: TemplateValue, field: string): TemplateValue => {
  const record = asRecord(value)
  if (record === undefined) {
    return undefined
  }

  const direct = record[field]
  if (direct !== undefined) {
    return typeof direct === "function" ? direct.bind(value) : direct
  }

  if (field === "IsAdmin") {
    return record.Role === "admin" || record.role === "admin"
  }
  if (field === "HasCapability") {
    return (capability: TemplateValue) => {
      const role = record.Role ?? record.role
      if (role === "admin") {
        return true
      }
      const capabilities = record.Capabilities ?? record.capabilities
      return (
        Array.isArray(capabilities) &&
        capabilities.includes(String(capability))
      )
    }
  }
  if (field === "Format") {
    return (layout: TemplateValue) => formatGoTime(value, String(layout))
  }

  return undefined
}

const resolvePath = (path: string, scope: RenderScope): TemplateValue => {
  if (path === "$") {
    return scope.root
  }
  if (path === ".") {
    return scope.dot
  }

  let value: TemplateValue
  let fields: ReadonlyArray<string>
  if (path.startsWith("$.")) {
    value = scope.root
    fields = path.slice(2).split(".").filter(Boolean)
  } else if (path.startsWith("$")) {
    const match = /^\$[A-Za-z_][A-Za-z0-9_]*/.exec(path)
    if (match === null) {
      return undefined
    }
    value = scope.variables.get(match[0])
    fields = path.slice(match[0].length).split(".").filter(Boolean)
  } else if (path.startsWith(".")) {
    value = scope.dot
    fields = path.slice(1).split(".").filter(Boolean)
  } else {
    return undefined
  }

  for (const field of fields) {
    value = getField(value, field)
  }
  return value
}

const tokenizeExpression = (source: string): ReadonlyArray<ExpressionPart> => {
  const root: Array<ExpressionPart> = []
  const stack: Array<Array<ExpressionPart>> = [root]
  let current = ""
  let quote = ""

  const pushCurrent = () => {
    if (current.length > 0) {
      stack.at(-1)?.push(current)
      current = ""
    }
  }

  for (let index = 0; index < source.length; index += 1) {
    const character = source[index] ?? ""
    if (quote.length > 0) {
      current += character
      if (character === "\\" && index + 1 < source.length) {
        current += source[index + 1] ?? ""
        index += 1
      } else if (character === quote) {
        quote = ""
      }
      continue
    }

    if (character === '"' || character === "'") {
      quote = character
      current += character
    } else if (character === "(") {
      pushCurrent()
      const nested: Array<ExpressionPart> = []
      stack.at(-1)?.push({ kind: "group", items: nested })
      stack.push(nested)
    } else if (character === ")") {
      pushCurrent()
      if (stack.length === 1) {
        throw new Error(`unexpected ) in template expression: ${source}`)
      }
      stack.pop()
    } else if (/\s/.test(character)) {
      pushCurrent()
    } else {
      current += character
    }
  }
  pushCurrent()

  if (quote.length > 0 || stack.length !== 1) {
    throw new Error(`unterminated template expression: ${source}`)
  }
  return root
}

const parseAtom = (atom: string, scope: RenderScope): TemplateValue => {
  if (atom.startsWith('"') || atom.startsWith("'")) {
    if (atom.startsWith('"')) {
      return JSON.parse(atom)
    }
    return atom.slice(1, -1).replaceAll("\\'", "'").replaceAll("\\\\", "\\")
  }
  if (/^-?(?:\d+\.?\d*|\.\d+)$/.test(atom)) {
    return Number(atom)
  }
  if (atom === "true") {
    return true
  }
  if (atom === "false") {
    return false
  }
  if (atom === "nil") {
    return null
  }
  if (atom.startsWith(".") || atom.startsWith("$")) {
    return resolvePath(atom, scope)
  }
  return atom
}

const formatIDR = (input: TemplateValue): string => {
  const amount = Math.trunc(Number(input))
  const absolute = Math.abs(amount)
  const formatted = String(absolute).replace(/\B(?=(\d{3})+(?!\d))/g, ".")
  return `${amount < 0 ? "-" : ""}Rp ${formatted}`
}

const formatQuantity = (input: TemplateValue): string => {
  const quantity = Math.trunc(Number(input))
  const whole = Math.trunc(quantity / 100)
  const fraction = quantity % 100
  if (fraction === 0) {
    return String(whole)
  }
  if (fraction % 10 === 0) {
    return `${whole}.${fraction / 10}`
  }
  return `${whole}.${String(fraction).padStart(2, "0")}`
}

const parseTemplateDate = (input: TemplateValue): Date | undefined => {
  if (input instanceof Date && !Number.isNaN(input.getTime())) {
    return input
  }
  const source = String(input ?? "")
  const match =
    /^(\d{4})-(\d{2})-(\d{2})(?:[ T](\d{2}):(\d{2})(?::(\d{2}))?)?/.exec(
      source
    )
  if (match === null) {
    return undefined
  }
  return new Date(
    Number(match[1]),
    Number(match[2]) - 1,
    Number(match[3]),
    Number(match[4] ?? 0),
    Number(match[5] ?? 0),
    Number(match[6] ?? 0)
  )
}

const formatGoTime = (input: TemplateValue, layout: string): string => {
  const date = parseTemplateDate(input)
  if (date === undefined) {
    return String(input ?? "")
  }
  const year = String(date.getFullYear()).padStart(4, "0")
  const month = String(date.getMonth() + 1).padStart(2, "0")
  const day = String(date.getDate()).padStart(2, "0")
  const hour = String(date.getHours()).padStart(2, "0")
  const minute = String(date.getMinutes()).padStart(2, "0")
  const second = String(date.getSeconds()).padStart(2, "0")

  if (layout === "Jan 2, 2006") {
    return `${monthNames[date.getMonth()]} ${date.getDate()}, ${year}`
  }
  if (layout === "Jan 2, 2006 15:04") {
    return `${monthNames[date.getMonth()]} ${date.getDate()}, ${year} ${hour}:${minute}`
  }
  if (layout === "2006-01-02 15:04:05") {
    return `${year}-${month}-${day} ${hour}:${minute}:${second}`
  }
  return String(input ?? "")
}

const formatDate = (input: TemplateValue): string => {
  const date = parseTemplateDate(input)
  if (date === undefined) {
    return String(input ?? "")
  }
  return `${date.getDate()} ${monthNames[date.getMonth()]} ${date.getFullYear()}`
}

const jsonForScript = (value: TemplateValue): SafeTemplateString => {
  const json = JSON.stringify(value) ?? "null"
  return new SafeTemplateString(
    json
      .replaceAll("&", "\\u0026")
      .replaceAll("<", "\\u003c")
      .replaceAll(">", "\\u003e")
      .replaceAll("\u2028", "\\u2028")
      .replaceAll("\u2029", "\\u2029")
  )
}

const evaluateCommand = (
  parts: ReadonlyArray<ExpressionPart>,
  scope: RenderScope
): TemplateValue => {
  if (parts.length === 0) {
    return undefined
  }
  const first = parts[0] as ExpressionPart
  const command =
    typeof first === "string"
      ? parseAtom(first, scope)
      : evaluateCommand(first.items, scope)
  const args = parts.slice(1).map((part) =>
    typeof part === "string"
      ? parseAtom(part, scope)
      : evaluateCommand(part.items, scope)
  )

  if (typeof first === "string") {
    switch (first) {
      case "eq":
        return args.length > 1 && args.slice(1).every((value) => value === args[0])
      case "ne":
        return args.length === 2 && args[0] !== args[1]
      case "gt":
        return Number(args[0]) > Number(args[1])
      case "ge":
        return Number(args[0]) >= Number(args[1])
      case "lt":
        return Number(args[0]) < Number(args[1])
      case "le":
        return Number(args[0]) <= Number(args[1])
      case "not":
        return !isTruthy(args[0])
      case "and": {
        let result: TemplateValue
        for (const value of args) {
          result = value
          if (!isTruthy(value)) {
            return value
          }
        }
        return result
      }
      case "or":
        return args.find(isTruthy) ?? args.at(-1)
      case "index": {
        let value = args[0]
        for (const key of args.slice(1)) {
          if (Array.isArray(value)) {
            value = value[Number(key)]
          } else {
            value = getField(value, String(key))
          }
        }
        return value
      }
      case "dict": {
        const result: Record<string, TemplateValue> = {}
        for (let index = 0; index + 1 < args.length; index += 2) {
          result[String(args[index])] = args[index + 1]
        }
        return result
      }
      case "add":
        return Number(args[0]) + Number(args[1])
      case "sub":
        return Number(args[0]) - Number(args[1])
      case "formatIDR":
        return formatIDR(args[0])
      case "formatQty":
        return formatQuantity(args[0])
      case "formatDate":
        return formatDate(args[0])
      case "isNegative":
        return args[0] !== undefined && args[0] !== null && Number(args[0]) < 0
      case "toJSON":
        return jsonForScript(args[0])
      case "hasString":
        return Array.isArray(args[1]) && args[1].includes(args[0])
      case "seq":
        return Array.from({ length: Number(args[0]) }, (_, index) => index + 1)
    }
  }

  return typeof command === "function" ? command(...args) : command
}

const evaluate = (expression: string, scope: RenderScope): TemplateValue =>
  evaluateCommand(tokenizeExpression(expression), scope)

const tokenizeTemplate = (source: string): ReadonlyArray<TemplateToken> => {
  const tokens: Array<TemplateToken> = []
  let offset = 0
  while (offset < source.length) {
    const start = source.indexOf("{{", offset)
    if (start < 0) {
      tokens.push({ kind: "text", value: source.slice(offset) })
      break
    }
    if (start > offset) {
      tokens.push({ kind: "text", value: source.slice(offset, start) })
    }
    const end = source.indexOf("}}", start + 2)
    if (end < 0) {
      throw new Error("unterminated template action")
    }
    tokens.push({
      kind: "action",
      value: source.slice(start + 2, end).trim()
    })
    offset = end + 2
  }
  return tokens
}

class TemplateParser {
  private index = 0
  readonly definitions = new Map<string, ReadonlyArray<TemplateNode>>()

  constructor(private readonly tokens: ReadonlyArray<TemplateToken>) {}

  parse(): void {
    this.parseSequence()
  }

  private parseSequence(stops = false): ParsedSequence {
    const nodes: Array<TemplateNode> = []
    while (this.index < this.tokens.length) {
      const token = this.tokens[this.index]
      this.index += 1
      if (token === undefined) {
        break
      }
      if (token.kind === "text") {
        nodes.push({ kind: "text", value: token.value })
        continue
      }

      const action = token.value
      if (action === "end" || action === "else" || action.startsWith("else if ")) {
        if (!stops) {
          throw new Error(`unexpected {{${action}}}`)
        }
        return { nodes, stop: { action } }
      }
      if (action.startsWith("/*") && action.endsWith("*/")) {
        continue
      }
      if (action.startsWith("define ")) {
        const [name] = parseInvocation(action.slice("define ".length))
        const parsed = this.parseSequence(true)
        if (parsed.stop?.action !== "end") {
          throw new Error(`template ${name} is missing {{end}}`)
        }
        this.definitions.set(name, parsed.nodes)
        continue
      }
      if (action.startsWith("if ")) {
        nodes.push(this.parseIf(action.slice(3)))
        continue
      }
      if (action.startsWith("range ")) {
        nodes.push(this.parseRange(action.slice(6)))
        continue
      }
      if (action.startsWith("with ")) {
        const parsed = this.parseSequence(true)
        let elseNodes: ReadonlyArray<TemplateNode> = []
        if (parsed.stop?.action === "else") {
          const alternate = this.parseSequence(true)
          if (alternate.stop?.action !== "end") {
            throw new Error("with block is missing {{end}}")
          }
          elseNodes = alternate.nodes
        } else if (parsed.stop?.action !== "end") {
          throw new Error("with block is missing {{end}}")
        }
        nodes.push({
          kind: "with",
          expression: action.slice(5),
          body: parsed.nodes,
          elseNodes
        })
        continue
      }
      if (action.startsWith("template ")) {
        const [name, expression] = parseInvocation(action.slice(9))
        nodes.push({ kind: "template", name, expression })
        continue
      }
      if (action.startsWith("block ")) {
        const [name, expression] = parseInvocation(action.slice(6))
        const parsed = this.parseSequence(true)
        if (parsed.stop?.action !== "end") {
          throw new Error(`block ${name} is missing {{end}}`)
        }
        nodes.push({
          kind: "block",
          name,
          expression,
          fallback: parsed.nodes
        })
        continue
      }

      const declaration = /^(\$[A-Za-z_][A-Za-z0-9_]*)\s*:=\s*(.+)$/.exec(action)
      if (declaration !== null) {
        nodes.push({
          kind: "declare",
          name: declaration[1] ?? "",
          expression: declaration[2] ?? ""
        })
      } else {
        nodes.push({ kind: "output", expression: action })
      }
    }
    return { nodes }
  }

  private parseIf(expression: string): TemplateNode {
    const parsed = this.parseSequence(true)
    let elseNodes: ReadonlyArray<TemplateNode> = []
    if (parsed.stop?.action === "else") {
      const alternate = this.parseSequence(true)
      if (alternate.stop?.action !== "end") {
        throw new Error("if block is missing {{end}}")
      }
      elseNodes = alternate.nodes
    } else if (parsed.stop?.action.startsWith("else if ") === true) {
      elseNodes = [this.parseIf(parsed.stop.action.slice("else if ".length))]
    } else if (parsed.stop?.action !== "end") {
      throw new Error("if block is missing {{end}}")
    }
    return {
      kind: "if",
      expression,
      thenNodes: parsed.nodes,
      elseNodes
    }
  }

  private parseRange(source: string): TemplateNode {
    const assignment =
      /^(?:(\$[A-Za-z_][A-Za-z0-9_]*)(?:\s*,\s*(\$[A-Za-z_][A-Za-z0-9_]*))?\s*:=\s*)?(.+)$/.exec(
        source
      )
    if (assignment === null) {
      throw new Error(`invalid range: ${source}`)
    }
    const parsed = this.parseSequence(true)
    let elseNodes: ReadonlyArray<TemplateNode> = []
    if (parsed.stop?.action === "else") {
      const alternate = this.parseSequence(true)
      if (alternate.stop?.action !== "end") {
        throw new Error("range block is missing {{end}}")
      }
      elseNodes = alternate.nodes
    } else if (parsed.stop?.action !== "end") {
      throw new Error("range block is missing {{end}}")
    }

    const firstName = assignment[1]
    const secondName = assignment[2]
    const node: {
      kind: "range"
      expression: string
      indexName?: string
      valueName?: string
      body: ReadonlyArray<TemplateNode>
      elseNodes: ReadonlyArray<TemplateNode>
    } = {
      kind: "range",
      expression: assignment[3] ?? "",
      body: parsed.nodes,
      elseNodes
    }
    if (secondName !== undefined) {
      if (firstName !== undefined) {
        node.indexName = firstName
      }
      node.valueName = secondName
    } else if (firstName !== undefined) {
      node.valueName = firstName
    }
    return node
  }
}

const parseInvocation = (source: string): readonly [string, string] => {
  const match = /^"([^"]+)"(?:\s+(.+))?$/.exec(source)
  if (match === null) {
    throw new Error(`invalid template invocation: ${source}`)
  }
  return [match[1] ?? "", match[2] ?? "."]
}

const cloneScope = (
  scope: RenderScope,
  dot: TemplateValue = scope.dot
): RenderScope => ({
  root: scope.root,
  dot,
  variables: new Map(scope.variables)
})

const rangeEntries = (
  value: TemplateValue
): ReadonlyArray<readonly [TemplateValue, TemplateValue]> => {
  if (Array.isArray(value)) {
    return value.map((item, index) => [index, item] as const)
  }
  if (value instanceof Map) {
    return [...value.entries()]
  }
  const record = asRecord(value)
  return record === undefined
    ? []
    : Object.keys(record)
        .sort()
        .map((key) => [key, record[key]] as const)
}

const renderNodes = (
  nodes: ReadonlyArray<TemplateNode>,
  scope: RenderScope,
  definitions: ReadonlyMap<string, ReadonlyArray<TemplateNode>>
): string => {
  let output = ""
  for (const node of nodes) {
    switch (node.kind) {
      case "text":
        output += node.value
        break
      case "output":
        output += stringify(evaluate(node.expression, scope))
        break
      case "declare":
        scope.variables.set(node.name, evaluate(node.expression, scope))
        break
      case "if":
        output += renderNodes(
          isTruthy(evaluate(node.expression, scope))
            ? node.thenNodes
            : node.elseNodes,
          cloneScope(scope),
          definitions
        )
        break
      case "with": {
        const value = evaluate(node.expression, scope)
        output += renderNodes(
          isTruthy(value) ? node.body : node.elseNodes,
          cloneScope(scope, isTruthy(value) ? value : scope.dot),
          definitions
        )
        break
      }
      case "range": {
        const entries = rangeEntries(evaluate(node.expression, scope))
        if (entries.length === 0) {
          output += renderNodes(node.elseNodes, cloneScope(scope), definitions)
          break
        }
        for (const [index, value] of entries) {
          const child = cloneScope(scope, value)
          if (node.indexName !== undefined) {
            child.variables.set(node.indexName, index)
          }
          if (node.valueName !== undefined) {
            child.variables.set(node.valueName, value)
          }
          output += renderNodes(node.body, child, definitions)
        }
        break
      }
      case "template": {
        const template = definitions.get(node.name)
        if (template === undefined) {
          throw new Error(`template ${node.name} is not defined`)
        }
        const data = evaluate(node.expression, scope)
        output += renderNodes(
          template,
          { root: data, dot: data, variables: new Map() },
          definitions
        )
        break
      }
      case "block": {
        const template = definitions.get(node.name) ?? node.fallback
        const data = evaluate(node.expression, scope)
        output += renderNodes(
          template,
          { root: data, dot: data, variables: new Map() },
          definitions
        )
        break
      }
    }
  }
  return output
}

export class GoTemplateSet {
  private readonly definitions: ReadonlyMap<
    string,
    ReadonlyArray<TemplateNode>
  >

  constructor(sources: ReadonlyArray<string>) {
    const definitions = new Map<string, ReadonlyArray<TemplateNode>>()
    for (const source of sources) {
      const parser = new TemplateParser(tokenizeTemplate(source))
      parser.parse()
      for (const [name, nodes] of parser.definitions) {
        definitions.set(name, nodes)
      }
    }
    this.definitions = definitions
  }

  render(name: string, data: TemplateValue): string {
    const template = this.definitions.get(name)
    if (template === undefined) {
      throw new Error(`template ${name} is not defined`)
    }
    return renderNodes(
      template,
      { root: data, dot: data, variables: new Map() },
      this.definitions
    )
  }
}
