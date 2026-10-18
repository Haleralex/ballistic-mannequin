// Package migrations embeds the SQL migrations. Files are named NNN_description.sql and applied in order.
package migrations

import "embed"

// FS holds the *.sql files of this directory.
//
//go:embed *.sql
var FS embed.FS
