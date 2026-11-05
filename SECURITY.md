# 安全政策

## 回報漏洞

請不要在公開 issue 貼出漏洞細節。請到 GitHub 的 [Security Advisories](https://github.com/WellWells/line-cli/security/advisories/new) 私下回報，內容包含：

- 使用的版本（`line version`）
- 重現步驟
- 可能的影響

回報時請不要附上聊天內容、資料庫、金鑰或 token，用合成資料說明即可。

收到後會先確認問題，修正後再公開。

## 支援範圍

只支援最新的正式版。`line update` 可以確認目前是不是最新版。

## 範圍

下列情況屬於安全問題：

- line-cli 把聊天內容、金鑰或 token 傳給 LINE 伺服器和 GitHub 以外的地方，或寫到不該寫的位置
- 聊天內容（訊息、名字、檔名）可以讓 line-cli 或使用它的 agent 執行非預期的動作
- 安裝腳本或更新檢查可以被用來執行非官方的程式
- `line uninstall` 刪除了不屬於 line-cli 的檔案

line-cli 讀取的是執行者自己 Windows 帳號下的資料。已經能以同一個 Windows 使用者身分執行程式的人，本來就能讀到這些資料，這不在範圍內。
