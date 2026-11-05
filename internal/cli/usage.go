package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/WellWells/line-cli/internal/line"
)

// Help for people: 'line', 'line help' and '<command> --help' print a
// short overview or one command, in English or Traditional Chinese (lang.go).
// Agents read the skill (skills/line-cli/SKILL.md) and 'help --json' instead.

type humanFlag struct {
	flag string    // as typed, e.g. "--since TIME"
	text [2]string // English, Traditional Chinese
}

type humanCmd struct {
	name  string
	group int       // overview section: groupRead, groupOnline or groupSetup; -1 to leave it out
	args  string    // shown after the name in the overview
	short [2]string // the overview line
	usage []string  // usage lines, after "line "
	long  [2]string // the command's own help
	flags []humanFlag
}

const (
	groupRead = iota
	groupOnline
	groupSetup
)

var groupTitles = [][2]string{
	{"Read (read-only)", "讀取（唯讀）"},
	{"Online (LINE must be running)", "連網（需要 LINE 正在執行）"},
	{"Setup", "設定"},
}

// openOnlineTitle replaces the online section's title in an open build.
var openOnlineTitle = [2]string{"Official prebuilt build only (not in this open build)", "只有官方預編譯版才有（open 版沒有）"}

func defaultN(n int) [2]string {
	return [2]string{fmt.Sprintf("(default %d)", n), fmt.Sprintf("（預設 %d）", n)}
}

func humanLimit(what [2]string, def int) humanFlag {
	d := defaultN(def)
	return humanFlag{"--limit N", [2]string{"At most N " + what[0] + " " + d[0], "最多 N " + what[1] + d[1]}}
}

var (
	flagSince    = humanFlag{"--since TIME", [2]string{"From this time, e.g. 2026-10-01 or '2026-10-01 18:00'", "從這個時間開始，例如 2026-10-01 或 '2026-10-01 18:00'"}}
	flagUntil    = humanFlag{"--until TIME", [2]string{"Up to this time (inclusive)", "到這個時間為止（含）"}}
	flagChat     = humanFlag{"--chat <chat>", [2]string{"Only this chat", "只看這個聊天室"}}
	flagChatType = humanFlag{"--chat-type TYPE", [2]string{"Only chats of this type: personal, group, open, ...", "只看這種類型的聊天室：personal、group、open 等"}}
	flagFrom     = humanFlag{"--from NAME", [2]string{"Only from this person (me for yourself)", "只看這個人傳的（me 代表自己）"}}
	flagOrder    = humanFlag{"--order desc|asc", [2]string{"Newest first (desc, default) or oldest first", "新的在前（desc，預設）或舊的在前"}}
	flagOffline  = humanFlag{"--offline", [2]string{"Only what this PC stores; never contact LINE's server", "只讀本機資料，不連到 LINE 伺服器"}}
)

func humanCmds() []humanCmd {
	return []humanCmd{
		{name: "help", group: -1, args: "[command]",
			short: [2]string{"Show help", "顯示說明"},
			usage: []string{"help [command|topic] [--json]"},
			long:  [2]string{"Show this help, or one command's. --json prints the full reference for AI agents, as do the topics glossary, semantics, recipes, safety and errors.", "顯示這份說明，或單一指令的說明。加上 --json 會輸出給 AI agent 的完整說明；glossary、semantics、recipes、safety、errors 這幾個主題也是給 agent 看的。"},
			flags: []humanFlag{{"--json", [2]string{"The machine-readable help, for AI agents", "給 AI agent 的機器可讀說明"}}}},
		{name: "status", group: groupRead,
			short: [2]string{"Whether the data is current and LINE is running", "資料是否最新、LINE 是否在執行"},
			usage: []string{"status"},
			long:  [2]string{"Show whether LINE is running, when its data was last written, the time span of the messages on this PC, and chats LINE has not synced yet.", "顯示 LINE 是否在執行、資料最後寫入的時間、本機訊息的時間範圍，以及 LINE 還沒同步的聊天室。"}},
		{name: "chats", group: groupRead, args: "[name]",
			short: [2]string{"List chats and their chat_id", "列出聊天室與 chat_id"},
			usage: []string{"chats [name] [--chat-type TYPE] [--limit N]"},
			long:  [2]string{"List chats, most recently active first. A name keeps only the chats whose name contains it.", "列出聊天室，最近有活動的在前。給名稱時，只列出名稱包含它的聊天室。"},
			flags: []humanFlag{flagChatType, humanLimit([2]string{"chats", "個聊天室"}, line.DefaultChatsLimit)}},
		{name: "contacts", group: groupRead, args: "[name]",
			short: [2]string{"List contacts, yourself included", "列出聯絡人（包含自己）"},
			usage: []string{"contacts [name|id] [--include-members] [--limit N]"},
			long:  [2]string{"List your contacts, you first. A name or id looks people up.", "列出聯絡人，自己排第一個。給名稱或 id 可以查詢特定的人。"},
			flags: []humanFlag{{"--include-members", [2]string{"Also open-chat members", "一併列出社群成員"}}, humanLimit([2]string{"people", "人"}, line.DefaultContactsLimit)}},
		{name: "members", group: groupRead, args: "<chat>",
			short: [2]string{"Members of a group or open chat", "群組或社群的成員"},
			usage: []string{"members <chat> [--limit N]"},
			long:  [2]string{"List who is in a group or open chat.", "列出群組或社群裡有哪些人。"},
			flags: []humanFlag{humanLimit([2]string{"members", "位成員"}, line.DefaultMembersLimit)}},
		{name: "read", group: groupRead, args: "<chat>",
			short: [2]string{"Read messages (--since, --until, --around <id>)", "讀取訊息（--since、--until、--around <id>）"},
			usage: []string{"read <chat> [--since TIME] [--until TIME] [--limit N] [--max-threads N] [--offline]", "read --around <id> [<chat>] [--limit N]"},
			long: [2]string{fmt.Sprintf("Read a chat's messages, oldest first: the newest %d, or up to %d in a --since/--until range. Open chats come with the replies in their threads, and a thread (t...) with all its replies. LINE for PC does not store those replies, or open-chat messages it has not synced yet, so read gets them from LINE's server (LINE must be running); without it, or with --offline, read shows what this PC stores.", line.DefaultReadLimit, line.DefaultReadRangeLimit),
				fmt.Sprintf("讀取聊天室的訊息，舊的在前：預設是最新 %d 則；指定 --since、--until 時，最多 %d 則。社群聊天會連同討論串裡的回覆一起列出，指定討論串（t 開頭）就列出它的所有回覆。LINE 電腦版不會保存這些回覆，社群裡還沒同步的訊息也不在本機，read 會從 LINE 伺服器取得（LINE 必須在執行）；連不上或加了 --offline 時，就只列出本機有的訊息。", line.DefaultReadLimit, line.DefaultReadRangeLimit)},
			flags: []humanFlag{flagSince, flagUntil,
				{"--around <id>", [2]string{"The messages around one message (an id from read or search)", "某則訊息的前後文（id 來自 read 或 search）"}},
				{"--limit N", [2]string{"At most N messages (in an open chat, thread replies are added)", "最多 N 則（社群聊天的討論串回覆另外加上）"}},
				{"--max-threads N", [2]string{fmt.Sprintf("Open chats: read the replies of at most N threads (default %d)", line.DefaultReadThreads), fmt.Sprintf("社群聊天：最多讀取 N 個討論串的回覆（預設 %d 個）", line.DefaultReadThreads)}},
				flagOffline}},
		{name: "search", group: groupRead, args: "[text]",
			short: [2]string{"Search messages across chats (--from, --chat, --since)", "跨聊天室搜尋訊息（--from、--chat、--since）"},
			usage: []string{"search [text] [--chat <chat>] [--from NAME] [--type TYPE] [--chat-type TYPE] [--since TIME] [--until TIME] [--limit N] [--order desc|asc]"},
			long:  [2]string{"Search messages in every chat, newest first. Every word must appear; words in double quotes must appear together.", "搜尋所有聊天室的訊息，新的在前。每個詞都要出現；用雙引號括起來的詞要連在一起出現。"},
			flags: []humanFlag{flagChat, flagFrom,
				{"--type TYPE", [2]string{"Only this kind of message: text, image, file, url, ...", "只看這種訊息：text、image、file、url 等"}},
				flagChatType, flagSince, flagUntil, humanLimit([2]string{"messages", "則"}, line.DefaultSearchLimit), flagOrder}},
		{name: "find-file", group: groupRead, args: "[name]",
			short: [2]string{"Find files shared in chats", "找聊天室裡分享過的檔案"},
			usage: []string{"find-file [name] [--ext EXT] [--chat <chat>] [--chat-type TYPE] [--from NAME] [--since TIME] [--until TIME] [--limit N] [--order desc|asc] [--no-local]"},
			long:  [2]string{"Find shared files by file name, newest first; local_path shows where this PC already has one.", "依檔名找分享過的檔案，新的在前；這台電腦已經有的檔案會附上 local_path。"},
			flags: []humanFlag{{"--ext EXT", [2]string{"Only this extension, e.g. pdf", "只看這種副檔名，例如 pdf"}},
				flagChat, flagChatType, flagFrom, flagSince, flagUntil, humanLimit([2]string{"files", "個檔案"}, line.DefaultFindFileLimit), flagOrder,
				{"--no-local", [2]string{"Do not look for copies on this PC", "不檢查這台電腦上有沒有檔案"}}}},
		{name: "stats", group: groupRead, args: "<chat>",
			short: [2]string{"Count messages (--by sender|day|hour|type)", "統計訊息數量（--by sender|day|hour|type）"},
			usage: []string{"stats <chat> [--by sender|day|hour|type] [--since TIME] [--until TIME] [--limit N] [--include-events]"},
			long:  [2]string{"Count a chat's messages without reading them.", "統計聊天室的訊息數量，不讀出內容。"},
			flags: []humanFlag{{"--by sender|day|hour|type", [2]string{"Count per sender (default), day, hour or type", "依發話者（預設）、日期、時段或類型分組"}},
				flagSince, flagUntil, humanLimit([2]string{"groups", "組"}, line.DefaultStatsLimit),
				{"--include-events", [2]string{"Also count system events and unsent messages", "也計入系統事件與收回的訊息"}}}},
		{name: "unread", group: groupRead,
			short: [2]string{"Unread messages", "未讀訊息"},
			usage: []string{"unread [--include-official] [--include-hidden] [--limit-chats N] [--per-chat-limit N] [--offline]"},
			long:  [2]string{"Show the chats with unread messages, and those messages. Unread open-chat messages that LINE for PC has not synced come from LINE's server (LINE must be running).", "列出有未讀訊息的聊天室，以及這些未讀訊息。社群裡 LINE 電腦版還沒同步的未讀訊息，會從 LINE 伺服器取得（LINE 必須在執行）。"},
			flags: []humanFlag{{"--include-official", [2]string{"Also official accounts", "也包含官方帳號"}},
				{"--include-hidden", [2]string{"Also hidden chats", "也包含隱藏的聊天室"}},
				{"--limit-chats N", [2]string{"At most N chats " + defaultN(line.DefaultUnreadChats)[0], "最多 N 個聊天室" + defaultN(line.DefaultUnreadChats)[1]}},
				{"--per-chat-limit N", [2]string{"At most N messages per chat " + defaultN(line.DefaultUnreadPerChat)[0], "每個聊天室最多 N 則" + defaultN(line.DefaultUnreadPerChat)[1]}},
				flagOffline}},
		{name: "watch", group: groupRead, args: "[chat]",
			short: [2]string{"Print new messages as LINE stores them", "LINE 存下新訊息時即時印出"},
			usage: []string{"watch [chat] [--interval N] [--count N] [--timeout N] [--include-official]"},
			long: [2]string{"Follow this PC's LINE data and print each new message on its own line, until Ctrl+C. Give a chat to follow only that one. LINE must be running. It only reads what LINE PC stores, so thread replies and open chats LINE has not synced do not appear.",
				"持續讀取這台電腦的 LINE 資料，每則新訊息印成一行，按 Ctrl+C 結束。指定聊天室就只看那一個。LINE 必須開著。只讀得到 LINE 電腦版存下的內容，討論串的回覆和還沒同步的社群訊息不會出現。"},
			flags: []humanFlag{{"--interval N", [2]string{"Look every N seconds (default 2)", "每 N 秒檢查一次（預設 2）"}},
				{"--count N", [2]string{"Stop after N messages", "收到 N 則訊息後結束"}},
				{"--timeout N", [2]string{"Stop after N seconds", "N 秒後結束"}},
				{"--include-official", [2]string{"Also show official accounts", "也顯示官方帳號"}}}},
		{name: "preview", group: groupRead, args: "<chat>",
			short: [2]string{"Save cached images and video thumbnails", "存下本機快取的圖片與影片縮圖"},
			usage: []string{"preview <chat> [--since TIME] [--until TIME] [--limit N] [--out DIR]"},
			long:  [2]string{"Save the images and video thumbnails LINE cached on this PC, decrypted, without going online. For full size, use download.", "把 LINE 快取在這台電腦的圖片與影片縮圖解密存檔，不連網。要原尺寸請用 download。"},
			flags: []humanFlag{flagSince, flagUntil, humanLimit([2]string{"images", "張"}, line.DefaultPreviewLimit),
				{"--out DIR", [2]string{`Save to DIR (default %LOCALAPPDATA%\linecli\media)`, `存到 DIR（預設 %LOCALAPPDATA%\linecli\media）`}}}},
		{name: "download", group: groupOnline, args: "<chat>",
			short: [2]string{"Download shared files (--media adds images, videos, voice)", "下載分享的檔案（--media 包含原圖、影片、語音）"},
			usage: []string{"download <chat> [--since TIME] [--until TIME] [--limit N] [--out DIR] [--media]", "download --id <id> [--out DIR]"},
			long:  [2]string{"Download a chat's shared files from LINE's server, decrypted. Files expire about 7 days after they were sent.", "從 LINE 伺服器下載聊天室分享的檔案並解密存檔。檔案大約在傳送 7 天後過期。"},
			flags: []humanFlag{{"--id <id>", [2]string{"Just this message's file, image, video or voice message", "只下載這則訊息的檔案、圖片、影片或語音"}},
				flagSince, flagUntil, humanLimit([2]string{"files", "個檔案"}, line.DefaultDownloadLimit),
				{"--out DIR", [2]string{`Save to DIR (default %LOCALAPPDATA%\linecli\downloads)`, `存到 DIR（預設 %LOCALAPPDATA%\linecli\downloads）`}},
				{"--media", [2]string{"Also full-size images, videos and voice messages", "也下載原尺寸圖片、影片與語音"}}}},
		{name: "react", group: groupOnline, args: "--to <chat_id> --id <id>",
			short: [2]string{"React to a message, or remove your reaction (--dry-run first, then -y)", "對訊息按表情回應，或收回你的回應（先 --dry-run，確認後加 -y）"},
			usage: []string{"react --to <chat_id> --id <id> (--reaction <name> | --remove) (--dry-run | -y)"},
			long: [2]string{"Add your reaction to a message stored on this PC, or remove it. Works in one-to-one chats and groups. The other people in the chat can see it.",
				"對這台電腦存有的訊息按表情回應，或收回回應。可用於 1 對 1 聊天和群組。聊天裡的其他人都看得到。"},
			flags: []humanFlag{{"--to <chat_id>", [2]string{"The chat the message is in", "訊息所在的聊天室"}},
				{"--id <id>", [2]string{"The message", "要回應的訊息"}},
				{"--reaction <name>", [2]string{"nice, love, fun, amazing, sad or omg", "nice、love、fun、amazing、sad 或 omg"}},
				{"--remove", [2]string{"Remove your reaction", "收回你的回應"}},
				{"--dry-run", [2]string{"Show what would be done, without doing it", "只顯示內容，不實際執行"}},
				{"-y, --yes", [2]string{"Do it", "確定執行"}}}},
		{name: "unsend", group: groupOnline, args: "--to <chat_id> --id <id>",
			short: [2]string{"Take back a message you sent (--dry-run first, then -y)", "收回你送出的訊息（先 --dry-run，確認後加 -y）"},
			usage: []string{"unsend --to <chat_id> --id <id> (--dry-run | -y)"},
			long: [2]string{"Take back your own message. LINE decides whether it still can be, and the result tells you. It cannot be undone, and people may already have read it.",
				"收回你自己送出的訊息。還能不能收回由 LINE 決定，結果會顯示成功或失敗。收回後無法復原，對方也可能已經看過。"},
			flags: []humanFlag{{"--to <chat_id>", [2]string{"The chat the message is in", "訊息所在的聊天室"}},
				{"--id <id>", [2]string{"The message", "要收回的訊息"}},
				{"--dry-run", [2]string{"Show what would be done, without doing it", "只顯示內容，不實際執行"}},
				{"-y, --yes", [2]string{"Do it", "確定執行"}}}},
		{name: "update", group: groupOnline,
			short: [2]string{"Show the latest version and how to update", "顯示最新版本與更新方式"},
			usage: []string{"update"},
			long: [2]string{"Ask GitHub for the latest release and show the command that updates line-cli. line-cli does not replace itself. It also looks once a day by itself, after the first command that succeeds, and adds one line to stderr when there is a new version; set LINECLI_NO_UPDATE_NOTIFIER=1 to turn that off.",
				"向 GitHub 查詢最新版本，並顯示更新指令。line-cli 不會自行更新。每天第一次指令成功後也會自動檢查一次，有新版本時在 stderr 多印一行；設定 LINECLI_NO_UPDATE_NOTIFIER=1 可關閉。"}},
		{name: "send", group: groupOnline, args: "--to <chat_id>",
			short: [2]string{"Send text, files, photos or videos as you (--dry-run first, then -y)", "以你的身分傳送文字、檔案、照片或影片（先 --dry-run，確認後加 -y）"},
			usage: []string{
				"send --to <chat_id> (--message TEXT | --message-file FILE | --stdin) [--reply-to <id>] [--mention <who> ...] [--mention-all] (--dry-run | -y)",
				"send --to <chat_id> --file PATH [--file PATH ...] [--as-file] [--message TEXT] (--dry-run | -y)",
			},
			long: [2]string{"Send a text message, files, photos or videos as you to a one-to-one chat or a group. Each file goes as its own message, after the text if there is one; JPEG and PNG images go as photos and H.264 MP4 videos as videos that play in the chat; other videos (HEVC, MOV, AVI ...) go as files. Only file types LINE allows can be sent (programs such as .exe cannot; put them in a ZIP file). A JPEG goes without its location (GPS), as LINE sends it. LINE keeps a file on its server for 7 days and takes up to 1 GB per file. Check it with --dry-run, then send it with -y.",
				"以你的身分傳送文字訊息、檔案、照片或影片到 1 對 1 聊天或群組。每個檔案各送一則訊息，有文字時先送文字；JPEG、PNG 圖片會以照片送出，H.264 編碼的 MP4 影片會以可在聊天裡直接播放的影片送出；其他影片（HEVC、MOV、AVI 等）以檔案送出。只能傳送 LINE 允許的檔案類型（.exe 這類程式不行，請先壓成 ZIP）。JPEG 照片會和 LINE 一樣去除拍攝位置（GPS）再送出。LINE 伺服器上的檔案保留 7 天，每個檔案上限 1 GB。先用 --dry-run 確認，再加 -y 送出。"},
			flags: []humanFlag{{"--to <chat_id>", [2]string{"The chat to send to", "要傳送到的聊天室"}},
				{"--message TEXT", [2]string{"The text", "訊息文字"}},
				{"--message-file FILE", [2]string{"Read the text from a UTF-8 file (safest in Windows PowerShell)", "從 UTF-8 檔案讀取文字（在 Windows PowerShell 最安全）"}},
				{"--stdin", [2]string{"Read the text from standard input", "從標準輸入讀取文字"}},
				{"--file PATH", [2]string{"Send this file; repeat for more files (up to 50); wildcards such as *.pdf work", "傳送這個檔案；要傳多個就重複指定（最多 50 個）；可用 *.pdf 這類萬用字元"}},
				{"--as-file", [2]string{"Send images and videos as files, not as photos or videos", "圖片和影片都以檔案送出，不以照片或影片送出"}},
				{"--dry-run", [2]string{"Show the message without sending it", "只顯示訊息，不送出"}},
				{"-y, --yes", [2]string{"Send it", "確定送出"}},
				{"--allow-duplicate", [2]string{"Send even if the same text or file went to this chat in the last 10 minutes", "10 分鐘內送過相同內容或檔案時，仍然送出"}},
				{"--reply-to <id>", [2]string{"Reply to that message of the chat, quoting it as LINE's Reply does", "回覆這個聊天裡的那則訊息，像 LINE 的「回覆」一樣引用它"}},
				{"--mention <who>", [2]string{"Tag a group member where the text says @name: their id, exact name, or <word>=<id>; repeat for more people", "標記群組成員，文字裡要寫 @名字：可用成員 id、完整名字或 <文字>=<id>；要標記多人就重複指定"}},
				{"--mention-all", [2]string{"Tag everyone where the text says @All", "標記所有人，文字裡要寫 @All"}}}},
		{name: "init", group: -1, args: "[--forget]",
			short: [2]string{"Fetch the database key again, or forget it", "重新取得資料庫金鑰，或刪除快取的金鑰"},
			usage: []string{"init [--forget]"},
			long:  [2]string{"Not needed in normal use: every command fetches and caches the database key by itself when it needs it, starting LINE if it does not run. This forces a new fetch.", "平常不需要執行：每個指令需要時都會自動取得並快取資料庫金鑰，LINE 沒在執行時也會自動啟動它。這個指令只是強制重新取得。"},
			flags: []humanFlag{{"--forget", [2]string{"Delete the cached key", "刪除快取的金鑰"}}}},
		{name: "skill", group: groupSetup, args: "install",
			short: [2]string{"Teach AI agents (Claude Code, Codex, ...) to use line-cli", "讓 AI agent（Claude Code、Codex 等）學會使用 line-cli"},
			usage: []string{"skill install [--agent LIST] [-y]", "skill uninstall", "skill"},
			long: [2]string{`install shows a checklist of AI agents, the ones on this PC ticked, and installs the line-cli skill for those you choose: in the shared folder ~\.agents\skills, which Codex, Cursor, GitHub Copilot, Gemini CLI and OpenCode read, and a copy for Claude Code (~\.claude\skills), Antigravity (~\.gemini\config\skills) and other agents. Your choice is saved, and updates keep to it without asking. uninstall removes the skill everywhere. Without either, print the skill.`,
				`install 會列出 AI agent 讓你勾選（這台電腦上有的會先勾好），把 line-cli 的 skill 裝給你選的 agent：本體放在共用資料夾 ~\.agents\skills（Codex、Cursor、GitHub Copilot、Gemini CLI、OpenCode 都讀這裡），Claude Code（~\.claude\skills）、Antigravity（~\.gemini\config\skills）等各放一份複本。你的選擇會記住，之後更新沿用，不再詢問。uninstall 會從所有地方移除。兩者都不加時，直接印出 skill 內容。`},
			flags: []humanFlag{{"--agent LIST", [2]string{"Install for these agents without asking, e.g. claude-code,antigravity (none: no agent)", "不詢問，直接裝給這些 agent，例如 claude-code,antigravity（none：都不裝）"}},
				{"--update", [2]string{"Keep to the saved choice; ask only the first time (the install script uses this)", "沿用記住的選擇，只有第一次才詢問（安裝腳本用這個）"}},
				{"-y, --yes", [2]string{"Do not ask: the saved choice, else the agents on this PC", "不詢問：沿用記住的選擇，沒有就裝給這台電腦上有的 agent"}}}},
		{name: "uninstall", group: groupSetup,
			short: [2]string{"Remove line-cli from this PC", "從這台電腦移除 line-cli"},
			usage: []string{"uninstall [-y]"},
			long: [2]string{"Delete line-cli, its entry on the user PATH, its skill for AI agents and %LOCALAPPDATA%\\linecli (key cache, send log, previews and downloads), then check that they are gone. It asks first; -y skips the question. LINE and its data are not touched.",
				"刪除 line-cli、使用者 PATH 上的項目、AI agent 的 skill，以及 %LOCALAPPDATA%\\linecli（金鑰快取、送出紀錄、預覽與下載的檔案），並確認都已刪除。執行前會先詢問，加 -y 不詢問。LINE 本身和 LINE 的資料不受影響。"},
			flags: []humanFlag{{"-y, --yes", [2]string{"Do not ask", "不詢問"}}}},
		{name: "version", group: groupSetup,
			short: [2]string{"Show the version", "顯示版本"},
			usage: []string{"version"},
			long:  [2]string{"Print the version and the git commit it was built from.", "顯示版本，以及建置時的 git commit。"}},
	}
}

// printOverview prints the short help 'line' and 'line help' show.
func printOverview(w io.Writer, l lang) {
	cmds := humanCmds()
	if edition == "open" {
		fmt.Fprintln(w, l.pick([2]string{
			"line-cli (open build) reads your own LINE PC data and never contacts LINE's servers.",
			"line-cli（open 版）讀取你自己電腦上的 LINE PC 資料，不會連到 LINE 伺服器。"}))
	} else {
		fmt.Fprintln(w, l.pick([2]string{"line-cli reads your own LINE PC data and sends messages as you.", "line-cli 讀取你自己電腦上的 LINE PC 資料，也能以你的身分傳送訊息。"}))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, l.pick([2]string{"Usage: line <command> [flags]", "用法：line <指令> [參數]"}))
	for g, title := range groupTitles {
		if g == groupOnline && edition == "open" {
			title = openOnlineTitle
		}
		fmt.Fprintf(w, "\n%s\n", l.pick(title))
		for _, c := range cmds {
			if c.group == g {
				fmt.Fprintf(w, "  %-30s %s\n", strings.TrimSpace("line "+c.name+" "+c.args), l.pick(c.short))
			}
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, l.pick([2]string{
		"<chat> is a chat_id or part of a chat name. Output is JSON; add --text for one line per item.\n" +
			"Help for a command: line <command> --help\n" +
			"For AI agents: line skill install, or the full reference with line help --json",
		"<chat> 可以是 chat_id 或聊天室名稱的一部分。輸出是 JSON，加 --text 改成一行一筆。\n" +
			"單一指令的說明：line <指令> --help\n" +
			"給 AI agent：line skill install，或用 line help --json 看完整說明",
	}))
}

// printCommandHelp prints one command's help for people; false if there is none.
func printCommandHelp(w io.Writer, l lang, name string) bool {
	for _, c := range humanCmds() {
		if c.name != name {
			continue
		}
		fmt.Fprintln(w, l.pick([2]string{"Usage:", "用法："}))
		for _, u := range c.usage {
			fmt.Fprintf(w, "  line %s\n", u)
		}
		fmt.Fprintf(w, "\n%s\n", l.pick(c.long))
		if c.group == groupOnline && edition == "open" {
			fmt.Fprintf(w, "\n%s\n", l.pick([2]string{"This command is not available in this open build.", "這個指令在 open 版無法使用。"}))
		}
		if len(c.flags) > 0 {
			fmt.Fprintf(w, "\n%s\n", l.pick([2]string{"Flags:", "參數："}))
			width := 0
			for _, f := range c.flags {
				width = max(width, len(f.flag))
			}
			for _, f := range c.flags {
				fmt.Fprintf(w, "  %-*s  %s\n", width, f.flag, l.pick(f.text))
			}
		}
		fmt.Fprintf(w, "\n%s\n", l.pick([2]string{
			"Global flags: --text, --compact, --jsonl, --out FILE. Full reference: line help " + name + " --json",
			"通用參數：--text、--compact、--jsonl、--out FILE。完整說明：line help " + name + " --json",
		}))
		return true
	}
	return false
}
