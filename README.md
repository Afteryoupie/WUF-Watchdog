# WUF Watchdog

Go service, native browser host, tray application and management tools.
The browser extension is the independent `../WUF-Translate` project.

The dashboard, tray, native menus, app display names and launcher messages use
**WUF Watchdog**. See [UI validation](docs/UI-REVIEW-20261024.md) for the current
design checks and intentionally preserved compatibility identifiers.

## Layout

- `watchdog-go/`: Go module, commands, tests and portable test fixtures.
- `watchdog/`: dashboard, icon sources, Swift tray source, and the three retained
  Laya inference adapter Python files. Management and tools run in Go.
- `windows/`: package builder and installer.
- `runtime/windows/`: locally deployed Go executables and machine launch settings.
- `data/`: model selection, logs and runtime state.

Runtime executables, machine configuration, data, logs and virtual environments
are excluded from Git. Models and llama.cpp remain external dependencies.

## Build and test

```powershell
go -C watchdog-go test ./...
go -C watchdog-go vet ./...
& ./windows/build-wuf.ps1 -GoExecutable 'C:/path/to/go.exe' -OutputDirectory 'C:/new/package'
```

The Windows registry tests create and clean isolated GUID test keys; run under
the intended Windows user. The Go toolchain must be available for development.

## Local operation

```powershell
& ./runtime/windows/wuf-tool.exe control --action start
& ./runtime/windows/wuf-tool.exe control --action stop
& ./runtime/windows/wuf-tool.exe control --action startup-menu
```

Human-facing shortcuts are in `C:/LLM_Project`. Automation invokes the executables
directly. `runtime/windows/launch.json` specifies this machine's model directory,
llama.cpp executable, Laya Python interpreter and this project's `data` directory.

Laya inference retains the existing isolated Python environment at
`C:/LLM_Project/translation_tools/shinkansen-custom/watchdog/.venv`.
That interpreter is an external runtime dependency, not imported management code.
For another machine create an isolated environment using
`watchdog/requirements-laya.txt` (or the platform-specific ROCm variant) and set
`--laya-python`, model and llama.cpp paths in launch.json. Do not commit the local
runtime configuration or virtual environment.

The original repositories under `translation_tools` retain their Git history and
uncommitted work. This project has its own independent Git history and remote.
