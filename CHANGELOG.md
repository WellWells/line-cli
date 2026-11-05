# 更新紀錄

版本號用 `v1.x.y`：x 是新增功能或其他改動，y 只修正 bug。

## 1.0.0 - 2026-11-05 22:40

第一個公開版本。

### 新功能

- 讀取本機的 LINE 資料：`chats`、`read`、`unread`、`search`、`stats`、`members`、`contacts`、`find-file`、`status`
- 查看與下載圖片和檔案：`preview`、`download`
- 傳送文字、檔案、照片和影片：`send`，支援 `--reply-to` 回覆訊息
- 在群組標記成員：`send --mention`、`send --mention-all`
- 按表情回應與收回訊息：`react`、`unsend`
- 即時看新訊息：`watch`
- 讓 AI agent 學會使用 line-cli：`skill install`
- 檢查新版本：`update`，每天第一次使用時也會自動檢查
- 一個指令移除乾淨：`uninstall`
