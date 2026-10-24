# 給開發者：建置 WUF Watchdog

一般使用者應取得已建置的程式套件，不需要安裝 Go 或 Swift 編譯工具。本文件供維護者、自行修改原始碼的人及協助打包的人使用。

截至 2026-10-25，GitHub 尚未發布現成安裝包。以下保留目前可用的建置方式；完成本機建置不等於已完成公開發布、Developer ID 簽署或 Apple 公證。

執行前先下載本專案原始碼，解壓縮至固定位置，確認資料夾直接包含 `watchdog-go`、`watchdog`、`windows`。Go 版本要求為 1.24 以上，可使用 [Go 官方穩定版](https://go.dev/dl/)。建置工具只在建立／更新套件時需要，日常使用不需要保持執行。

## Windows：建立 EXE 套件

安裝 Go 的 Windows x64 MSI，將原始碼放在 `C:\WUF\WUF-Watchdog`。以下使用 PowerShell；輸出 `C:\WUF\Watchdog` 必須尚未存在。


安裝 Go 後，關閉並重新開啟 PowerShell，再執行：

```powershell
& 'C:\Program Files\Go\bin\go.exe' version
```

看到 `go version` 和版本號後，執行下方指令。若 Go 安裝到其他位置，兩處 Go 路徑都要改成實際位置。

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File 'C:\WUF\WUF-Watchdog\windows\build-wuf.ps1' -GoExecutable 'C:\Program Files\Go\bin\go.exe' -OutputDirectory 'C:\WUF\Watchdog'
```

這次執行的 `Bypass` 只套用於該 PowerShell 程序，不必變更整台電腦的執行原則。首次建置可能需要連線下載 Go 工具鏈，請等它結束。

看到 **`Built Windows amd64 package: C:\WUF\Watchdog`** 表示建置成功。輸出資料夾必須尚未存在；如果已經建置過，請改用新的輸出資料夾，並同步調整後面的路徑。

## macOS：建立原生 APP

在支援 Apple Intelligence 的 Mac 上建置。使用系統 `fm` 的預設入門流程需要 macOS 27；不必下載 GGUF、llama.cpp 或另外建立模型資料夾。

1. 安裝 Go 官方 macOS `.pkg`（Apple M 系列選 arm64）。若使用 Homebrew 等其他安裝方式，將下方 Go 路徑改成實際位置。
2. 執行 `/usr/bin/xcode-select --install`，依畫面完成命令列開發工具安裝。
3. 將原始碼放在 `~/LLM/WUF-Watchdog`，確認 Go 與 Swift 工具可用：

```sh
/usr/local/go/bin/go version
/usr/bin/swiftc --version
```

建立應用程式，輸出位置必須尚未存在：

```sh
/usr/local/go/bin/go -C "$HOME/LLM/WUF-Watchdog/watchdog-go" run ./cmd/wuf-tool package-mac \
  --repo "$HOME/LLM/WUF-Watchdog" \
  --output "$HOME/Applications/WUF Watchdog.app" \
  --go /usr/local/go/bin/go \
  --swiftc /usr/bin/swiftc
```

此命令會編譯 Go 執行檔及 Swift 選單列程式，並進行本機 ad hoc 簽署；這不等於 Developer ID 簽署及公證。輸出 `.app` 可以供本機測試，對外發布前應另處理目標架構、簽署、公證及其他電腦的實際安裝驗證。

使用 Apple 模型時，保留產生的 `launch.json` 即可，不必設定 `--models-dir` 或 `--llama`。程式會偵測 `/usr/bin/fm`，在控制台列出 Apple Foundation Models；若存在自訂 `WUF_APPLE_API`，程式會優先使用該後端，測試系統 `fm` 時需留意既有環境設定。

回到 [README 的 macOS 步驟](../README.md#2b-macos-安裝)，完成 Apple 模型可用性確認、啟動及瀏覽器連接註冊。日常啟動使用原生 `.app`，自動化直接呼叫套件內的核心執行檔。

## 發布前

- 測試全新使用者環境，不應要求使用者安裝 Go、Swift、Xcode 或下載原始碼。
- macOS 交付完整 `.app`（例如 ZIP／DMG），Windows 交付建置後完整套件；不要只提供單一核心執行檔。
- 不要包含個人 `launch.json` 路徑、私人紀錄、模型、虛擬環境或本機憑證。
- 在 GitHub Releases 清楚標示系統、架構、簽署狀態、必要的外部執行工具及實際驗證範圍，再更新 README 的下載說明。
