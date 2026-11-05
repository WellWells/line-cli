# Contributing

## Scope

This repository holds the open edition of line-cli. It reads and searches local LINE data and decrypts locally cached media.

The code that talks to LINE's servers (`send`, `react`, `unsend`, `download`, OpenChat sync) and the update check are not included. To use those features, install an [official release](https://github.com/WellWells/line-cli/releases/latest).

## Build and test

Requires Windows x64 and Go 1.27.3 or later. No C compiler is needed.

```powershell
git clone https://github.com/WellWells/line-cli
cd line-cli
go build -o linecli.exe ./cmd/linecli
go vet ./...
go test ./...
```

Tests use synthetic data and do not need LINE.

## Guidelines

- Run `gofmt -w <file>` after editing Go files.
- Write code comments in English.
- Keep JSON on stdout and notes on stderr. When output fields change, update `line help --json`.
- Do not put real chat content, databases, keys or tokens in tests, issues or pull requests.
- Use `type(scope): summary` for commit messages, for example `fix(cli): reject an empty --to`.

## Pull requests

Describe what changed, which commands you ran and their results. If output changes, include a redacted example.

To report a security issue, see [SECURITY.md](SECURITY.md).
