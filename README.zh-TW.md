# line-cli：Windows 版 LINE 指令列工具

[English](README.md) | **繁體中文** | [日本語](README.ja.md)

[![Release](https://img.shields.io/github/v/release/WellWells/line-cli?label=release)](https://github.com/WellWells/line-cli/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

在 Windows 上用指令或 AI agent（Claude Code、Codex 等）操作自己的 LINE：讀訊息、搜尋、傳訊息和檔案、標記成員、按表情回應、收回訊息。

不需要申請 LINE Bot，也不用另外登入。只要這台電腦的 LINE PC 登入過，就能直接使用。

[安裝](#安裝) · [搭配 Claude Code 或 Codex](#搭配-claude-code-或-codex) · [手動操作](#手動操作) · [常見問題](#常見問題) · [安全與隱私](#安全與隱私)

```text
你：幫我看 Family 群組今天在聊什麼，有人找我的話整理給我
Claude：（用 line read 讀訊息後）今天主要在討論週末聚餐，媽媽問你週六有沒有空……
你：幫我回「週六可以」，並標記媽媽
Claude：我會傳這則訊息到 Family，可以嗎？
```

## 安裝

開始前，請先確認：

- 電腦是 Windows 10 或 11（x64）。
- 電腦版 LINE 已經安裝，並且登入過一次。

下面兩種方法選一種。已經在用 Claude Code 或 Codex，建議用方法一。

### 方法一：請 AI agent 幫你安裝

把下面整段貼給 Claude Code 或 Codex：

```text
請幫我安裝 line-cli，這是在 Windows 上操作電腦版 LINE 的指令工具。

1. 執行這行安裝：
   powershell -NoProfile -NonInteractive -Command "irm https://github.com/WellWells/line-cli/releases/latest/download/install.ps1 | iex"
2. 這個工作階段還找不到 line 指令，所以用完整路徑確認安裝成功：
   %LOCALAPPDATA%\linecli\cmd\line.exe status
3. 告訴我安裝結果，以及 line_running 是 true 還是 false。
4. 如果被防毒軟體或防火牆擋下，不要繞過，把訊息和要放行的程式路徑告訴我。
5. 最後提醒我關掉你，開一個新的終端機再啟動，skill 才會生效。
```

agent 要執行指令時可能會先問你，請選允許。這個方法不會跳出勾選清單，skill 會裝給這台電腦上找得到的 agent。

### 方法二：自己用 PowerShell 安裝

**1. 打開 PowerShell**

按鍵盤上的 Windows 鍵，輸入 `powershell`，再按 Enter。會開啟一個可以打字的視窗，不需要選「以系統管理員身分執行」。

**2. 貼上安裝指令**

複製下面這行，到視窗裡按 Ctrl+V 或滑鼠右鍵貼上，再按 Enter：

```powershell
irm https://github.com/WellWells/line-cli/releases/latest/download/install.ps1 | iex
```

**3. 選擇要裝 skill 的 AI agent**

如果跳出清單，這台電腦上有的 agent 已預先勾選。按 Enter，確認摘要後再按一次 Enter。沒有在用 AI agent 的話，按 Esc 略過。

**4. 關掉視窗，再開一個新的 PowerShell**

新開的視窗才找得到 `line` 指令。在新視窗輸入：

```powershell
line status
```

看到 `"line_running": true` 和 `newest_message_at`（最新訊息的時間），代表安裝成功。如果 `line_running` 是 `false`，代表 LINE 沒有開著，資料會停在最後一次同步的時間，請打開 LINE。

第一次使用時，line-cli 會自動取得資料庫金鑰，大約要幾秒鐘，不需要其他設定。

### 更新

再執行一次安裝指令就是更新。`line update` 會告訴你有沒有新版本；每天第一次使用時也會自動檢查，有新版會提示更新指令。

## 搭配 Claude Code 或 Codex

skill 是給 AI agent 看的使用說明。裝好後，你只要用平常的話交代，agent 就知道要用 `line` 指令。

### 1. 確認 skill 裝好了

[安裝](#安裝)時就會一起裝好 skill，不需要另外設定。

如果當時略過了，或是之後才裝 agent，打開 PowerShell 執行：

```powershell
line skill install
```

會跳出下面的清單，這台電腦上有的 agent 已預先勾選。方向鍵移動，空白鍵勾選，Enter 確認：

```text
安裝 line-cli skill：選擇要學會使用 line 的 AI agent。

  [*] 共用資料夾   ~\.agents\skills         固定安裝
      Codex、Cursor、GitHub Copilot、Gemini CLI、OpenCode 等 agent 都讀這裡
      （已偵測到：Codex）
> [x] Claude Code  ~\.claude\skills         已偵測到
  [x] Antigravity  ~\.gemini\config\skills  已偵測到

方向鍵 移動，空白鍵 勾選，A 全選，Enter 下一步，Esc 略過。
```

| Agent | skill 位置 |
|---|---|
| Claude Code | `~\.claude\skills\line-cli` |
| Codex、Cursor、GitHub Copilot、Gemini CLI、OpenCode | `~\.agents\skills\line-cli` |
| Antigravity | `~\.gemini\config\skills\line-cli` |

更新 line-cli 時，skill 會一起更新。

### 2. 開始使用

打開新的 PowerShell，輸入 `claude` 或 `codex` 啟動 agent（用桌面版的話，把程式完整關掉再打開），接著直接說你要做什麼。在任何資料夾都可以：

| 你可以這樣說 | agent 會做的事 |
|---|---|
| 我有哪些未讀的 LINE？幫我摘要 | 讀未讀訊息，整理重點 |
| 小明上週有傳報價單給我嗎？幫我下載 | 搜尋檔案並下載 |
| 整理 Family 群組這個月的重要決定 | 讀取並摘要群組訊息 |
| 回小明「收到，謝謝」 | 先讓你確認，再傳送 |
| 在專案群組標記阿志，問他進度 | 用 `@阿志` 標記他並傳送 |
| 對小明剛才那則訊息按愛心 | 按表情回應 |
| 把我剛才傳錯的那則收回 | 收回你自己的訊息 |
| 等小明回覆，有回覆就告訴我 | 用 `line watch` 等新訊息 |

傳訊息、標記和收回都會以你的身分執行，所以 agent 會先給你看內容，你同意後才動作。

聊天內容是別人寫的文字，agent 不會把訊息裡的話當成指令。

## 手動操作

不用 AI agent 也可以直接下指令。下面的 `小明` 可以換成聊天室名稱，名稱重複時改用 `chat_id`，用 `line chats 小明` 查詢。

### 常用指令

| 想做什麼 | 指令 |
|---|---|
| 看最近的聊天室 | `line chats --limit 10` |
| 看未讀訊息 | `line unread` |
| 讀最新 20 則訊息 | `line read 小明 --limit 20` |
| 讀某段時間的訊息 | `line read 小明 --since 2026-10-01 --until 2026-10-31` |
| 搜尋訊息 | `line search "會議 時間" --since 2026-10-01` |
| 查某人說過什麼 | `line search --from "小明" --since 2026-10-01` |
| 找分享過的檔案 | `line find-file "報價" --ext pdf` |
| 看群組成員 | `line members Family` |
| 統計誰講最多話 | `line stats Family --by sender` |
| 傳訊息 | `line send --to 小明 --message "晚點到" -y` |
| 傳檔案或照片 | `line send --to 小明 --file C:\docs\report.pdf -y` |
| 即時看新訊息 | `line watch` |
| 查看新版本 | `line update` |

想看結果比較好讀，可以加 `--text`：

```powershell
line read 小明 --limit 20 --text
```

### 傳訊息

`-y` 是確定送出。想先看會送出什麼，把 `-y` 換成 `--dry-run`，不會送出：

```powershell
line send --to 小明 --message "晚點到" --dry-run
line send --to 小明 --message "晚點到" -y
```

訊息包含雙引號或很長時，先存成 UTF-8 檔案再傳，這在 PowerShell 裡最穩定：

```powershell
Set-Content -Encoding UTF8 -Path reply.txt -Value "訊息內容"
line send --to 小明 --message-file reply.txt -y
```

### 回覆某則訊息

先用 `--ids` 找到訊息 id，再用 `--reply-to` 回覆，效果和 LINE 的「回覆」相同：

```powershell
line read 小明 --ids --limit 5
line send --to 小明 --message "好的" --reply-to 639305953766277172 -y
```

### 在群組標記成員

文字裡寫 `@名字`，再用 `--mention` 指定是誰。對方會收到通知。

```powershell
line members 專案群組
line send --to 專案群組 --message "@阿志 進度如何？" --mention 阿志 -y
```

| 情況 | 寫法 |
|---|---|
| 標記多人 | `--message "@阿志 @小美 開會" --mention 阿志 --mention 小美` |
| 文字裡的稱呼和本機名字不同 | `--message "@老闆 看一下" --mention 老闆=<成員 id>` |
| 標記所有人 | `--message "@All 開會" --mention-all` |

名字後面要接空白或標點，例如 `@阿志 你好`，不能寫成 `@阿志你好`。

標記和回覆可以同時使用：標記是通知某個人，回覆是引用某則訊息。

### 按表情回應

可用的表情有 `nice`、`love`、`fun`、`amazing`、`sad`、`omg`：

```powershell
line read 小明 --ids --limit 5
line react --to 小明 --id 639305953766277172 --reaction love -y
```

再按一次會換成新的表情。要收回回應：

```powershell
line react --to 小明 --id 639305953766277172 --remove -y
```

### 收回訊息

只能收回自己的訊息，收回後無法復原，對方也可能已經看過：

```powershell
line unsend --to 小明 --id 639305953766277172 -y
```

line-cli 不設時間限制，還能不能收回由 LINE 決定。超過 LINE 的期限時會回傳失敗。

### 傳檔案、照片和影片

```powershell
line send --to 小明 --file C:\docs\report.pdf --file C:\photos\trip.jpg -y
```

- 每個檔案各送一則訊息，一次最多 50 個。每個檔案和每天的總量上限都是 1 GB（LINE 的限制）。
- JPEG、PNG 會以照片送出，H.264 編碼的 MP4 會以可直接播放的影片送出，其他格式以檔案送出。
- 想全部以檔案送出，加 `--as-file`。
- `.exe` 這類程式 LINE 不允許傳送，請先壓成 ZIP。
- JPEG 會和 LINE App 一樣先去除拍攝位置（GPS），電腦上的原檔不會改變。
- LINE 伺服器上的檔案保留 7 天。

### 下載檔案

```powershell
line find-file "報價" --ext pdf
line download --id 639305953766277172
line download 小明 --since 2026-10-01 --media
```

檔案預設存到 `%LOCALAPPDATA%\linecli\downloads`，用 `--out 資料夾` 可以改位置。`--media` 會一併下載原圖、影片和語音。

### 即時看新訊息

```powershell
line watch                  # 所有聊天，按 Ctrl+C 結束
line watch 小明             # 只看一個聊天
line watch --timeout 300    # 看 5 分鐘後結束
line watch --count 1        # 收到一則就結束
```

每則訊息印成一行 JSON，包含你自己傳的訊息（`from_me`）。官方帳號預設不顯示，需要時加 `--include-official`。LINE 要開著才會有新訊息。

### 給程式使用

每個指令預設輸出 JSON，提示訊息寫到 stderr，所以可以放進腳本：

```powershell
line chats Family
line read <chat_id> --limit 100 --out messages.jsonl --jsonl
line watch --jsonl > events.jsonl
```

- 失敗時輸出 `{"error": "代碼", "message": "說明"}`，結束碼是 1。
- `sent: true` 代表 LINE 已經收下，即使 `verified: false` 也不要重送。
- 腳本裡請用 `chat_id`，名稱可能會改變或重複。

完整的參數和欄位：

```powershell
line help                  # 所有指令
line read --help           # 單一指令
line help read --json      # 給程式或 agent 的完整說明
line help errors --json    # 所有錯誤碼
```

## 常見問題

### 輸入 `line` 後顯示無法辨識

關掉 PowerShell 視窗，再開一個新的。安裝完成後，要新開的視窗才找得到 `line` 指令。還是不行，就再執行一次安裝指令。

### 裝好後 `line` 指令不見了，或顯示 Access denied

line-cli 沒有程式碼簽章，又要讀取執行中的 LINE，防毒軟體可能誤判。

1. 確認你是從本專案的 [Releases](https://github.com/WellWells/line-cli/releases) 安裝。
2. 在防毒軟體的隔離區還原下面兩個程式，並加入例外：
   - `%LOCALAPPDATA%\linecli\cmd\line.exe`
   - `%LOCALAPPDATA%\linecli\bin\linecli.exe`
3. 重新執行安裝指令。

公司管理的電腦請先詢問 IT，不要自行加入例外。

### 傳送失敗，說 Windows 擋下了連線

防火牆不允許程式連網。請放行 `%LOCALAPPDATA%\linecli\bin\linecli.exe`。`line.exe` 不會連網，不必放行。更新後路徑不變，規則可以沿用。

### 顯示 `E2EE_KEY_MISSING`

本機還沒有對方的加密金鑰。在 LINE 打開一次這個聊天，再試一次。

### 顯示 `DUPLICATE_SEND`

10 分鐘內已經傳過相同內容到同一個聊天。確定要重傳時，加 `--allow-duplicate`。

### 顯示 `SEND_UNCERTAIN`

訊息可能已經送達。先用 `line read <聊天> --limit 5` 確認，不要直接重送。

### LINE 沒開可以用嗎？

可以讀到最後一次同步的資料。傳送和下載時，line-cli 會自動啟動 LINE，最多等一分鐘。不想讓它自動啟動，設定環境變數 `LINECLI_NO_AUTOSTART=1`。

### `line` 指令突然沒有輸出就結束

LINE 更新時，它的更新程式會結束所有名為 `LINE.exe` 的程式，`line` 也可能被一起結束。重新執行即可。

## 安全與隱私

line-cli 只處理你自己的帳號和這台電腦上的資料。

- 聊天資料只在記憶體中解密，不會在硬碟留下明文資料庫，也不會修改 LINE 的資料。
- 訊息和檔案以 LINE 的 Letter Sealing 加密後才送出。照片和影片的預覽圖與原檔使用同一把金鑰，不想這樣時請加 `--as-file`。
- 只會連到 LINE 的伺服器和 GitHub，不會把資料傳給第三方。連到 GitHub 只是檢查新版本，設定 `LINECLI_NO_UPDATE_NOTIFIER=1` 可關閉。
- 下載的檔案和 `--out` 的輸出是私人資料，請不要放進 git repo、雲端同步或共用資料夾。
- 非官方用戶端可能違反 LINE 的使用條款，帳號可能被限制或停權。請維持一般使用頻率，不要大量發送。

## 限制

- 只支援 Windows。
- 只能傳送到 1 對 1 聊天和群組，不能傳送到社群。還不能傳貼圖。
- 許多官方帳號無法傳送訊息。
- 只讀得到這台電腦存下來的訊息。某個聊天沒有同步時，在 LINE 打開它再試一次。
- 社群討論串的回覆只能用 `read` 讀取。
- 社群裡的圖片、影片和語音無法下載。
- LINE 伺服器上的檔案大約 7 天後過期。

## 解除安裝

```powershell
line uninstall
```

確認後會刪除 line-cli、PATH 上的項目、AI agent 的 skill，以及 `%LOCALAPPDATA%\linecli` 裡的金鑰快取、紀錄和下載的檔案，並檢查沒有殘留。LINE 和 LINE 的聊天資料不受影響。

只想移除 skill，請用 `line skill uninstall`。

## 參與開發

開發方式見 [CONTRIBUTING.md](CONTRIBUTING.md)，回報安全問題見 [SECURITY.md](SECURITY.md)，更新內容見 [CHANGELOG.md](CHANGELOG.md)。

## 授權

- 公開的原始碼以 [MIT](LICENSE) 授權。
- 官方預編譯版包含不開源的協定核心，條款見 [NOTICE](NOTICE)。
- 第三方軟體的授權見 [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt)。
- LINE 是 LY Corporation 的商標。line-cli 是獨立的非官方專案，與 LY Corporation 沒有關聯。請自行確保使用方式符合 LINE 的使用條款。
