# WUF Watchdog｜讓電腦上的 AI 隨用隨醒

WUF Watchdog 幫你啟動、切換及暫停電腦上的 AI 模型，並提供繁體中文控制台。搭配 [WUF Translate](https://github.com/Afteryoupie/WUF-Translate)，就能讓瀏覽器把網頁或 YouTube 字幕送到自己的電腦翻譯。

不需要先學會 LLM 的原理。macOS 預設使用 Apple Foundation Models，也就是 Apple 提供的本機 AI；Windows 則另外準備 GGUF 模型。先完成安裝與測試，再考慮其他模型或進階功能。

> **目前安裝狀態（2026-10-25）：尚未提供可直接下載的 WUF Watchdog 安裝包。**
> 一般使用者應下載已建置的 Windows 套件或 macOS APP，不需要自行編譯 EXE／APP。現階段公開安裝包仍待維護者提供；取得完整程式套件後，依下方流程設定。需要自行建置的人請見[開發者建置文件](docs/BUILD.md)，它不是一般使用者的安裝前置需求。
>
> 未來的安裝包會放在 [Releases 發布頁](https://github.com/Afteryoupie/WUF-Watchdog/releases)。GitHub 的 **Code → Download ZIP** 是原始碼，不是安裝包；只下載並解壓縮，還不能直接使用。

## 先了解要安裝哪些東西

| 名稱 | 白話說明 | 是否需要 |
| --- | --- | --- |
| WUF Watchdog | 管理 AI 的程式，負責啟動、切換與閒置休眠 | 需要 |
| Apple Foundation Models | macOS 提供的本機 AI，模型由 Apple 管理 | macOS 預設使用 |
| GGUF 模型檔 | 另外下載的 AI 模型檔案 | Windows 需要；macOS 選用 |
| llama.cpp 的 `llama-server` | 讀取 GGUF 模型、實際產生文字的工具 | Windows 需要；macOS 的 Apple 模式不需要 |
| Go／Swift 編譯工具 | 維護者用來製作安裝包 | 一般使用者不需要 |
| WUF Translate | 瀏覽器擴充功能，負責讀取原文、顯示譯文 | 要翻譯網頁或字幕時需要另外安裝 |
| Laya | 協助字幕情境判斷的另一個模型 | 選用，第一次安裝先跳過 |

**macOS 的 Apple 模型由系統下載及管理；Windows 的 GGUF 模型需另外準備。Watchdog 不會安裝瀏覽器擴充功能。** 使用本指南的基本功能不需要 Python、Node.js、ChatGPT 帳號或 API 金鑰。

目前 Windows 建置腳本產生 **x64** 套件，適用於 Intel／AMD 的 64 位元 Windows；Windows ARM 電腦不在這份入門流程的範圍內。macOS 的 Apple 預設流程需要支援 Apple Intelligence 的 Mac 與 macOS 27，直接使用原生 `.app`。Linux 暫不提供本頁的桌面安裝流程。

模型大小、記憶體與顯示晶片都會影響速度；模型檔的容量也不等於執行時需要的記憶體。Windows 可先用下方的小模型確認流程；macOS 先用系統 Apple 模型。電腦可以載入模型，不代表一定能跟上影片播放速度。

## 安裝順序

1. **macOS**：直接看 [macOS 安裝](#2b-macos-安裝)，先準備 Apple Intelligence，不用下載測試模型。
2. **Windows**：先[下載一個測試模型](#1-下載一個測試模型)，再看 [Windows 安裝](#2a-windows-安裝)。
3. [開啟控制台，確認模型能運作](#3-第一次啟動確認安裝成功)。
4. [連接 WUF Translate](#4-連接瀏覽器開始翻譯)。

下面的指令一次執行一個區塊，等完成後再往下進行。路徑若與你的電腦不同，請改成實際位置；路徑兩側的引號要保留。

## 1. 下載一個測試模型

**本節供 Windows 使用；macOS 使用預設 Apple Foundation Models 時直接跳到第 2B 節。**

先使用 Qwen 官方提供的 [Qwen3-0.6B-GGUF 檔案頁](https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/tree/main)。在 **Files** 分頁找到 **`Qwen3-0.6B-Q8_0.gguf`**，按檔案旁的下載按鈕，等待下載完成。

這個檔案約 639 MB，用來確認模型可以載入及回應；它是小型通用模型，翻譯品質需要另外評估，並非本專案的翻譯品質保證。模型的授權與使用說明請見同頁的 Model card。

建立一個固定的模型資料夾，將下載的 `.gguf` 檔直接放進去：

- **Windows**：本指南使用 `C:\WUF\Models`。
- **macOS 選用 GGUF 時**：在你的個人資料夾中建立 `LLM`，再在裡面建立 `Models`，也就是 `~/LLM/Models`；使用 Apple 預設模型可略過。

例如 Windows 完成後應該是：

```text
C:\WUF\Models\Qwen3-0.6B-Q8_0.gguf
```

請確認檔案已完整下載，不是瀏覽器尚未完成的暫存檔。第一次先放一個模型即可；不要把 ZIP 壓縮檔、模型介紹網頁或 `mmproj` 檔當作模型本體。

## 2A. Windows 安裝

以下使用 **PowerShell**。在開始選單搜尋「PowerShell」並開啟，一般使用者身分即可；不需要為了本指南以系統管理員身分執行。

### A. 準備程式套件與模型執行工具

1. 到 [WUF Watchdog Releases](https://github.com/Afteryoupie/WUF-Watchdog/releases)取得已建置的 Windows x64 完整套件，解壓縮至 `C:\WUF\Watchdog`。**目前尚未發布此附件；若頁面沒有套件，這一步暫時無法從公開下載完成。** 維護者可依[建置文件](docs/BUILD.md)製作套件。
2. 確認 `C:\WUF\Watchdog` 直接包含 `app.exe`、`watchdog.exe`、`tray.exe`、`native-host.exe`、`wuf-tool.exe`、`launch.json`、`install-wuf.ps1` 及 `resources` 資料夾。不要只複製單一 EXE。
3. 到 [llama.cpp 官方 Releases](https://github.com/ggml-org/llama.cpp/releases)，展開有程式附件的版本之 **Assets**，找名稱包含 **`bin-win-cpu-x64.zip`** 的附件。此處先使用 CPU 版確認流程；不要下載 Source code 或只有 `cudart` 的附件。若最新版本只有標記檔，往下找有二進位套件的版本。
4. 將 llama.cpp 的 ZIP 完整解壓縮至 **`C:\WUF\llama.cpp`**，確認其中有 **`llama-server.exe`**。保留同包的 DLL 等檔案，不要只抽出 EXE。

完成後主要檔案應該在：

```text
C:\WUF\
├─ Watchdog\
│  ├─ app.exe
│  ├─ launch.json
│  └─ resources\
├─ llama.cpp\
│  └─ llama-server.exe  （及同包其他檔案）
└─ Models\
   └─ Qwen3-0.6B-Q8_0.gguf
```

如果解壓縮後 `llama-server.exe` 在更深一層，請在後面的設定改填該 EXE 的實際完整路徑。

### B. 告訴 Watchdog 檔案放在哪裡

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

### C. 啟動並安裝瀏覽器連接功能

1. 在檔案總管開啟 `C:\WUF\Watchdog`，雙擊 **`app.exe`**。它會在背景啟動，不一定出現一般程式視窗。
2. 用 Chrome 或 Edge 開啟 **[http://127.0.0.1:8080](http://127.0.0.1:8080)**，依第 3 節確認模型能運作。
3. 若要讓 WUF Translate 能從瀏覽器喚醒 Watchdog，在 PowerShell 執行：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File 'C:\WUF\Watchdog\install-wuf.ps1' -Action Install
```

看到 **`Native Host installed for current-user Chrome and Edge`** 就完成瀏覽器連接功能的註冊。請關閉並重新開啟瀏覽器。這一步只處理目前 Windows 使用者的 Chrome／Edge 連接，不會安裝擴充功能、模型或啟用登入自動啟動。

## 2B. macOS 安裝

**預設使用 Apple Foundation Models，不必下載 Qwen／GGUF，不需要 llama.cpp、Python、Go 或 Xcode，也不用修改 `launch.json`。** 本節使用 macOS 27 內建的 `fm` 連接系統本機模型；Apple Foundation Models 框架與 `fm` 工具的系統版本需求不同，不是所有支援 Apple Intelligence 的舊系統都能照本節操作。[Apple 官方介紹](https://developer.apple.com/videos/play/wwdc2026/334/)

### A. 先確認 Apple Intelligence 可以使用

1. 點蘋果選單 →「關於這台 Mac」，確認使用支援 Apple Intelligence 的晶片（例如 M1 或後續 M 系列），且系統為 **macOS 27 或更新版本**。Intel Mac 不適用這個 Apple 模型流程。
2. 到「系統設定 → Apple Intelligence 與 Siri」，依系統提示完成設定，等待系統模型下載完畢。裝置語言、Siri 語言與所在地區也會影響可用性，詳見 [Apple 官方需求](https://support.apple.com/zh-tw/121115)。不需要另開 ChatGPT 帳號或填 API 金鑰。
3. 按 Command + 空白鍵，搜尋「終端機」並開啟，貼上以下指令，按 Enter：

```sh
/usr/bin/fm available --model system
```

看到 **`System model available`** 表示本機 Apple 模型可用。若提示尚未接受命令列工具條款，執行：

```sh
sudo /usr/bin/fm license
```

閱讀並自行決定是否接受 Apple 條款。系統可能要求 Mac 管理員密碼，輸入時不會顯示字元，屬正常現象；完成後再執行一次可用性檢查。這是 Apple 工具的首次設定，不是編譯程式。

如果顯示找不到 `/usr/bin/fm`，先確認系統版本；如果模型不可用，先依 Apple Intelligence 畫面完成下載及設定。**Watchdog 不會替不支援的 Mac 安裝 Apple 系統模型。**

### B. 安裝並開啟 WUF Watchdog

> **目前 GitHub 尚未提供 macOS APP 下載附件。** 以下步驟適用於取得維護者提供的完整 `.app` 後；不能用 Source code ZIP 代替。開發者自行製作 APP 的方式另見[建置文件](docs/BUILD.md#macos建立原生-app)。

1. 從 [Releases](https://github.com/Afteryoupie/WUF-Watchdog/releases)取得適合自己 Mac 的已建置套件。若頁面沒有 APP 附件，表示公開安裝包尚未提供。
2. 將套件解壓縮，把完整的 **WUF Watchdog.app** 放入個人「應用程式」資料夾。Finder 按 Shift + Command + G，輸入 `~/Applications` 即可前往；若資料夾不存在，先在個人資料夾建立 `Applications`。
3. 雙擊 **WUF Watchdog.app**。它使用選單列圖示，不會出現一般主視窗。
4. 用 Chrome 或 Edge 開啟 **[http://127.0.0.1:8080](http://127.0.0.1:8080)**，在控制台選擇 **Apple Foundation Models**；若已選定且休眠，按「啟動引擎」。新手先維持這個模型即可。程式會記住上次選擇，曾用其他模型的人可手動切回 Apple。
5. 狀態變成「準備就緒」後，繼續第 4 節的瀏覽器設定，測試實際翻譯。

若 macOS 阻擋程式，確認來源後查看「系統設定 → 隱私權與安全性」顯示的提示。程式是否已完成 Developer ID 簽署及公證，應以該發布版本的說明為準。

### C. 讓瀏覽器可以喚醒 Watchdog（選用）

手動開啟 APP 後即可使用 API 翻譯。要讓 WUF Translate 能從瀏覽器喚醒尚未啟動的 Watchdog，才需要註冊連接功能。在終端機執行：

```sh
"$HOME/Applications/WUF Watchdog.app/Contents/MacOS/wuf-tool" install-mac \
  --app "$HOME/Applications/WUF Watchdog.app" --action install
```

整段一起貼上，行尾的 `\` 表示同一條指令繼續到下一行。看到 **`Completed install`** 後，關閉並重新開啟 Chrome／Edge。若 APP 放在其他位置，兩處路徑都要改成實際位置。

這一步只為目前使用者註冊瀏覽器連接，不會安裝 WUF Translate，也不會開啟登入自動啟動。

## 3. 第一次啟動：確認安裝成功

在瀏覽器網址列輸入 **[http://127.0.0.1:8080](http://127.0.0.1:8080)**。`127.0.0.1` 的意思是「這台電腦」，只有程式已啟動時才會有控制台。

1. **看到 WUF Watchdog 控制台**：代表管理服務已啟動。
2. **確認模型清單**：macOS 應看到 **Apple Foundation Models**；Windows 依前述範例應看到 `Qwen3-0.6B-Q8_0`。前者代表偵測到 Apple 後端，仍需通過模型可用性檢查；後者代表找到 GGUF 模型檔。
3. **按該模型**：選取會直接開始載入；若已選定且處於休眠，按「啟動引擎」。首次載入請稍候。
4. **狀態變成「準備就緒」**：代表模型執行工具已啟動並通過就緒檢查。接著用下一節的「測試模型運作」確認能產生文字。

「看得到控制台」和「模型可以翻譯」是兩個階段。macOS 看不到 Apple 模型時，先確認系統版本及 `/usr/bin/fm`；Windows 清單空白時先處理模型路徑，不必重裝瀏覽器擴充功能。

預設閒置約 **3 分鐘**會卸載模型，狀態變成「休眠待命」。這是正常省資源行為；Watchdog 還在，下次請求會再載入模型。Apple 模型休眠時會停止本機服務進程，系統模型記憶體則由 macOS 管理，不保證全部立即釋放。若按「退出 WUF Watchdog」，才會關閉整個服務，之後需重新啟動應用程式或透過已註冊的擴充功能喚醒。

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

更新 Watchdog 前，先備份 **`launch.json`**，並退出服務。取得新版已建置套件後更新應用程式。只有使用自訂模型或執行工具路徑的人，才需要將原設定填回新套件；開發者自行建置請依獨立建置文件操作。若應用程式路徑改變，需先用舊位置執行下方解除註冊，再用新位置重新安裝瀏覽器連接功能及登入啟動設定。模型不必跟著重新下載。

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
| Releases 沒有 Watchdog 安裝附件 | 目前尚未發布安裝包；需等待維護者提供，或由開發者依獨立建置文件製作。Source code ZIP 不能直接執行 |
| Windows 說缺少 DLL | 將 llama.cpp 整包解壓縮，保留 EXE 旁的 DLL；不要只複製單一 EXE |
| macOS 找不到 `/usr/bin/fm` | 本頁預設的 Apple 流程需要 macOS 27；有 Apple Intelligence 不代表一定有這個工具 |
| Apple 模型不可用 | 執行 `/usr/bin/fm available --model system`，依提示檢查 Apple Intelligence 下載狀態、語言／地區及工具條款 |
| macOS 阻擋應用程式或 llama-server | 確認下載來源，查看「隱私權與安全性」的允許開啟提示 |
| 開不了 `127.0.0.1:8080` | 先開啟 `app.exe` 或 `.app`；檢查 `launch.json` 是否為合法純文字 JSON、路徑是否正確 |
| 顯示連接埠已被使用 | 可能已有 Watchdog 或其他服務使用 8080；先辨認並關閉衝突的服務，再重試。不要同時啟動另一個使用 8080 的 llama-server |
| 模型清單空白 | macOS Apple 流程先確認 `/usr/bin/fm` 可用；Windows／自訂 GGUF 則確認 `--models-dir`、檔案完整性及資料夾層級，重新啟動後再確認 |
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

macOS 的 MLX、oMLX、自訂 Apple API 後端，以及 Windows GPU 加速，均需要另外準備對應工具與相容硬體。首次安裝時，macOS 請先完成 Apple 預設流程；Windows 請先完成 GGUF 流程。

### 本機連線與資料

Watchdog 預設只接受這台電腦的連線。提供給其他應用程式的 API 網址是 `http://127.0.0.1:8080/v1`；8081、8082 為內部模型服務使用，不需填入擴充功能。不要將這些連接埠轉發到網際網路。

macOS 依本頁選用 Apple 的 `system` 模型時，推論由本機系統模型處理；Windows GGUF 流程由本機 llama.cpp 處理。下載應用程式、系統模型或 GGUF 模型仍需要網路。若擴充功能另選 Google 翻譯或線上服務，文字將依該服務設定傳送。

## 給開發者

日常使用者可以跳過這一節。EXE／APP 的製作方式已移至[開發者建置文件](docs/BUILD.md)，不應要求一般使用者安裝編譯工具。

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
