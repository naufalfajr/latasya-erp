declare const LATASYA_BUILD_VERSION: string

export const buildVersion =
  typeof LATASYA_BUILD_VERSION === "string"
    ? LATASYA_BUILD_VERSION
    : "dev"
