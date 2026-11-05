# 參與開發

## 公開原始碼的範圍

這裡的原始碼是 open 版，能讀取、搜尋本機的 LINE 資料，也能解密本機快取的媒體。和 LINE 伺服器通訊的部分（`send`、`react`、`unsend`、`download`、社群補讀）與更新檢查不在這份原始碼內，要使用這些功能請安裝官方預編譯版。

## 建置與測試

需要 Windows x64 和 Go 1.27.3 以上，建置不需要 C 編譯器。

```powershell
git clone https://github.com/WellWells/line-cli
cd line-cli
go build -o linecli.exe ./cmd/linecli
go vet ./...
go test ./...
```

一般測試不需要安裝 LINE，全部使用合成資料。

## 修改時請注意

- 修改 Go 檔案後執行 `gofmt -w <檔案>`。
- 程式註解用英文。
- stdout 維持 JSON 契約，提示寫到 stderr。輸出欄位有變動時，同步更新 `line help --json`。
- 測試、issue 和 PR 都不得包含真實的聊天內容、資料庫、金鑰或 token。
- Commit 訊息用 `type(scope): 簡短說明`，例如 `fix(cli): 修正參數驗證`。

PR 請說明行為變更、實際驗證的指令與結果；輸出有變動時附去識別化的範例。

回報安全問題請看 [SECURITY.md](SECURITY.md)。
