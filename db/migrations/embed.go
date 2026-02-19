package migrations

import "embed"

// Files contains all SQL migration files in ascending order by filename.
//
//go:embed *.sql
var Files embed.FS
