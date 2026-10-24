# WUF Watchdog · Windows and macOS

Windows uses the Go service and native tray. Left-click opens an icon-anchored status panel; right-click opens the model and control menu. Status, countdown, auto-sleep, API copying, and confirmed shutdown are available without a browser. The dashboard provides the full model and connection view.

Build from the repository root using an absolute Go executable:

```powershell
& ./windows/build-wuf.ps1 -GoExecutable 'C:/path/to/go.exe' -OutputDirectory 'C:/new/package'
& ./runtime/windows/wuf-tool.exe control --action start
```

The package uses native Go binaries directly. Python is needed only for isolated Laya inference; no BAT or command-file launcher is used by automation.

## macOS

The package builder includes `watchdog/WUFMenuBar.swift`, the shared dashboard, and the app icon. It sets both CFBundleName and CFBundleDisplayName to **WUF Watchdog**. Build and sign on macOS with Xcode command-line tools:

```sh
/absolute/path/to/wuf-tool package-mac --repo /absolute/path/to/WUF-Watchdog --output '/absolute/path/to/WUF Watchdog.app' --go /absolute/path/to/go --swiftc /usr/bin/swiftc
```

`--layout-only` can inspect bundle resources on Windows; it does not produce a runnable macOS application. Swift compilation, Finder behavior, and the menu must be verified on macOS.

## State icons

The shared minimal Border Collie vectors are in `watchdog/assets/watchdog-vectors.json`; the existing SVG assets supply the dashboard artwork. Go's `wuf-tool icons --vectors /absolute/path/to/watchdog-vectors.json --output /absolute/path/to/new/icons` generates platform icon assets. The dashboard keeps state text alongside the mascot. The macOS menu reads the same JSON contours from Resources; sleeping folds the ears and ready/loading raises them.

## Compatibility identifiers

Display names are WUF Watchdog. Existing `com.wuf_translate.watchdog`, `local.wuf.translate.watchdog`, `WUFTranslate` registry/data keys, `wuf-translate` health identity, Go module imports, and the legacy `shinkansen_target_language` request alias remain stable so existing extensions, startup entries, and saved settings continue to work. The independent browser extension is named WUF Translate.
