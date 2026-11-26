package cli

import (
	"fmt"

	"github.com/WellWells/line-cli/internal/line"
)

// Help for line notes and line albums: the agents' entries (help --json) and
// the help for people.

// timelineOnline says where notes and albums come from.
func timelineOnline() string {
	const scope = "Groups only (c...). LINE PC never stores group notes or albums. The full edition reads them online from LINE's servers."
	if edition == "open" {
		return "Group notes and albums are unavailable in this open build. " + scope
	}
	return scope + " Once local access is set up, LINE need not be running. The chat resolves like read. Writes take --to with a chat_id or exact name."
}

// timelineWrites is how the writes of notes and albums work.
const timelineWrites = "Preview the requested subcommand with --dry-run. Show the group, action and affected items. After the user approves, repeat it with the resolved group id and the same item ids, replacing --dry-run with -y. On exit 1, check the error and any partial result before retrying. " + verifyWrite + " Each real write is logged to %LOCALAPPDATA%\\linecli\\send-log.jsonl without its text. CHANNEL_TOKEN_MISSING means the user should open the group's "

// timelinePacing is how fast notes and albums ask LINE's server.
const timelinePacing = "Each call to LINE's server waits at least 1.2 s after the one before; a firewall that blocks linecli.exe gives the same error as download."

const notesTextColumns = "--text prints one note per line: created_at, id, author, me (\"me\" or empty), kind, comments=N, then the text (with a link's title and URL and [N photos/videos] added); each comment follows its note on its own line: created_at, id, author, me, comment, an empty column, the text. A write prints a short report: DONE, DRY-RUN (nothing done) or NOT DONE, then the action, the group and the ids. " + textEscapes

const albumsTextColumns = "--text prints one album per line: id, title, photos=N, created_at, updated_at; with --id one photo per line: created_at, id, uploader, me, type, WIDTHxHEIGHT; download prints download's --text columns, and a write a short report as notes does. " + textEscapes

// timelinePlanFields are the fields of a notes or albums write.
const timelinePlanFields = "(writes print one object of) action (note_add|note_comment|note_delete|comment_delete|album_create|album_add|album_delete|photo_delete), to, to_name, chat_type, text (the note or comment; for a delete, the first 100 characters of what was deleted), post_id, comment_id, album_id, title, photo_count (album_delete: how many photos the album holds), files{name, kind, bytes, gps_removed (a JPEG's location was blanked in what was sent), status (planned|sent|failed|uncertain|not_sent: failed was certainly not added, uncertain may have been, so check before adding it again), error}, photos (the photo ids to delete), url (a note's https://line.me link, or an album's line:// link that only LINE's app opens), dry_run, sent, deleted (photo_delete: how many photos LINE says it deleted, 0 included), error, message (only when a write went part way: an album was made or some photos were added but a later step failed, or LINE deleted fewer photos than asked; the command then exits 1)"

// timelineDownloadFields are download's fields, which notes download and
// albums download print.
const timelineDownloadFields = "(download prints download's array of) id, chat_id, chat_name, type, sender, sender_id, filename, sent_at (the photo's upload time), from_me, status, saved_as, bytes, note"

func notesDoc() cmdDoc {
	return cmdDoc{
		Name: "notes",
		Usage: "line notes <chat_id|name> [--limit N] [--comments N] [--since T] [--until T] [--cursor C] | line notes <chat_id|name> --id POSTID [--comments N] | line notes download <chat_id|name> --id POSTID | " +
			"line notes add --to <chat_id|exact-name> (--message S | --message-file PATH | --stdin) (--dry-run | -y) [--allow-duplicate] | " +
			"line notes comment --to <chat_id|exact-name> --id POSTID (--message S | --message-file PATH | --stdin) (--dry-run | -y) [--allow-duplicate] | " +
			"line notes delete --to <chat_id|exact-name> --id POSTID [--comment COMMENTID] (--dry-run | -y)",
		Summary: "Read a group's notes (記事本，也常稱備忘錄), add a text note, comment on one, or delete your own note or comment. " + timelineOnline() + " " +
			"A first argument add, comment, delete or download is the subcommand; give a group named like that by its chat_id. " +
			fmt.Sprintf("notes <chat>: the most recently created notes first (default %d, max %d; an edited note keeps its place by created_at), with --since/--until on created_at (page older with --until <oldest created_at> as the stderr note says, deduping by id) and --comments N (at most %d) of each note's newest comments inline, oldest first. A run reads at most %d pages of notes; when it stops there, the stderr note gives a --cursor that continues the scan where it stopped (keep the same --since/--until). ", line.DefaultNotesLimit, line.MaxNotesLimit, line.MaxNoteInlineComments, 25) +
			fmt.Sprintf("notes <chat> --id POSTID: one note with its newest comments (default %d, max %d; --comments 0 for none). ", line.DefaultNoteComments, line.MaxNoteComments) +
			"notes download <chat> --id POSTID: the note's photos and videos into %LOCALAPPDATA%\\linecli\\downloads, named and reused like download. " +
			"kind: note (text), link (a link preview, in link) or poll (投票: a poll is a note; its question is the text, but its options and votes are not read, so never guess them). author is the name you know them by, else the name LINE gives. " +
			"add posts a text note (LINE also posts a notice of it in the chat); comment comments on note --id; delete deletes note --id, or with --comment that comment of it; only your own notes and comments can be deleted (someone else's is INVALID_ARGUMENT, checked when it runs). Text follows send's rules (UTF-8, at most 10000 characters, normalized). " +
			timelineWrites + "Notes (記事本) tab in LINE once, then retry. DUPLICATE_SEND: the same text was added to the same group, or commented on the same note, in the last 10 minutes; --allow-duplicate only for an intended repeat. " +
			"Not supported: notes with photos (LINE refuses them from a desktop client), creating polls, likes and editing. " +
			fmt.Sprintf("At most %d pages of notes and %d of comments are read per run. ", 25, 25) + timelinePacing + " " + notesTextColumns + " " + untrusted,
		Flags: []string{
			fmt.Sprintf("--limit N (notes; default %d, max %d)", line.DefaultNotesLimit, line.MaxNotesLimit),
			fmt.Sprintf("--comments N (each note's newest comments, inline: default 0, max %d; with --id default %d, max %d, 0 for none)", line.MaxNoteInlineComments, line.DefaultNoteComments, line.MaxNoteComments),
			"--since T (notes created from T on)",
			"--until T (notes created up to T)",
			"--cursor C (continue the scan where a run that read its page limit stopped, as its stderr note says)",
			"--id POSTID (one note; for download, comment and delete: the note)",
			"--to chat_id|exact-name (add, comment, delete: the group)",
			"--message S (add, comment: the text)",
			"--message-file PATH (add, comment: the text from a UTF-8 file; safest on Windows PowerShell 5.1)",
			"--stdin (add, comment: read the text from standard input)",
			"--comment COMMENTID (delete: that comment of the note, not the note)",
			"--dry-run (show it, write nothing)",
			"-y/--yes (confirm a real write)",
			"--allow-duplicate (add, comment: even if the same text went there in the last 10 minutes)",
		},
		Output: "array of notes (the most recently created first; with --id one note object): id, chat_id, chat_name, author, author_id (only with --ids or where two people share a name), text, kind (note|link|poll), created_at, updated_at (only when it differs), comment_count, like_count, media{id, type (image|video)}, link{title, url}, url (the note's line.me link), " +
			"comments[] of: id, author, author_id, text, created_at, from_me (only when true; comments oldest first), " + timelineDownloadFields + ", " + timelinePlanFields,
	}
}

func albumsDoc() cmdDoc {
	return cmdDoc{
		Name: "albums",
		Usage: "line albums <chat_id|name> [--id ALBUMID [--limit N]] | line albums download <chat_id|name> --id ALBUMID [--photo PHOTOID ...] [--limit N] | " +
			"line albums add --to <chat_id|exact-name> (--title T | --id ALBUMID) [--file PATH ...] (--dry-run | -y) [--allow-duplicate] | " +
			"line albums delete --to <chat_id|exact-name> --id ALBUMID [--photo PHOTOID ...] (--dry-run | -y)",
		Summary: "List a group's albums (相簿) and their photos, download photos, make an album, add photos, or delete an album or some of its photos. " + timelineOnline() + " " +
			"A first argument add, delete or download is the subcommand; give a group named like that by its chat_id. " +
			"albums <chat>: the group's albums with their counts. " +
			fmt.Sprintf("albums <chat> --id ALBUMID: its photos and videos, newest first (default %d, max %d). ", line.DefaultAlbumPhotosLimit, line.MaxAlbumPhotosLimit) +
			"albums download <chat> --id ALBUMID: its newest --limit photos and videos, or those named with --photo, into %LOCALAPPDATA%\\linecli\\downloads, named and reused like download (same statuses). " +
			fmt.Sprintf("add --title T makes a new album (LINE posts a notice of it in the chat) and adds the --file photos to it; add --id ALBUMID adds photos to an album. Photos are JPEG or PNG files of at most 20 MB, at most %d a run, all checked before anything is sent (otherwise INVALID_ARGUMENT); a JPEG goes without its location (gps_removed), as LINE's apps send it. ", line.MaxAlbumUploadFiles) +
			"When an album was made, or some photos added, but a later step failed, the output still lists every file with its status (sent|failed|uncertain|not_sent), error and message at the top level, and exits 1 (with --out the file is still written): never redo what was sent, and check an uncertain photo with albums <chat> --id ALBUMID before adding it again. " +
			"delete --id ALBUMID deletes the whole album with its photos, or with --photo only those photos; it cannot be undone, and LINE decides whether you may (SEND_FAILED when it refuses). With --photo, deleted is how many LINE deleted; fewer than asked is error SEND_FAILED with sent:true and exit 1. " +
			timelineWrites + "Albums (相簿) tab in LINE once, then retry. DUPLICATE_SEND: an album with the same title was made in the same group, or the same photos were added to the same album, in the last 10 minutes; --allow-duplicate only for an intended repeat (it also allows the same photo twice in one run). " +
			"Not supported: adding videos, renaming an album. " +
			fmt.Sprintf("At most %d pages of photos are read per run. ", 20) + timelinePacing + " " + albumsTextColumns + " " + untrusted,
		Flags: []string{
			"--id ALBUMID (one album: its photos; for download, add and delete: the album)",
			fmt.Sprintf("--limit N (photos of one album, newest first; default %d, max %d)", line.DefaultAlbumPhotosLimit, line.MaxAlbumPhotosLimit),
			"--photo PHOTOID (download, delete: only this photo of the album; repeat for more)",
			"--to chat_id|exact-name (add, delete: the group)",
			"--title T (add: make a new album with this title, one line of at most 100 characters)",
			fmt.Sprintf("--file PATH (add: a JPEG or PNG photo of at most 20 MB; repeat for more, up to %d; wildcards such as *.jpg work)", line.MaxAlbumUploadFiles),
			"--dry-run (show it, write nothing)",
			"-y/--yes (confirm a real write)",
			"--allow-duplicate (add: even if an album with the same title was made, or the same photos were added to the album, in the last 10 minutes, or a photo is given twice)",
		},
		Output: "array of albums: id, title, photo_count, image_count, video_count, created_at, updated_at, url (the album's line:// link, which only LINE's app opens), " +
			"(with --id an array of photos, newest first, of) id, type (image|video), uploader, uploader_id (only with --ids or where two people share a name), created_at, shot_at, width, height, from_me (only when true), " +
			timelineDownloadFields + ", " + timelinePlanFields,
	}
}

func notesHuman() humanCmd {
	return humanCmd{name: "notes", group: groupOnline, args: "<chat>",
		short: [2]string{"List group notes (preview additions, comments or deletions with --dry-run, then confirm with -y)", "列出群組記事（新增、留言或刪除前先用 --dry-run 預覽，確認後加 -y）"},
		usage: []string{
			"notes <chat> [--limit N] [--comments N] [--since TIME] [--until TIME] [--cursor C]",
			"notes <chat> --id <id> [--comments N]",
			"notes download <chat> --id <id>",
			"notes add --to <chat_id> (--message TEXT | --message-file FILE | --stdin) (--dry-run | -y)",
			"notes comment --to <chat_id> --id <id> (--message TEXT | --message-file FILE | --stdin) (--dry-run | -y)",
			"notes delete --to <chat_id> --id <id> [--comment <id>] (--dry-run | -y)",
		},
		long: [2]string{"Read a group's notes (newest first) and their comments, save a note's photos, add a text note, comment on a note, or delete your own note or comment. LINE PC does not store notes, so this goes online every time; LINE need not be running. Groups only. Notes with photos and polls cannot be made from a computer.",
			"讀取群組記事本（新的在前）與留言、下載記事的照片、新增文字記事、留言，或刪除自己的記事或留言。LINE 電腦版不會保存記事本，所以每次都會連網讀取；LINE 不必開著。只限群組。電腦版無法建立附照片的記事或投票。"},
		flags: []humanFlag{
			humanLimit([2]string{"notes", "則記事"}, line.DefaultNotesLimit),
			{"--comments N", [2]string{"Also show each note's newest N comments", "每則記事一併列出最新 N 則留言"}},
			{"--since TIME", [2]string{"Notes created from this time", "從這個時間之後建立的記事"}},
			{"--until TIME", [2]string{"Notes created up to this time", "到這個時間為止建立的記事"}},
			{"--cursor C", [2]string{"Continue where a run that read its page limit stopped", "從上次讀到頁數上限的地方繼續讀"}},
			{"--id <id>", [2]string{"One note (from the list)", "指定一則記事（id 來自列表）"}},
			{"--to <chat_id>", [2]string{"The group to write in", "要寫入的群組"}},
			{"--message TEXT", [2]string{"The text", "文字內容"}},
			{"--message-file FILE", [2]string{"Read the text from a UTF-8 file", "從 UTF-8 檔案讀取文字"}},
			{"--stdin", [2]string{"Read the text from standard input", "從標準輸入讀取文字"}},
			{"--comment <id>", [2]string{"Delete this comment of the note instead", "改為刪除這則記事的這則留言"}},
			{"--dry-run", [2]string{"Show what would be done, without doing it", "只顯示內容，不實際執行"}},
			{"-y, --yes", [2]string{"Do it", "確定執行"}},
			{"--allow-duplicate", [2]string{"Even if the same text went there in the last 10 minutes", "10 分鐘內寫過相同內容時，仍然執行"}},
		}}
}

func albumsHuman() humanCmd {
	return humanCmd{name: "albums", group: groupOnline, args: "<chat>",
		short: [2]string{"List group albums and download photos (preview changes with --dry-run, then confirm with -y)", "列出群組相簿或下載照片（新增或刪除相簿、照片前先用 --dry-run 預覽，確認後加 -y）"},
		usage: []string{
			"albums <chat> [--id <id> [--limit N]]",
			"albums download <chat> --id <id> [--photo <id> ...] [--limit N]",
			"albums add --to <chat_id> (--title TITLE [--file PATH ...] | --id <id> --file PATH [--file PATH ...]) (--dry-run | -y)",
			"albums delete --to <chat_id> --id <id> [--photo <id> ...] (--dry-run | -y)",
		},
		long: [2]string{"List a group's albums and their photos, download photos, make an album and add JPEG or PNG photos to it, or delete an album or some of its photos. LINE PC does not store albums, so this goes online every time; LINE need not be running. Groups only.",
			"列出群組的相簿與照片、下載照片、建立相簿並加入 JPEG 或 PNG 照片，或刪除相簿或其中幾張照片。LINE 電腦版不會保存相簿，所以每次都會連網讀取；LINE 不必開著。只限群組。"},
		flags: []humanFlag{
			{"--id <id>", [2]string{"One album (from the list)", "指定一本相簿（id 來自列表）"}},
			humanLimit([2]string{"photos", "張"}, line.DefaultAlbumPhotosLimit),
			{"--photo <id>", [2]string{"Only this photo; repeat for more", "只處理這張照片；要多張就重複指定"}},
			{"--to <chat_id>", [2]string{"The group to write in", "要寫入的群組"}},
			{"--title TITLE", [2]string{"Make a new album with this title", "用這個名稱建立新相簿"}},
			{"--file PATH", [2]string{"A JPEG or PNG photo to add; repeat for more (up to 50)", "要加入的 JPEG 或 PNG 照片；要多張就重複指定（最多 50 張）"}},
			{"--dry-run", [2]string{"Show what would be done, without doing it", "只顯示內容，不實際執行"}},
			{"-y, --yes", [2]string{"Do it", "確定執行"}},
			{"--allow-duplicate", [2]string{"Even if an album with this title was just made, the same photos were just added, or a photo is given twice", "剛建立過同名相簿、剛加過同樣的照片，或同一張照片給了兩次時，仍然執行"}},
		}}
}
