# WUF Watchdog｜讓電腦上的 AI 隨用隨醒

WUF Watchdog 幫你啟動、切換及暫停電腦上的 AI 模型，並提供繁體中文控制台。搭配 [WUF Translate](https://github.com/Afteryoupie/WUF-Translate)，就能讓瀏覽器把網頁或 YouTube 字幕送到自己的電腦翻譯。

不需要先學會 LLM 的原理。照著下面的步驟，先完成「安裝 → 載入一個模型 → 測試翻譯」，之後再更換模型或開啟進階功能。

> **目前安裝狀態（2026-10-25）：尚未提供可直接下載的 WUF Watchdog 安裝包。**
> 目前需要依本頁步驟，將原始碼建置成可執行的程式。這個步驟只需在安裝或更新時進行，日常使用可以直接開啟應用程式。若不熟悉終端機，可以請人協助完成第一次安裝。
>
> 未來的安裝包會放在 [Releases 發布頁](https://github.com/Afteryoupie/WUF-Watchdog/releases)。GitHub 的 **Code → Download ZIP** 是原始碼，不是安裝包；只下載並解壓縮，還不能直接使用。

## 先了解要安裝哪些東西

| 名稱 | 白話說明 | 是否需要 |
| --- | --- | --- |
| WUF Watchdog | 管理 AI 的程式，負責啟動、切換與閒置休眠 | 需要 |
| 模型檔 | AI 學到的內容；本指南使用副檔名為 `.gguf` 的檔案 | 至少一個 |
| llama.cpp 的 `llama-server` | 讀取模型、實際產生文字的工具 | 本指南需要 |
| Go | 把本專案的原始碼轉成應用程式的工具 | 自行建置時需要 |
| WUF Translate | 瀏覽器擴充功能，負責讀取原文、顯示譯文 | 要翻譯網頁或字幕時需要另外安裝 |
| Laya | 協助字幕情境判斷的另一個模型 | 選用，第一次安裝先跳過 |

**安裝 Watchdog 不會一併下載模型，也不會安裝瀏覽器擴充功能。** 使用本指南的基本功能不需要 Python、Node.js、ChatGPT 帳號或 API 金鑰。

目前 Windows 建置腳本產生 **x64** 套件，適用於 Intel／AMD 的 64 位元 Windows；Windows ARM 電腦不在這份入門流程的範圍內。macOS 請在要使用的 Mac 上建置，產生原生 `.app`。Linux 暫不提供本頁的桌面安裝流程。

模型大小、記憶體與顯示晶片都會影響速度；模型檔的容量也不等於執行時需要的記憶體。先用下方的小模型確認流程，再嘗試較大的翻譯模型。電腦可以載入模型，不代表一定能跟上影片播放速度。

## 安裝順序

1. [下載一個測試模型](#1-下載一個測試模型)。
2. 依你的系統選擇 [Windows 安裝](#2a-windows-安裝)或 [macOS 安裝](#2b-macos-安裝)。兩段只需選一段。
3. [開啟控制台，確認模型能運作](#3-第一次啟動確認安裝成功)。
4. [連接 WUF Translate](#4-連接瀏覽器開始翻譯)。

下面的指令一次執行一個區塊，等完成後再往下進行。路徑若與你的電腦不同，請改成實際位置；路徑兩側的引號要保留。

## 1. 下載一個測試模型

先使用 Qwen 官方提供的 [Qwen3-0.6B-GGUF 檔案頁](https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/tree/main)。在 **Files** 分頁找到 **`Qwen3-0.6B-Q8_0.gguf`**，按檔案旁的下載按鈕，等待下載完成。

這個檔案約 639 MB，用來確認模型可以載入及回應；它是小型通用模型，翻譯品質需要另外評估，並非本專案的翻譯品質保證。模型的授權與使用說明請見同頁的 Model card。

建立一個固定的模型資料夾，將下載的 `.gguf` 檔直接放進去：

- **Windows**：本指南使用 `C:\WUF\Models`。
- **macOS**：在你的個人資料夾中建立 `LLM`，再在裡面建立 `Models`，也就是 `~/LLM/Models`。

例如 Windows 完成後應該是：

```text
C:\WUF\Models\Qwen3-0.6B-Q8_0.gguf
```

請確認檔案已完整下載，不是瀏覽器尚未完成的暫存檔。第一次先放一個模型即可；不要把 ZIP 壓縮檔、模型介紹網頁或 `mmproj` 檔當作模型本體。

## 2A. Windows 安裝

以下使用 **PowerShell**。在開始選單搜尋「PowerShell」並開啟，一般使用者身分即可；不需要為了本指南以系統管理員身分執行。

### A. 準備 Go、原始碼與模型執行工具

1. 到 [Go 官方下載頁](https://go.dev/dl/)下載 Windows x64 的 `.msi` 安裝程式，依畫面完成安裝。本專案要求 Go **1.24 以上版本**，可使用官方目前提供的穩定版。官方安裝說明見 [Download and install](https://go.dev/doc/install)。
2. 在本 GitHub 專案頁按 **Code → Download ZIP**，解壓縮後，把直接包含 `README.md`、`windows`、`watchdog-go` 的資料夾放到 **`C:\WUF\WUF-Watchdog`**。避免多包一層 `WUF-Watchdog-main`。
3. 到 [llama.cpp 官方 Releases](https://github.com/ggml-org/llama.cpp/releases)，展開有程式附件的版本之 **Assets**，找名稱包含 **`bin-win-cpu-x64.zip`** 的附件。此處先使用 CPU 版確認流程；不要下載 Source code 或只有 `cudart` 的附件。若最新版本只有標記檔，往下找有二進位套件的版本。
4. 將 llama.cpp 的 ZIP 完整解壓縮至 **`C:\WUF\llama.cpp`**，確認其中有 **`llama-server.exe`**。保留同包的 DLL 等檔案，不要只抽出 EXE。

完成後主要檔案應該在：

```text
C:\WUF\
├─ WUF-Watchdog\
│  ├─ README.md
│  ├─ watchdog-go\
│  └─ windows\
├─ llama.cpp\
│  └─ llama-server.exe  （及同包其他檔案）
└─ Models\
   └─ Qwen3-0.6B-Q8_0.gguf
```

如果解壓縮後 `llama-server.exe` 在更深一層，請在後面的設定改填該 EXE 的實際完整路徑。

### B. 建立 WUF Watchdog 程式

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

### C. 告訴 Watchdog 檔案放在哪裡

用記事本開啟 **`C:\WUF\Watchdog\launch.json`**，將內容替換成下方設定，再儲存。請確認檔名仍為 `launch.json`，沒有變成 `launch.json.txt`。

```json
{
  "core": "watchdog.exe",
  "tray": "tray.exe",
  "base_url": "http://127.0.0.1:8080",
  "arguments": [
    "--dashboard", "resources/dashboard.html",
    "--laya-root", "resources/laya",
    "--models-dir", "C:/WUF/Models",
    "--llama", "C:/WUF/llama.cpp/llama-server.exe"
  ],
  "tray_arguments": [
    "--config", "launch.json",
    "--assets", "resources/icons"
  ]
}
```

這裡使用 `/` 是為了方便填寫 JSON 路徑；Windows 可以讀取。`--models-dir` 後面填**模型資料夾**，`--llama` 後面填 **EXE 檔案本身**。先不要修改其他欄位。

### D. 啟動並安裝瀏覽器連接功能

1. 在檔案總管開啟 `C:\WUF\Watchdog`，雙擊 **`app.exe`**。它會在背景啟動，不一定出現一般程式視窗。
2. 用 Chrome 或 Edge 開啟 **[http://127.0.0.1:8080](http://127.0.0.1:8080)**，依第 3 節確認模型能運作。
3. 若要讓 WUF Translate 能從瀏覽器喚醒 Watchdog，在 PowerShell 執行：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File 'C:\WUF\Watchdog\install-wuf.ps1' -Action Install
```

看到 **`Native Host installed for current-user Chrome and Edge`** 就完成瀏覽器連接功能的註冊。請關閉並重新開啟瀏覽器。這一步只處理目前 Windows 使用者的 Chrome／Edge 連接，不會安裝擴充功能、模型或啟用登入自動啟動。

## 2B. macOS 安裝

以下使用 macOS 的「終端機」：可在 Spotlight 搜尋「終端機」開啟。日常使用的成品是 **WUF Watchdog.app**。

### A. 準備工具與原始碼

1. 到 [Go 官方下載頁](https://go.dev/dl/)下載 macOS 的 `.pkg` 安裝程式。Apple M 系列選 **arm64**，Intel Mac 選 **amd64**；可從蘋果選單 →「關於這台 Mac」確認晶片。本專案要求 Go **1.24 以上版本**。安裝完成後重新開啟終端機。
2. 執行下方指令安裝 Apple 命令列開發工具，依畫面完成；若系統表示已安裝，直接繼續。

```sh
/usr/bin/xcode-select --install
```

3. 在本 GitHub 頁按 **Code → Download ZIP**，解壓縮後，將直接包含 `README.md`、`watchdog-go` 的資料夾放到個人資料夾的 `LLM` 之下，命名為 **`WUF-Watchdog`**，也就是 `~/LLM/WUF-Watchdog`。
4. 到 [llama.cpp 官方 Releases](https://github.com/ggml-org/llama.cpp/releases)，選擇有程式附件的版本：Apple M 系列下載名稱包含 **`bin-macos-arm64.tar.gz`** 的附件，Intel 下載 **`bin-macos-x64.tar.gz`**。不要選 Source code。解壓縮後保留完整資料夾，放到 `~/LLM/llama.cpp`。
5. 找出裡面的 **`llama-server`**。用 Finder 搜尋也可以；選中檔案後按 **Option + Command + C** 複製完整路徑，後面設定會用到。它可能在 `bin` 子資料夾中。

確認 Go 與 Swift 編譯工具能執行：

```sh
/usr/local/go/bin/go version
/usr/bin/swiftc --version
```

若 Go 安裝在其他位置，後續指令中的 `/usr/local/go/bin/go` 都要換成實際完整路徑。llama.cpp 也可依其[官方安裝說明](https://github.com/ggml-org/llama.cpp/blob/master/docs/install.md)，由已安裝的 Homebrew 執行 `brew install llama.cpp`；完成後用 `command -v llama-server` 查出路徑。

### B. 建立原生應用程式

在終端機執行以下指令，將程式建立在個人的「應用程式」資料夾：

```sh
/usr/local/go/bin/go -C "$HOME/LLM/WUF-Watchdog/watchdog-go" run ./cmd/wuf-tool package-mac \
  --repo "$HOME/LLM/WUF-Watchdog" \
  --output "$HOME/Applications/WUF Watchdog.app" \
  --go /usr/local/go/bin/go \
  --swiftc /usr/bin/swiftc
```

整段一起貼上，行尾的 `\` 表示同一條指令繼續到下一行。首次建置可能需要下載 Go 工具鏈；結束後應出現 `.app` 的路徑。輸出位置不能已存在，失敗後再次建置請先將該次產物移到別處。

### C. 設定模型與執行工具位置

1. 在 Finder 前往個人資料夾的 `Applications`，對 **WUF Watchdog.app** 按右鍵 →「顯示套件內容」。
2. 開啟 `Contents` → `MacOS`，找到 **`launch.json`**，用純文字編輯器開啟。
3. 保留原本欄位，在 `arguments` 陣列最後加入 `--models-dir` 與 `--llama`。完整範例如下。

**範例中的 `你的帳號` 必須換成真正的 macOS 帳號資料夾名稱；`llama-server` 必須換成上一步找到的實際路徑。** JSON 內不能使用 `~` 或 `$HOME` 代替個人資料夾。

```json
{
  "core": "watchdog",
  "tray": "tray",
  "base_url": "http://127.0.0.1:8080",
  "arguments": [
    "--dashboard", "../Resources/dashboard.html",
    "--laya-root", "../Resources/laya",
    "--models-dir", "/Users/你的帳號/LLM/Models",
    "--llama", "/Users/你的帳號/LLM/llama.cpp/bin/llama-server"
  ],
  "tray_arguments": [
    "--config", "launch.json",
    "--swift", "../Resources/WUFMenuBar"
  ]
}
```

請儲存為純文字 `launch.json`，不要使用富文字格式。由於設定放在 `.app` 裡，更新應用程式前記得另外備份。

### D. 啟動並安裝瀏覽器連接功能

1. 在 Finder 雙擊 **WUF Watchdog.app**，再用瀏覽器開啟 **[http://127.0.0.1:8080](http://127.0.0.1:8080)**。應用程式使用選單列圖示，不會有一般主視窗。
2. 若 macOS 阻擋你確認來源的程式，依「系統設定 → 隱私權與安全性」顯示的提示處理；不需要關閉整台電腦的安全防護。llama.cpp 的工具也可能需要個別允許執行。
3. 要讓 WUF Translate 能喚醒 Watchdog，在終端機執行：

```sh
"$HOME/Applications/WUF Watchdog.app/Contents/MacOS/wuf-tool" install-mac \
  --app "$HOME/Applications/WUF Watchdog.app" --action install
```

看到 **`Completed install`** 表示已為目前使用者的 Chrome／Edge 註冊連接功能。完成後重新開啟瀏覽器。這一步不會安裝擴充功能或開啟登入自動啟動。

## 3. 第一次啟動：確認安裝成功

在瀏覽器網址列輸入 **[http://127.0.0.1:8080](http://127.0.0.1:8080)**。`127.0.0.1` 的意思是「這台電腦」，只有程式已啟動時才會有控制台。

1. **看到 WUF Watchdog 控制台**：代表管理服務已啟動。
2. **在模型清單看到 `Qwen3-0.6B-Q8_0`**：代表模型資料夾設定正確，已找到模型檔。
3. **按該模型**：選取會直接開始載入；若已選定且處於休眠，按「啟動引擎」。首次載入請稍候。
4. **狀態變成「準備就緒」**：代表模型執行工具已啟動並通過就緒檢查。接著用下一節的「測試模型運作」確認能產生文字。

「看得到控制台」和「模型可以翻譯」是兩個階段。清單空白時先處理路徑，不必重裝瀏覽器擴充功能。

預設閒置約 **3 分鐘**會卸載模型，狀態變成「休眠待命」。這是正常省資源行為；Watchdog 還在，下次請求會再載入模型。若按「退出 WUF Watchdog」，才會關閉整個服務，之後需重新啟動應用程式或透過已註冊的擴充功能喚醒。

## 4. 連接瀏覽器，開始翻譯

依 [WUF Translate 的安裝指南](https://github.com/Afteryoupie/WUF-Translate#readme)另外安裝擴充功能，完成後開啟 WUF 設定。

1. 到「本機自訂模型」，填入下表內容。
2. 按「重新讀取模型清單」，選擇剛才載入的模型；也可以選「回到服務預設模型」。
3. 按 **「測試模型運作」**。成功後，代表瀏覽器到本機模型的完整連線已打通。
4. 在「網頁翻譯」將翻譯引擎改為 **「本地模型（Watchdog／Ollama）」**，開啟一般英文文章，按 WUF 的「翻譯網頁」。
5. 若要翻譯 YouTube，另到「YouTube 字幕翻譯」設定相同引擎；第一次先將「決策模型來源」設為 **「不使用決策模型」**。開啟有英文字幕的影片，打開 CC 與 WUF 的 YouTube 字幕功能，再重新整理分頁。

| 欄位 | 本指南的填法 |
| --- | --- |
| API 網址 | `http://127.0.0.1:8080/v1`，注意最後有 `/v1` |
| API 金鑰 | 留空 |
| 模型名稱 | 從重新讀取後的清單選擇，或使用服務預設模型 |
| 專屬模型覆寫 | 先留空 |

網頁與 YouTube 的翻譯引擎是分開設定的，兩者都要確認。模型尚未載入時，第一筆翻譯需要等待喚醒；可先暫停影片，等第一批譯文出現。

## 日常使用、更新與移除

### 開啟與關閉

- **Windows**：雙擊 `C:\WUF\Watchdog\app.exe`。可自行在桌面建立它的捷徑。
- **macOS**：開啟個人「應用程式」資料夾內的 `WUF Watchdog.app`。
- **只想釋放模型記憶體**：在控制台按「立即休眠」。
- **要完全關閉**：在控制台按「退出 WUF Watchdog」並確認。只關閉瀏覽器分頁不會停止服務。

瀏覽器喚醒需已安裝 Native Host（原生瀏覽器連接功能），並使用符合註冊識別碼的 WUF Translate：`iecdpfnjngfljdlpookikhidlbfbmidb`。若你自行修改擴充功能身分，仍可手動啟動 Watchdog 後透過 API 連線，自動喚醒則需另行調整註冊。

### 登入電腦時自動啟動（選用）

一般安裝不會自動開啟這項功能。Windows 在 PowerShell 執行：

```powershell
& 'C:\WUF\Watchdog\wuf-tool.exe' control --action enable-autostart
```

macOS 在終端機執行：

```sh
"$HOME/Applications/WUF Watchdog.app/Contents/MacOS/wuf-tool" install-mac \
  --app "$HOME/Applications/WUF Watchdog.app" --action enable-autostart
```

這會設定下次登入時啟動。要取消，將上述 `enable-autostart` 改成 `disable-autostart` 再執行。

### 更換模型與更新程式

新增 GGUF 模型時，可以直接放在模型資料夾內，或為每個模型建立一個子資料夾。掃描只查看模型根目錄與下一層，不要再套多層資料夾；每個子資料夾先只放一個完整 GGUF 模型。分片模型需依模型提供者的說明處理，不要混放不同版本。

新增後重新整理控制台；若仍沒有出現，退出並重新啟動 Watchdog。模型會佔用記憶體，請先保留可用的小模型，再嘗試其他模型。

更新 Watchdog 前，先備份 **`launch.json`**，並退出服務。下載新原始碼後依相同流程建置到新的位置，再將模型與執行工具路徑填回新設定。若應用程式路徑改變，需先用舊位置執行下方解除註冊，再用新位置重新安裝瀏覽器連接功能及登入啟動設定。模型不必跟著重新下載。

### 移除

先退出 Watchdog，再解除瀏覽器連接與登入啟動註冊。Windows 執行：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File 'C:\WUF\Watchdog\install-wuf.ps1' -Action Uninstall
```

macOS 執行：

```sh
"$HOME/Applications/WUF Watchdog.app/Contents/MacOS/wuf-tool" install-mac \
  --app "$HOME/Applications/WUF Watchdog.app" --action uninstall
```

完成後才能刪除程式資料夾或 `.app`。解除註冊會保留模型、設定與紀錄；確定不需要時再自行刪除。WUF Translate 需另在瀏覽器中移除。

## 安裝遇到問題

| 情況 | 處理方式 |
| --- | --- |
| Releases 沒有 Watchdog 安裝附件 | 目前尚未發布安裝包，依本頁從原始碼建置，或請人協助；Source code ZIP 不能直接執行 |
| 找不到 Go 或 `go.exe` | 完成 Go 安裝、重新開啟終端機，確認使用的是實際完整路徑 |
| 建置顯示 `Output already exists`／`file exists` | 指定尚未存在的新輸出位置；失敗留下的半成品也會佔住原位置 |
| Windows 說缺少 DLL | 將 llama.cpp 整包解壓縮，保留 EXE 旁的 DLL；不要只複製單一 EXE |
| macOS 找不到 Swift | 完成 `xcode-select --install`，再確認 `swiftc --version` 能執行 |
| macOS 阻擋應用程式或 llama-server | 確認下載來源，查看「隱私權與安全性」的允許開啟提示 |
| 開不了 `127.0.0.1:8080` | 先開啟 `app.exe` 或 `.app`；檢查 `launch.json` 是否為合法純文字 JSON、路徑是否正確 |
| 顯示連接埠已被使用 | 可能已有 Watchdog 或其他服務使用 8080；先辨認並關閉衝突的服務，再重試。不要同時啟動另一個使用 8080 的 llama-server |
| 模型清單空白 | 確認 `--models-dir` 指向正確資料夾、檔案下載完成、格式為 GGUF，且沒有藏在多層子資料夾；重新啟動後再確認 |
| 模型出現但載入失敗 | 確認 `--llama` 指向可執行檔本身；查看 `model.log`。可能是工具版本、模型格式、記憶體不足或硬體不支援目前啟動參數 |
| 載入很慢、字幕跟不上 | 先使用小模型；Windows CPU 版可能較慢。GPU 版本需要匹配硬體、驅動及執行庫，請依 llama.cpp 官方說明安裝，再更新 `--llama` 路徑 |
| 決策區顯示未找到 Laya／未安裝環境 | 第一次安裝可以略過；WUF Translate 選「不使用決策模型」即可測試基本翻譯 |
| 手動啟動可翻譯，但瀏覽器無法喚醒 | 重新確認 Native Host 註冊、擴充功能識別碼及程式位置，完成後重開瀏覽器 |
| 註冊顯示 `Refusing`／`foreign registration` | 已有不同位置或其他版本的註冊；先用原版本解除註冊，再註冊新位置 |

查看紀錄時，可用記事本或純文字編輯器開啟：

| 系統 | 預設紀錄位置 |
| --- | --- |
| Windows | 在檔案總管網址列輸入 `%APPDATA%\WUFTranslate` |
| macOS | Finder →「前往」→「前往檔案夾」，輸入 `~/Library/Application Support/WUFTranslate` |

`launcher.log` 記錄程式啟動問題，`model.log` 記錄翻譯模型執行問題。若你自訂了資料目錄，請到自訂位置查看。回報時請附上系統、模型檔名、操作步驟與錯誤文字；分享紀錄前先移除私人路徑或內容。

## 進階功能：之後需要再設定

### Laya 決策模型

Laya 負責輔助判斷，與產生譯文的模型分開執行。它需要另外下載完整的 Laya 模型目錄，並準備獨立 Python 環境；一般 GGUF 模型不能替代它。

專案保留的環境清單為 [`watchdog/requirements-laya.txt`](watchdog/requirements-laya.txt)；Windows AMD 的環境清單為 [`watchdog/requirements-laya-rocm.txt`](watchdog/requirements-laya-rocm.txt)。這些固定版本及硬體環境需另行確認，不屬於本頁基本安裝的必要步驟。

準備好後，在 `launch.json` 的 `arguments` 加入 `--laya-python` 與 `--laya-model`，分別填入**該虛擬環境 Python 的完整路徑**及**完整模型資料夾路徑**。保留原有 `--laya-root`。重新啟動後，在控制台載入決策模型，確認就緒，再於 WUF Translate 選擇「本機 Laya」，端點為 `http://127.0.0.1:8080/v1/systemone`。

macOS 的 MLX、oMLX、Apple 相關後端，以及 Windows GPU 加速，均需要另外準備對應工具與相容硬體。首次安裝請先完成本指南的 GGUF 流程。

### 本機連線與資料

Watchdog 預設只接受這台電腦的連線。提供給其他應用程式的 API 網址是 `http://127.0.0.1:8080/v1`；8081、8082 為內部模型服務使用，不需填入擴充功能。不要將這些連接埠轉發到網際網路。

依本頁 GGUF 流程執行時，推論在本機的 llama.cpp 處理；下載原始碼、工具、模型與首次建置仍需要網路。若擴充功能另選 Google 翻譯或線上服務，文字將依該服務設定傳送。

## 給開發者

日常使用者可以跳過這一節。

| 目錄 | 用途 |
| --- | --- |
| `watchdog-go/` | Go 核心服務、桌面啟動器、Native Host、管理工具與測試 |
| `watchdog/` | 控制台、圖示、Swift 選單列及 Laya Python 推論介面 |
| `windows/` | Windows 建置及安裝腳本 |
| `docs/` | 介面與圖示設計紀錄 |

在專案根目錄執行測試，將 Go 換成實際完整路徑：

```sh
/usr/local/go/bin/go -C watchdog-go test ./...
/usr/local/go/bin/go -C watchdog-go vet ./...
```

Windows PowerShell 使用：

```powershell
& 'C:\Program Files\Go\bin\go.exe' -C watchdog-go test ./...
& 'C:\Program Files\Go\bin\go.exe' -C watchdog-go vet ./...
```

Windows 登錄測試使用獨立 GUID 測試鍵，應以預定的 Windows 使用者執行。背景服務及自動化直接呼叫核心執行檔；macOS 桌面成品使用原生 `.app`。

模型、推論工具、虛擬環境、個人路徑設定及執行紀錄不應提交到 Git。介面設計紀錄見 [UI review](docs/UI-REVIEW-20261024.md)，圖示設計見 [ICON-DESIGN](docs/ICON-DESIGN.md)。

## 授權

本專案程式碼採用 [Apache-2.0](LICENSE)。Go 執行階段及其他外部工具、模型各自適用其授權；建置套件會附帶相應的 Go 授權檔案，模型授權請向提供者確認。
