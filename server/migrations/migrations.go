// Package migrations embeds the SQL migrations so the server binary carries
// them; db.Migrate applies them on boot. It is data only: go:embed cannot
// reach files outside its own directory, so this package sits next to them.
package migrations

import "embed"

// FS holds every NNNN_name.sql file in this directory. db.Migrate applies
// them in name order, each once.
//
//go:embed *.sql
var FS embed.FS
