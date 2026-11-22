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

//go:embed bay-chart.svg
var BayChart []byte
