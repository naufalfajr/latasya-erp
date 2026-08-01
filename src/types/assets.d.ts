declare module "*.sql" {
  const source: string
  export default source
}

declare module "*.css" {
  const path: string
  export default path
}

declare module "*.js" {
  const path: string
  export default path
}
