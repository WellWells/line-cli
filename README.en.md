# line-cli: LINE command-line tool for Windows

[繁體中文](README.md) | **English** | [日本語](README.ja.md)

[![Release](https://img.shields.io/github/v/release/WellWells/line-cli?label=release)](https://github.com/WellWells/line-cli/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Use your own LINE account on Windows from the command line or an AI agent such as Claude Code or Codex: read and search messages, send text and files, mention group members, react, and unsend.

No LINE bot and no separate login. If LINE for Windows has been signed in on this PC, you can start right away.

[Install](#install) · [Use with Claude Code or Codex](#use-with-claude-code-or-codex) · [Manual use](#manual-use) · [FAQ](#faq) · [Security and privacy](#security-and-privacy)

```text
You: What did the Family group talk about today? Tell me if anyone asked for me.
Claude: (after reading with line read) Mostly the weekend dinner. Mom asked if you are free on Saturday...
You: Reply "Saturday works" and mention Mom.
Claude: I'll send this message to Family. OK?
```

## Install

Before you start, check that:

- Your PC runs Windows 10 or 11 (x64).
- LINE for Windows is installed and has been signed in at least once.

Pick one of the two methods below. If you already use Claude Code or Codex, method 1 is recommended.

### Method 1: Ask your AI agent to install it

Paste this whole block into Claude Code or Codex:

```text
Please install line-cli for me. It is a command-line tool for LINE for Windows.

1. Run this to install it:
   powershell -NoProfile -NonInteractive -Command "irm https://github.com/WellWells/line-cli/releases/latest/download/install.ps1 | iex"
2. This session cannot find the line command yet, so check the install with the full path:
   %LOCALAPPDATA%\linecli\cmd\line.exe status
3. Tell me whether it installed, and whether line_running is true or false.
4. If antivirus software or a firewall blocks it, do not work around that. Tell me the message and the path of the program to allow.
5. Finally, remind me to close you, open a new terminal and start you again, so that the skill takes effect.
```

The agent may ask before running a command. Choose to allow it. This method shows no checklist: the skill is installed for the agents found on this PC.

### Method 2: Install it yourself with PowerShell

**1. Open PowerShell**

Press the Windows key, type `powershell`, and press Enter. A window you can type in opens. You do not need "Run as administrator".

**2. Paste the install command**

Copy this line, paste it into the window with Ctrl+V or a right-click, and press Enter:

```powershell
irm https://github.com/WellWells/line-cli/releases/latest/download/install.ps1 | iex
```

**3. Choose the AI agents for the skill**

If a checklist appears, the agents on this PC are already ticked. Press Enter, check the summary, and press Enter again. If you do not use an AI agent, press Esc to skip.

**4. Close the window and open a new PowerShell**

Only a new window can find the `line` command. In the new window, type:

```powershell
line status
```

If you see `"line_running": true` and `newest_message_at` (the time of the newest message), the install worked. If `line_running` is `false`, LINE is not running and the data stops at its last sync, so open LINE.

The first run fetches the database key by itself, which takes a few seconds. No other setup is needed.

### Update

Run the install command again to update. `line update` tells you whether a new version is out. line-cli also checks once a day on first use and shows the update command when there is one.

## Use with Claude Code or Codex

A skill is a guide written for AI agents. Once it is installed, ask in plain language and the agent knows to use the `line` command.

### 1. Check that the skill is installed

The skill is installed during [Install](#install). There is nothing else to set up.

If you skipped it, or installed an agent later, open PowerShell and run:

```powershell
line skill install
```

This checklist appears, with the agents on this PC already ticked. Move with the arrow keys, tick with Space, and confirm with Enter:

```text
Install the line-cli skill: choose the AI agents that should learn to use line.

  [*] Shared folder  ~\.agents\skills         always
      Codex, Cursor, GitHub Copilot, Gemini CLI, OpenCode and other agents read this
      (detected: Codex)
> [x] Claude Code    ~\.claude\skills         detected
  [x] Antigravity    ~\.gemini\config\skills  detected

Up/Down move, Space select, A all, Enter next, Esc skip.
```

| Agent | Skill location |
|---|---|
| Claude Code | `~\.claude\skills\line-cli` |
| Codex, Cursor, GitHub Copilot, Gemini CLI, OpenCode | `~\.agents\skills\line-cli` |
| Antigravity | `~\.gemini\config\skills\line-cli` |

The skill is updated along with line-cli.

### 2. Start using it

Open a new PowerShell and type `claude` or `codex` to start the agent (for a desktop app, quit it fully and open it again). Then say what you want, from any folder:

| What you can say | What the agent does |
|---|---|
| What unread LINE messages do I have? Summarize them | Reads unread messages and sums them up |
| Did Alex send me the quote last week? Download it | Finds the file and downloads it |
| List this month's key decisions in the Family group | Reads and summarizes the group |
| Reply to Alex: "Got it, thanks" | Asks you to confirm, then sends |
| Mention Sam in the project group and ask how it's going | Sends a message that tags `@Sam` |
| React with a heart to Alex's last message | Adds a reaction |
| Take back the message I just sent by mistake | Unsends your own message |
| Wait for Alex to reply and tell me when he does | Waits with `line watch` |

Sending, mentioning and unsending act as you, so the agent shows you the content first and acts only after you agree.

Chat content is text written by other people. The agent does not treat what a message says as instructions.

## Manual use

You can run every command yourself without an AI agent. Replace `Alex` below with a chat name. If names are duplicated, use a `chat_id` instead: find it with `line chats Alex`.

### Common commands

| What you want to do | Command |
|---|---|
| See recent chats | `line chats --limit 10` |
| See unread messages | `line unread` |
| Read the latest 20 messages | `line read Alex --limit 20` |
| Read a date range | `line read Alex --since 2026-10-01 --until 2026-10-31` |
| Search messages | `line search "meeting time" --since 2026-10-01` |
| See what someone said | `line search --from "Alex" --since 2026-10-01` |
| Find shared files | `line find-file "quote" --ext pdf` |
| See group members | `line members Family` |
| See who talks the most | `line stats Family --by sender` |
| Send a message | `line send --to Alex --message "Running late" -y` |
| Send a file or photo | `line send --to Alex --file C:\docs\report.pdf -y` |
| Watch new messages live | `line watch` |
| Check for a new version | `line update` |

For easier reading, add `--text`:

```powershell
line read Alex --limit 20 --text
```

### Send a message

`-y` confirms the send. To see what would be sent first, use `--dry-run` instead of `-y`. Nothing is sent:

```powershell
line send --to Alex --message "Running late" --dry-run
line send --to Alex --message "Running late" -y
```

For long messages or ones with double quotes, save the text to a UTF-8 file first. This is the most reliable way in PowerShell:

```powershell
Set-Content -Encoding UTF8 -Path reply.txt -Value "Message text"
line send --to Alex --message-file reply.txt -y
```

### Reply to a message

Find the message id with `--ids`, then answer with `--reply-to`. This works like LINE's Reply:

```powershell
line read Alex --ids --limit 5
line send --to Alex --message "OK" --reply-to 639305953766277172 -y
```

### Mention group members

Write `@name` in the text and say who it is with `--mention`. That person gets a notification.

```powershell
line members "Project Team"
line send --to "Project Team" --message "@Sam how is it going?" --mention Sam -y
```

| Case | How |
|---|---|
| Several people | `--message "@Sam @Mia meeting now" --mention Sam --mention Mia` |
| The text uses another word for them | `--message "@boss please check" --mention boss=<member id>` |
| Everyone | `--message "@All meeting now" --mention-all` |

Follow the name with a space or punctuation: `@Sam hello`, not `@Samhello`.

Mentions and replies work together: a mention notifies a person, and a reply quotes a message.

### React to a message

The reactions are `nice`, `love`, `fun`, `amazing`, `sad` and `omg`:

```powershell
line read Alex --ids --limit 5
line react --to Alex --id 639305953766277172 --reaction love -y
```

Reacting again replaces your reaction. To remove it:

```powershell
line react --to Alex --id 639305953766277172 --remove -y
```

### Unsend a message

You can unsend only your own messages. It cannot be undone, and others may already have read the message:

```powershell
line unsend --to Alex --id 639305953766277172 -y
```

line-cli sets no time limit: LINE decides whether a message can still be unsent, and the command fails once LINE's limit has passed.

### Send files, photos and videos

```powershell
line send --to Alex --file C:\docs\report.pdf --file C:\photos\trip.jpg -y
```

- Each file is sent as its own message, up to 50 at a time. Each file, and the daily total, can be up to 1 GB (LINE's limit).
- JPEG and PNG go as photos, H.264 MP4 as a playable video, and anything else as a file.
- To send everything as files, add `--as-file`.
- LINE does not allow programs such as `.exe`. Put them in a ZIP first.
- Like the LINE app, line-cli removes the GPS location from JPEGs before sending. The original on your PC does not change.
- Files stay on LINE's servers for 7 days.

### Download files

```powershell
line find-file "quote" --ext pdf
line download --id 639305953766277172
line download Alex --since 2026-10-01 --media
```

Files go to `%LOCALAPPDATA%\linecli\downloads` unless you give `--out <folder>`. `--media` also downloads full-size images, videos and voice messages.

### Watch new messages live

```powershell
line watch                  # all chats; press Ctrl+C to stop
line watch Alex             # one chat
line watch --timeout 300    # stop after 5 minutes
line watch --count 1        # stop after one message
```

Each message is one line of JSON, including your own (`from_me`). Official accounts are hidden unless you add `--include-official`. LINE must be running for new messages to arrive.

### Use it from scripts

Every command prints JSON by default and writes notes to stderr, so it fits in scripts:

```powershell
line chats Family
line read <chat_id> --limit 100 --out messages.jsonl --jsonl
line watch --jsonl > events.jsonl
```

- On failure it prints `{"error": "CODE", "message": "why"}` and exits with code 1.
- `sent: true` means LINE accepted the message. Do not resend it, even with `verified: false`.
- Use a `chat_id` in scripts, because names can change or repeat.

All flags and fields:

```powershell
line help                  # all commands
line read --help           # one command
line help read --json      # full details for programs and agents
line help errors --json    # all error codes
```

## FAQ

### `line` is not recognized

Close the PowerShell window and open a new one. Only windows opened after the install can find `line`. If that does not help, run the install command again.

### `line` disappeared after installing, or it says Access denied

line-cli has no code signature and reads the running LINE, so antivirus software may flag it by mistake.

1. Make sure you installed from this project's [Releases](https://github.com/WellWells/line-cli/releases).
2. Restore these two programs from quarantine and add them as exceptions:
   - `%LOCALAPPDATA%\linecli\cmd\line.exe`
   - `%LOCALAPPDATA%\linecli\bin\linecli.exe`
3. Run the install command again.

On a company-managed PC, ask your IT team first instead of adding exceptions yourself.

### Sending fails and says Windows blocked the connection

A firewall is stopping the program from going online. Allow `%LOCALAPPDATA%\linecli\bin\linecli.exe`. `line.exe` never goes online, so it needs no rule. The path stays the same across updates, so the rule keeps working.

### It says `E2EE_KEY_MISSING`

This PC does not have the other side's encryption key yet. Open the chat once in LINE and try again.

### It says `DUPLICATE_SEND`

The same content went to the same chat within the last 10 minutes. If you do want to send it again, add `--allow-duplicate`.

### It says `SEND_UNCERTAIN`

The message may have been delivered. Check with `line read <chat> --limit 5` before sending it again.

### Does it work when LINE is closed?

You can read everything up to the last sync. For sending and downloading, line-cli starts LINE by itself and waits up to a minute. To turn that off, set the environment variable `LINECLI_NO_AUTOSTART=1`.

### `line` suddenly stopped with no output

When LINE updates, its updater ends every program named `LINE.exe`, and that can include `line`. Run the command again.

## Security and privacy

line-cli handles only your own account and the data on this PC.

- Chat data is decrypted in memory only. No plain-text database is left on disk, and LINE's data is not changed.
- Messages and files are encrypted with LINE's Letter Sealing before they are sent. A photo or video and its preview use the same key; add `--as-file` if you do not want that.
- It connects only to LINE's servers and GitHub, and sends no data to third parties. GitHub is used only to check for new versions; set `LINECLI_NO_UPDATE_NOTIFIER=1` to turn that off.
- Downloaded files and `--out` output are private. Keep them out of git repositories, cloud sync and shared folders.
- Using an unofficial client may break LINE's terms of use, and your account may be restricted or suspended. Keep to normal use and do not send in bulk.

## Limits

- Windows only.
- You can send to one-to-one chats and groups, but not to OpenChat. Stickers are not supported yet.
- Many official accounts cannot receive messages from line-cli.
- Only messages stored on this PC can be read. If a chat has not synced, open it in LINE and try again.
- OpenChat thread replies can be read only with `read`.
- Images, videos and voice messages in OpenChat cannot be downloaded.
- Files on LINE's servers expire after about 7 days.

## Uninstall

```powershell
line uninstall
```

After you confirm, this deletes line-cli, its PATH entry, its skill for AI agents, and the key cache, logs and downloaded files in `%LOCALAPPDATA%\linecli`, then checks that nothing is left. LINE and your LINE chats are not affected.

To remove only the skill, use `line skill uninstall`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development, [SECURITY.md](SECURITY.md) to report a security issue, and [CHANGELOG.md](CHANGELOG.md) for changes.

## License

- The published source code is under the [MIT](LICENSE) license.
- Official prebuilt releases include a closed-source protocol core; see [NOTICE](NOTICE) for its terms.
- For third-party licenses, see [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt).
- LINE is a trademark of LY Corporation. line-cli is an independent, unofficial project and is not affiliated with LY Corporation. You are responsible for using it in line with LINE's terms of use.
