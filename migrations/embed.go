// Package migrations embeds the SQL migrations so the binary runs from any directory.
package migrations

import "embed"

// FS holds every *.sql migration, applied in file-name order.
//
//go:embed *.sql
var FS embed.FS
