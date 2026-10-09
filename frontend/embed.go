// Package frontend embeds the static web user interface of the desktop application.
package frontend

import "embed"

// Assets holds index.html, the scripts, the style sheet and the logo.
//
//go:embed index.html style.css core.js pages.js app.js logo.svg
var Assets embed.FS
