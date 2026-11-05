# line-cli：Windows 版 LINE のコマンドラインツール

[English](README.md) | [繁體中文](README.zh-TW.md) | **日本語**

[![Release](https://img.shields.io/github/v/release/WellWells/line-cli?label=release)](https://github.com/WellWells/line-cli/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Windows で、自分の LINE をコマンドや AI エージェント（Claude Code、Codex など）から操作できます。メッセージの閲覧と検索、テキストやファイルの送信、グループメンバーへのメンション、リアクション、送信取消に対応しています。

LINE Bot の申請も、別途ログインも不要です。この PC の LINE にログインしたことがあれば、すぐに使えます。

[インストール](#インストール) · [Claude Code や Codex と使う](#claude-code-や-codex-と使う) · [手動で使う](#手動で使う) · [よくある質問](#よくある質問) · [セキュリティとプライバシー](#セキュリティとプライバシー)

```text
あなた：家族グループで今日どんな話をしていたか教えて。私宛ての話があればまとめて
Claude：（line read で読んだあと）主に週末の食事の話です。お母さんが土曜日は空いているか聞いています……
あなた：「土曜日で大丈夫」と返信して、お母さんにメンションして
Claude：このメッセージを家族グループに送信します。よろしいですか？
```

## インストール

始める前に、次の点を確認してください。

- PC が Windows 10 または 11（x64）であること。
- PC 版 LINE がインストール済みで、一度はログインしていること。

次の 2 つの方法から選んでください。Claude Code や Codex をすでに使っている場合は、方法 1 がおすすめです。

### 方法 1：AI エージェントにインストールを頼む

次の内容をすべて Claude Code または Codex に貼り付けます。

```text
line-cli をインストールしてください。Windows の PC 版 LINE を操作するコマンドラインツールです。

1. 次のコマンドでインストールしてください。
   powershell -NoProfile -NonInteractive -Command "irm https://github.com/WellWells/line-cli/releases/latest/download/install.ps1 | iex"
2. このセッションでは line コマンドがまだ見つからないので、フルパスで確認してください。
   %LOCALAPPDATA%\linecli\cmd\line.exe status
3. インストールの結果と、line_running が true か false かを教えてください。
4. ウイルス対策ソフトやファイアウォールにブロックされた場合は、回避せず、メッセージと許可が必要なプログラムのパスを教えてください。
5. 最後に、skill を有効にするため、あなたを終了して新しいターミナルで起動し直すよう案内してください。
```

エージェントがコマンドの実行前に確認してきたら、許可を選んでください。この方法ではチェックリストは表示されず、この PC で見つかったエージェントに skill がインストールされます。

### 方法 2：PowerShell で自分でインストールする

**1. PowerShell を開く**

キーボードの Windows キーを押し、`powershell` と入力して Enter を押します。文字を入力できるウィンドウが開きます。「管理者として実行」は不要です。

**2. インストールコマンドを貼り付ける**

次の行をコピーし、ウィンドウに Ctrl+V または右クリックで貼り付けて、Enter を押します。

```powershell
irm https://github.com/WellWells/line-cli/releases/latest/download/install.ps1 | iex
```

**3. skill を入れる AI エージェントを選ぶ**

チェックリストが表示された場合、この PC にあるエージェントはあらかじめ選択されています。Enter を押し、概要を確認してもう一度 Enter を押します。AI エージェントを使わない場合は Esc でスキップします。

**4. ウィンドウを閉じて、新しい PowerShell を開く**

新しいウィンドウでないと `line` コマンドは見つかりません。新しいウィンドウで次を入力します。

```powershell
line status
```

`"line_running": true` と `newest_message_at`（最新メッセージの時刻）が表示されれば、インストールは成功です。`line_running` が `false` の場合は LINE が起動していないため、データは最後に同期した時点までです。LINE を開いてください。

初回はデータベースの鍵を自動で取得するため、数秒かかります。ほかの設定は不要です。

### アップデート

インストールコマンドをもう一度実行するとアップデートされます。`line update` で新しいバージョンの有無を確認できます。毎日最初に使うときにも自動で確認し、新しいバージョンがあれば更新コマンドを表示します。

## Claude Code や Codex と使う

skill は AI エージェント向けの使い方ガイドです。インストールすると、普段の言葉で頼むだけで、エージェントが `line` コマンドを使います。

### 1. skill がインストールされていることを確認する

skill は[インストール](#インストール)のときに一緒に入ります。追加の設定は不要です。

スキップした場合や、あとからエージェントを入れた場合は、PowerShell で次を実行します。

```powershell
line skill install
```

次のチェックリストが表示されます（line-cli の画面は英語または繁体字中国語で表示されます）。この PC にあるエージェントはあらかじめ選択されています。矢印キーで移動し、スペースで選択、Enter で確定します。

```text
Install the line-cli skill: choose the AI agents that should learn to use line.

  [*] Shared folder  ~\.agents\skills         always
      Codex, Cursor, GitHub Copilot, Gemini CLI, OpenCode and other agents read this
      (detected: Codex)
> [x] Claude Code    ~\.claude\skills         detected
  [x] Antigravity    ~\.gemini\config\skills  detected

Up/Down move, Space select, A all, Enter next, Esc skip.
```

| エージェント | skill の場所 |
|---|---|
| Claude Code | `~\.claude\skills\line-cli` |
| Codex、Cursor、GitHub Copilot、Gemini CLI、OpenCode | `~\.agents\skills\line-cli` |
| Antigravity | `~\.gemini\config\skills\line-cli` |

line-cli をアップデートすると、skill も一緒に更新されます。

### 2. 使ってみる

新しい PowerShell を開き、`claude` または `codex` と入力してエージェントを起動します（デスクトップ版の場合は、アプリを完全に終了してから開き直します）。あとは、やりたいことをそのまま伝えてください。どのフォルダーからでも使えます。

| 頼み方の例 | エージェントの動作 |
|---|---|
| LINE の未読を要約して | 未読メッセージを読み、要点をまとめる |
| 田中さんから先週見積書が届いた？ダウンロードして | ファイルを検索してダウンロードする |
| 家族グループの今月の決定事項をまとめて | グループのメッセージを読んで要約する |
| 田中さんに「了解です、ありがとうございます」と返信して | 内容を確認してもらってから送信する |
| プロジェクトのグループで佐藤さんにメンションして進捗を聞いて | `@佐藤` とメンションして送信する |
| 田中さんのさっきのメッセージにハートでリアクションして | リアクションする |
| さっき間違えて送ったメッセージを取り消して | 自分のメッセージを送信取消する |
| 田中さんから返信が来たら教えて | `line watch` で新しいメッセージを待つ |

送信、メンション、送信取消はあなたとして実行されるため、エージェントは先に内容を見せ、あなたの同意を得てから実行します。

トークの内容は他人が書いた文章です。エージェントはメッセージに書かれた内容を指示として扱いません。

## 手動で使う

AI エージェントがなくても、コマンドを直接実行できます。下の例の `田中` はトーク名に置き換えてください。名前が重複する場合は `chat_id` を使います。`line chats 田中` で確認できます。

### よく使うコマンド

| やりたいこと | コマンド |
|---|---|
| 最近のトークを見る | `line chats --limit 10` |
| 未読メッセージを見る | `line unread` |
| 最新 20 件を読む | `line read 田中 --limit 20` |
| 期間を指定して読む | `line read 田中 --since 2026-10-01 --until 2026-10-31` |
| メッセージを検索する | `line search "会議 時間" --since 2026-10-01` |
| 特定の人の発言を探す | `line search --from "田中" --since 2026-10-01` |
| 共有されたファイルを探す | `line find-file "見積" --ext pdf` |
| グループのメンバーを見る | `line members 家族` |
| 誰の発言が多いか集計する | `line stats 家族 --by sender` |
| メッセージを送る | `line send --to 田中 --message "少し遅れます" -y` |
| ファイルや写真を送る | `line send --to 田中 --file C:\docs\report.pdf -y` |
| 新しいメッセージをリアルタイムで見る | `line watch` |
| 新しいバージョンを確認する | `line update` |

結果を読みやすくするには `--text` を付けます。

```powershell
line read 田中 --limit 20 --text
```

### メッセージを送る

`-y` で送信を確定します。送信内容を先に確認するには、`-y` の代わりに `--dry-run` を使います。この場合は送信されません。

```powershell
line send --to 田中 --message "少し遅れます" --dry-run
line send --to 田中 --message "少し遅れます" -y
```

ダブルクォートを含むメッセージや長いメッセージは、UTF-8 のファイルに保存してから送るのが PowerShell では最も確実です。

```powershell
Set-Content -Encoding UTF8 -Path reply.txt -Value "メッセージ本文"
line send --to 田中 --message-file reply.txt -y
```

### メッセージにリプライする

`--ids` でメッセージ ID を調べ、`--reply-to` で返信します。LINE のリプライと同じです。

```powershell
line read 田中 --ids --limit 5
line send --to 田中 --message "わかりました" --reply-to 639305953766277172 -y
```

### グループでメンションする

本文に `@名前` と書き、`--mention` で相手を指定します。相手には通知が届きます。

```powershell
line members プロジェクト
line send --to プロジェクト --message "@佐藤 進捗はいかがですか？" --mention 佐藤 -y
```

| 場合 | 書き方 |
|---|---|
| 複数の人 | `--message "@佐藤 @鈴木 会議です" --mention 佐藤 --mention 鈴木` |
| 本文の呼び方が表示名と違う | `--message "@部長 ご確認ください" --mention 部長=<メンバー ID>` |
| 全員 | `--message "@All 会議です" --mention-all` |

名前のあとには空白か句読点が必要です。`@佐藤 こんにちは` は使えますが、`@佐藤こんにちは` は使えません。

メンションとリプライは併用できます。メンションは相手への通知、リプライはメッセージの引用です。

### リアクションする

使えるリアクションは `nice`、`love`、`fun`、`amazing`、`sad`、`omg` です。

```powershell
line read 田中 --ids --limit 5
line react --to 田中 --id 639305953766277172 --reaction love -y
```

もう一度実行すると新しいリアクションに置き換わります。取り消すには次のようにします。

```powershell
line react --to 田中 --id 639305953766277172 --remove -y
```

### 送信を取り消す

取り消せるのは自分のメッセージだけです。元に戻せません。また、相手がすでに読んでいる場合もあります。

```powershell
line unsend --to 田中 --id 639305953766277172 -y
```

line-cli に時間制限はなく、取り消せるかどうかは LINE が判断します。LINE の期限を過ぎると失敗します。

### ファイル、写真、動画を送る

```powershell
line send --to 田中 --file C:\docs\report.pdf --file C:\photos\trip.jpg -y
```

- ファイルごとに 1 件のメッセージとして送信され、一度に最大 50 個まで送れます。1 ファイルの上限と 1 日の合計の上限はどちらも 1 GB です（LINE の制限）。
- JPEG と PNG は写真、H.264 の MP4 は再生できる動画、それ以外はファイルとして送信されます。
- すべてファイルとして送るには `--as-file` を付けます。
- `.exe` などのプログラムは LINE で送信できません。ZIP に圧縮してください。
- JPEG は LINE アプリと同じく、送信前に位置情報（GPS）を削除します。PC 上の元ファイルは変更されません。
- LINE のサーバー上のファイルは 7 日間保存されます。

### ファイルをダウンロードする

```powershell
line find-file "見積" --ext pdf
line download --id 639305953766277172
line download 田中 --since 2026-10-01 --media
```

保存先は `%LOCALAPPDATA%\linecli\downloads` で、`--out フォルダー` で変更できます。`--media` を付けると、元画像、動画、音声もダウンロードします。

### 新しいメッセージをリアルタイムで見る

```powershell
line watch                  # すべてのトーク。Ctrl+C で終了
line watch 田中             # 1 つのトークだけ
line watch --timeout 300    # 5 分後に終了
line watch --count 1        # 1 件受け取ったら終了
```

各メッセージは 1 行の JSON で表示され、自分のメッセージ（`from_me`）も含まれます。公式アカウントは通常表示されません。表示するには `--include-official` を付けます。新しいメッセージを受け取るには LINE の起動が必要です。

### プログラムから使う

各コマンドは標準で JSON を出力し、補足は stderr に書き出すので、スクリプトに組み込めます。

```powershell
line chats 家族
line read <chat_id> --limit 100 --out messages.jsonl --jsonl
line watch --jsonl > events.jsonl
```

- 失敗すると `{"error": "コード", "message": "説明"}` を出力し、終了コードは 1 になります。
- `sent: true` は LINE が受け付けたという意味です。`verified: false` でも再送しないでください。
- 名前は変更や重複があるため、スクリプトでは `chat_id` を使ってください。

すべてのオプションと項目は次のコマンドで確認できます。

```powershell
line help                  # すべてのコマンド
line read --help           # 1 つのコマンド
line help read --json      # プログラムやエージェント向けの詳細
line help errors --json    # すべてのエラーコード
```

## よくある質問

### `line` が認識されない

PowerShell のウィンドウを閉じて、新しいウィンドウを開いてください。`line` が見つかるのはインストール後に開いたウィンドウだけです。それでも解決しない場合は、インストールコマンドをもう一度実行してください。

### インストール後に `line` が消えた、または Access denied と表示される

line-cli にはコード署名がなく、実行中の LINE を読み取るため、ウイルス対策ソフトが誤検知することがあります。

1. このプロジェクトの [Releases](https://github.com/WellWells/line-cli/releases) からインストールしたことを確認します。
2. 次の 2 つのプログラムを検疫から復元し、例外に追加します。
   - `%LOCALAPPDATA%\linecli\cmd\line.exe`
   - `%LOCALAPPDATA%\linecli\bin\linecli.exe`
3. インストールコマンドをもう一度実行します。

会社で管理されている PC では、自分で例外を追加せず、先に IT 担当者に確認してください。

### 送信に失敗し、Windows が接続をブロックしたと表示される

ファイアウォールがプログラムの通信を止めています。`%LOCALAPPDATA%\linecli\bin\linecli.exe` を許可してください。`line.exe` は通信しないので許可は不要です。アップデート後もパスは変わらないので、ルールはそのまま使えます。

### `E2EE_KEY_MISSING` と表示される

この PC に相手の暗号鍵がまだありません。LINE でそのトークを一度開いてから、もう一度試してください。

### `DUPLICATE_SEND` と表示される

10 分以内に同じ内容を同じトークへ送信しています。本当にもう一度送る場合は `--allow-duplicate` を付けてください。

### `SEND_UNCERTAIN` と表示される

メッセージはすでに届いている可能性があります。再送する前に `line read <トーク> --limit 5` で確認してください。

### LINE を起動していなくても使えますか？

最後に同期した時点までのデータは読めます。送信やダウンロードのときは line-cli が LINE を自動で起動し、最大 1 分待ちます。自動起動を止めるには、環境変数 `LINECLI_NO_AUTOSTART=1` を設定します。

### `line` が何も出力せずに突然終了した

LINE のアップデート時に、アップデーターが `LINE.exe` という名前のプログラムをすべて終了するため、`line` も終了することがあります。もう一度実行してください。

## セキュリティとプライバシー

line-cli が扱うのは、あなた自身のアカウントとこの PC のデータだけです。

- トークのデータはメモリ上でのみ復号します。平文のデータベースをディスクに残さず、LINE のデータも変更しません。
- メッセージとファイルは LINE の Letter Sealing で暗号化してから送信します。写真と動画のプレビューと元ファイルには同じ鍵を使います。これを避けるには `--as-file` を付けてください。
- 接続先は LINE のサーバーと GitHub だけで、第三者にデータを送りません。GitHub への接続は新しいバージョンの確認だけで、`LINECLI_NO_UPDATE_NOTIFIER=1` を設定すると無効にできます。
- ダウンロードしたファイルと `--out` の出力は個人情報です。git リポジトリ、クラウド同期、共有フォルダーに置かないでください。
- 非公式クライアントの利用は LINE の利用規約に違反するおそれがあり、アカウントが制限または停止される場合があります。通常の利用頻度を保ち、大量送信はしないでください。

## 制限事項

- Windows のみ対応しています。
- 送信できるのは 1 対 1 のトークとグループだけで、オープンチャットには送信できません。スタンプはまだ送れません。
- 多くの公式アカウントにはメッセージを送れません。
- 読めるのはこの PC に保存されたメッセージだけです。同期されていないトークは、LINE で開いてからもう一度試してください。
- オープンチャットのスレッドの返信は `read` でのみ読めます。
- オープンチャットの画像、動画、音声はダウンロードできません。
- LINE のサーバー上のファイルは約 7 日で期限が切れます。

## アンインストール

```powershell
line uninstall
```

確認のあと、line-cli、PATH の項目、AI エージェントの skill、`%LOCALAPPDATA%\linecli` 内の鍵キャッシュ、ログ、ダウンロードしたファイルを削除し、残っていないことを確認します。LINE と LINE のトークには影響しません。

skill だけを削除するには `line skill uninstall` を使います。

## 開発への参加

開発方法は [CONTRIBUTING.md](CONTRIBUTING.md)、セキュリティ問題の報告は [SECURITY.md](SECURITY.md)、変更内容は [CHANGELOG.md](CHANGELOG.md) をご覧ください。

## ライセンス

- 公開しているソースコードは [MIT](LICENSE) ライセンスです。
- 公式のビルド済みリリースには非公開のプロトコルコアが含まれます。条件は [NOTICE](NOTICE) をご覧ください。
- 第三者のライセンスは [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt) をご覧ください。
- LINE は LY Corporation の商標です。line-cli は独立した非公式プロジェクトで、LY Corporation とは関係ありません。LINE の利用規約に沿ってご利用ください。
