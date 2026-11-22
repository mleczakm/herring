# Śledź brand guide

The product is **Śledź** (śledź.mleczki.pl). Herring is the name of the code
repository. The brand grows out of the illustration *Śledź na tropie*
(`internal/httpapi/assets/sledz-na-tropie.webp`): an engraved herring in an
orange beanie, studying a dotted route through a brass magnifier on a night
sea chart.

## Character and voice

Śledź is a detective who reports to you. He is calm, a little cheeky, and on
your side, never on the side of whoever watches the car.

- Write simple Polish for people who are not technical. Say what happens
  ("serwer usuwa pozycję"), not how it is built.
- Never put a period at the end of a heading, kicker, or button.
- Never name third-party services in user-facing text (SMS gateway, map tiles,
  hosting, AI models). Say what they do instead: "SMS-em", "żart".
  Naming the tracker maker (SinoTrack) is fine where users need to recognise
  their device.
- Humor is opt-in and gentle. Signature lines: "Śledź melduje", "Pośpiech
  poniża!", "Szykuj się, ktoś już jest w drodze!". Alert text never assumes
  grammatical gender, because users name their own devices.
- Privacy comes first in every message: what is stored, where, for how long,
  and who can see it.

### Claims we can make

| Claim | Basis |
| --- | --- |
| Dane w Unii Europejskiej, serwer w Finlandii | The production IP is in a Finnish data centre |
| Polska usługa | Run from Poland, Polish domain and language |
| Historia tylko na Twoim telefonie | Privacy mode, see [privacy.md](privacy.md) |
| Ty decydujesz, jak długo | Server and browser retention settings |
| Bez reklam i liczników | No ads, analytics, or third-party scripts (enforced by the CSP) |
| Otwarty kod | Public repository |

Do **not** claim servers in Poland until the hosting actually moves there.
Do not claim legal compliance (for example "zgodne z RODO"); "chronią je
europejskie przepisy" is as far as the copy goes.

## Palette

Defined as CSS custom properties in `internal/httpapi/assets/herring.css`.

| Token | Hex | Role |
| --- | --- | --- |
| `--sea` | `#0e3332` | Night sea: hero, bands, primary buttons, app icon background |
| `--sea-2` | `#164846` | Raised surfaces on `--sea`, button hover |
| `--wave` | `#2d6a66` | Wave lines and decoration on dark backgrounds |
| `--moon` | `#efe2bf` | Text and the herring on dark backgrounds |
| `--moon-soft` | `#b9c9bf` | Secondary text on dark backgrounds |
| `--signal` | `#f05c2e` | The beanie: route dots, calls to action, highlights |
| `--signal-deep` | `#c9431c` | Beanie shading |
| `--brass` | `#c99a4a` | Magnifier: frames, borders, step numbers |
| `--action` | `#b23d17` | Orange for text and links on light backgrounds |
| `--bg` | `#f6efdc` | Parchment page background |
| `--surface` | `#fffaf0` | Cards and panels |
| `--ink` | `#10292a` | Body text |
| `--ink-soft` / `--ink-faint` | `#3d5654` / `#566b69` | Secondary and tertiary text |

Contrast (WCAG): ink on parchment 13.3, `--action` on parchment 5.1, moon on
sea 10.6, white on sea 13.7, ink on signal 4.6. `--signal` on `--sea` is only
4.1, so use it there for large headings and decoration, not body text.

## Logo

`internal/httpapi/assets/herring.svg` (64 × 64): the herring in a moon-cream
colour with the orange pom-pom beanie, a brass magnifier, and the orange
dotted route running into the lens, on a rounded `--sea` square. Keep it
recognisable at 16 px: do not add detail.

Raster icons are rendered from the SVG with `rsvg-convert`:

| File | Use |
| --- | --- |
| `icon-180.png` | iOS home screen (`apple-touch-icon`) |
| `icon-192.png`, `icon-512.png` | Web app manifest, push notifications |
| `icon-maskable-512.png` | Android adaptive icon: full-bleed `--sea`, artwork scaled to 80 % to stay in the safe zone |

The wordmark is "Śledź" set in Space Grotesk Bold next to the mark.

## Typography

- Headings: Space Grotesk 500–700, self-hosted, tight letter spacing.
- Body: the system UI font.
- Kickers and small labels: the system monospace font in uppercase with wide
  letter spacing, like an annotation on a sea chart.

## Motifs

- **Dotted route**: orange dots on a line (`.trail`, the step connectors). It is
  the visual signature: the trail Śledź follows.
- **Night sea**: `--sea` with faint concentric wave rings and a moonlight glow.
- **Brass frames** around the illustration and the phone mock-up.

## Illustration

Use *Śledź na tropie* for the landing hero and, tinted with `--sea` at about
90 %, as the background of sign-in and setup. Always add alt text that
describes the scene.

## Assets and caching

Assets under `/assets/` are served as immutable for a year, and the service
worker serves them cache-first. When an asset changes, bump its `?v=` in every
URL that references it (templates, manifest, `serviceWorkerJS`) and bump
`CACHE` in `internal/httpapi/pwa.go`.
