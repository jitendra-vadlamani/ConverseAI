package client

import "embed"

//go:embed all:dist
var StaticContent embed.FS
