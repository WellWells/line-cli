# Changelog

Versions follow `v1.x.y`: x for new features or other changes, y for bug fixes only.

## 1.3.0 - 2026-11-26 14:50

### Features

- notes: read, write and download group notes and albums

### Fixes

- send: blank EXIF GPS anywhere in a JPEG

## 1.2.0 - 2026-11-15 23:29

### Features

- update: ask before installing a newer version, -y to skip

## 1.1.1 - 2026-11-15 22:56

### Fixes

- send: skip expired access tokens and use the live one

## 1.1.0 - 2026-11-06 16:03

### Other

- readme: add English and Japanese READMEs
- readme: make English the main README
- make English the primary language of project docs

## 1.0.0 - 2026-11-05 22:40

First public release.

### Features

- Read local LINE data: `chats`, `read`, `unread`, `search`, `stats`, `members`, `contacts`, `find-file`, `status`
- View and download images and files: `preview`, `download`
- Send text, files, photos and videos with `send`; reply to a message with `--reply-to`
- Mention group members with `send --mention` and `send --mention-all`
- React to and unsend messages: `react`, `unsend`
- Follow new messages live: `watch`
- Install a skill for AI agents: `skill install`
- Check for new versions with `update`, and automatically once a day
- Remove line-cli and its data: `uninstall`
