package main

import "embed"

// frontendDist holds the compiled frontend assets.
// The build step copies frontend/dist/ to backend/cmd/shorty/dist/ before go build.
//
//go:embed all:dist
var frontendDist embed.FS
