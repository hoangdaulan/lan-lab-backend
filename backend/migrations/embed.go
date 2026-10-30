// Package migrations embeds the goose SQL migrations so cmd/migrate can run
// them without the files being present next to the binary.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
