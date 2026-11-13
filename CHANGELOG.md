# 版本紀錄

## v0.2.1（2026-11-13）

- `app` 啟動時自動為目前使用者註冊 Chrome／Edge 瀏覽器喚醒連接（Native Host），並保留不覆蓋異地註冊的安全檢查。
- 新增 `/api/setup` 端點與控制台「03 / System」設定區，支援一鍵切換登入時自動啟動、重新註冊或解除瀏覽器連接與登入啟動設定。
- macOS 選單列與 Windows 系統匣選單新增「登入時自動啟動」開關。
- macOS 控制台自動檢查 `/usr/bin/fm available --model system` 狀態；若尚未接受條款，提供一鍵複製 `sudo /usr/bin/fm license` 指令。
- 更新 `README.md` 安裝與自動設定說明。

## v0.2.0（2026-10-27）

- Apple 後端允許同一模型同時接收兩筆推論請求，第三筆排隊；保留取消、模型切換、休眠與停止保護。
- 加入並行名額、排隊取消、模型切換與停止的回歸測試，並通過 Go race detector。
- 改善 macOS 選單列圖示與 Homebrew Go 授權檔打包相容性。
- macOS APP 寫入正式版本號。

M3／24 GB 實測：Watchdog 可同時接收兩筆請求，但 Apple 系統模型仍呈現串行生成。
此版本不能視為字幕與網頁已能同時生成，也不保證吞吐量加倍。
