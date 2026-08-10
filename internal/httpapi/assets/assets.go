// Package assets embeds third-party static files (Leaflet) so the dashboard
// map works without depending on a third-party CDN at runtime.
package assets

import _ "embed"

//go:embed leaflet.js
var LeafletJS []byte

//go:embed leaflet.css
var LeafletCSS []byte

//go:embed space-grotesk-latin.woff2
var SpaceGroteskLatin []byte

//go:embed space-grotesk-latin-ext.woff2
var SpaceGroteskLatinExt []byte
