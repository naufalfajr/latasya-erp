# School calendar module

This module owns manual and Google-sourced school closures, effective billing
days, pricing multipliers, OAuth state, and the persisted Google connection.
`internal/googlecalendar` remains the external HTTP/OAuth adapter and calls the
trusted sync-storage methods exposed here.
