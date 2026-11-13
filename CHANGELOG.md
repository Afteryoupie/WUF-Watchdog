# 版本紀錄

## v0.2.2（2026-11-13）

- 完成無 Python 重構：移除 Laya Python 決策服務、8082 連接埠、OMLX／MLX Python 後端與相關介面控制；保留舊啟動參數向後相容忽略，舊 `last_model.json` 指向已移除後端時自動回復可用模型。
- 強化管理 API 安全：`/api/setup`、`/api/autostart` 及管理路由統一驗證來源（Origin／Host／Sec-Fetch-Site），設定修改限定 `POST`＋JSON，並移除管理端點的萬用 CORS。
- 改善連線與推論穩定性：ReverseProxy 補上 `GetBody` 支援失效 Keep-Alive 連線安全重試、修正 `fm` 可用性快取失效處理，並防止 Riva 翻譯前綴重複附加。

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
