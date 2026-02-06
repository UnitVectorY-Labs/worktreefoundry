package app

import "embed"

//go:embed templates/*.html static/*
var webAssets embed.FS
