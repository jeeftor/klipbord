// Package webassets embeds the static files served by Klipbord.
package webassets

import (
	_ "embed"

	"github.com/jeeftor/klipbord/internal/app"
)

var (
	//go:embed static/index.html
	indexHTML []byte

	//go:embed static/swagger.html
	swaggerHTML []byte

	//go:embed static/manifest.json
	manifestJSON []byte

	//go:embed static/sw.js
	serviceWorker []byte

	//go:embed static/icon.svg
	iconSVG []byte
)

// Embedded returns the static assets compiled into the Klipbord binary.
func Embedded() app.Assets {
	return app.Assets{
		IndexHTML:     indexHTML,
		SwaggerHTML:   swaggerHTML,
		ManifestJSON:  manifestJSON,
		ServiceWorker: serviceWorker,
		IconSVG:       iconSVG,
	}
}
