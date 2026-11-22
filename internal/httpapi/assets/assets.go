// Package assets embeds the interface assets served with Herring.
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

//go:embed herring.css
var Styles []byte

//go:embed herring.svg
var BrandMark []byte

// PNG icons rendered from herring.svg with rsvg-convert, for PWA install
// prompts and the iOS home screen, which do not accept SVG icons.
//
//go:embed icon-180.png
var Icon180 []byte

//go:embed icon-192.png
var Icon192 []byte

//go:embed icon-512.png
var Icon512 []byte

// Full-bleed variant whose artwork stays inside the maskable safe zone, for
// launchers that crop icons to a circle or squircle.
//
//go:embed icon-maskable-512.png
var IconMaskable512 []byte
