# Security Policy

## Reporting a vulnerability

Do not post vulnerability details in a public issue. Report them privately through [GitHub Security Advisories](https://github.com/WellWells/line-cli/security/advisories/new), and include:

- The version (`line version`)
- Steps to reproduce
- The possible impact

Do not attach chat content, databases, keys or tokens. Use synthetic data.

Reports are confirmed first. Details are published after a fix is available.

## Supported versions

Only the latest release is supported. Run `line update` to check whether you have it.

## Scope

These count as security issues:

- line-cli sends chat content, keys or tokens anywhere other than LINE's servers and GitHub, or writes them where it should not
- Chat content (messages, names, file names) can make line-cli, or an agent using it, do something unintended
- The install script or update check can be used to run an unofficial program
- `line uninstall` deletes files that do not belong to line-cli

line-cli reads data under the Windows account that runs it. Anyone who can already run programs as that user can read the same data, so that is out of scope.
