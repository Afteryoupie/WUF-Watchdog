# WUF Watchdog · Desktop identity

WUF Watchdog uses warm paper (#F5F3EC), ink (#242620), and vermilion (#B43D1D). The dashboard combines an editorial heading with a compact status strip, direct engine controls, model selection, and connection/energy settings. System fonts and inline SVG keep it usable offline without frontend dependencies.

The minimal Border Collie identity comes from `../assets/watchdog-{sleeping,ready,running}.svg`. The dashboard embeds those existing paths and uses the ready expression as its favicon. Sleeping/disconnected folds the ears; ready/loading raises them; active inference opens the mouth. Text distinguishes states independently of the artwork and color. Command icons use one outline style, with separate play, moon, refresh, copy, and exit symbols.

Layout adapts at 1100px and 640px. On small screens the current model spans the status strip, the remaining metrics share a row, and settings stack under the model list. Long model identifiers wrap. Buttons retain 44px targets, keyboard focus indicators, and pressed feedback. Reduced motion removes entrance transitions and mascot rotation.

## Sources

- `../templates/dashboard.html`: self-contained interface, state rendering, and API integration.
- `../WUFMenuBar.swift`: native macOS menu and template artwork.
- `../assets/watchdog-vectors.json`: shared icon geometry, also packaged for the native macOS template.
- `../../watchdog-go/internal/tooling/icons.go`: icon generation.
- `../../windows/build-wuf.ps1`: Windows package build.

See [WINDOWS.md](WINDOWS.md) for native packaging and [UI-REVIEW-20261024.md](../../docs/UI-REVIEW-20261024.md) for the current validation and limitations.

## Quality references

These guide composition, usability, and distinctive interaction; they do not constitute an award or certification.

- [Awwwards mobile excellence](https://www.awwwards.com/mobile-excellence-guidelines.pdf)
- [Webby judging criteria](https://www.webbyawards.com/judging-criteria/)
- [FWA project gallery](https://thefwa.com/)
