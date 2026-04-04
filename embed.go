package latasyaerp

import "embed"

//go:embed migrations/*.sql
var MigrationFS embed.FS

//go:embed templates/*.html templates/**/*.html
var TemplateFS embed.FS

//go:embed static
var StaticFS embed.FS
