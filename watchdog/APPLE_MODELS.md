# macOS 27 本地 Apple Foundation Models

WUF Watchdog偵測到 `/usr/bin/fm` 時會列出「Apple Foundation Models（本機）」。
在選單列或 http://127.0.0.1:8080/status 選取即可，客戶端仍使用
http://127.0.0.1:8080/v1。請求中的模型名稱會轉為 Apple 的 `system`。

首次使用須由使用者在終端機執行 `sudo fm license`，閱讀並接受全機適用的
Apple CLI 條款，並在系統設定中啟用 Apple Intelligence。可用
`fm available --model system` 確認可用性。

後端使用 `fm serve --host 127.0.0.1 --port 8081`，
並在 API 請求中明確指定 `model: system` 選用本地模型。
沿用WUF Watchdog的串流轉發、切換及閒置停止流程。
停止的是 fm 服務進程；Apple 管理的系統模型記憶體由 macOS 自行回收，
不保證全部釋放。模型上下文與支援參數受 Apple 的 Chat Completions API 限制。

更新程式後重新啟動 WUF Watchdog.app；建置方式見 watchdog/design/WINDOWS.md。

官方介紹：https://developer.apple.com/videos/play/wwdc2026/334/

## OpenCode 實測（2026-10-01）

- 環境：macOS 27.0.1、OpenCode 桌面版 1.18.31。
- 經 8080 代理的簡短 Chat Completions 請求成功，回覆 `APPLE_LOCAL_OK`，
  API 回傳 `model: system`。
- OpenCode 使用既有 local provider 的 `default_model`（8080/v1）與 Build
  代理，請求已送達 Apple 後端，但收到
  `The session's transcript exceeded the model's context size.`，未產生正文回覆。
- WUF Watchdog會將既有 local 模型別名改為 `system`；OpenCode 顯示的 Ling 名稱
  是既有設定標籤，實際後端以WUF Watchdog狀態與 API 的 `model` 為準。
- 尚未通過 OpenCode Build 代理與工具操作測試；需另配置精簡提示與工具集，
  並依實際模型限制設定上下文，不能將 API 連線成功視為編程代理可用。

## 並行翻譯

Apple 後端（`apple`、`apple-api`）現在允許同一模型同時處理兩筆推論請求，
讓字幕與網頁可各佔一筆；第三筆會排隊，並沿用既有排隊逾時與取消機制。
共用同一個後端服務，不會為每條通道另啟一份模型進程。
Apple API 為每個請求建立獨立的 `LanguageModelSession`；實際運算仍由 macOS 排程，
不保證兩筆請求的總速度加倍。

任一請求尚未結束（包含串流傳送期間），手動休眠與切換模型會回傳 409；
請求指定不同模型時則等現有請求全部結束後再切換。停止 Watchdog 會取消兩筆
執行中的請求與排隊工作。`/api/status` 的 `active_requests` 可用來觀察並行數。
其他後端仍維持單筆推論。這是服務端能力；客戶端也需同時送出字幕與網頁請求。

本機驗證（M3／24 GB，2026-10-27）：Watchdog 可同時保留兩筆 HTTP／SSE 請求，
忙碌時休眠回傳 409，兩筆回應均成功。串流測試中，第二筆仍在第一筆生成結束後
才開始輸出；直接用兩個獨立 Foundation Models 工作階段重測也相同。
因此此修改解除的是 Watchdog 的整段回應互斥，不能視為系統模型已能同時生成，
也不能據此宣稱字幕延遲或總吞吐量已改善。
