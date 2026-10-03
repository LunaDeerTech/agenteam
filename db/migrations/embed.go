// Package migrations contains the immutable global SQL migration sequence.
package migrations

import "embed"

//go:embed *.sql
var SQL embed.FS
