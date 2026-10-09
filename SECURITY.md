# Security Policy

## Supported versions

BackupHarbor has not had a public release yet. Once releases exist, security fixes are
made for the latest release.

## Reporting a vulnerability

Please report suspected vulnerabilities **privately**, not in a public issue:

- Use GitHub's private vulnerability reporting ("Security" tab, "Report a vulnerability") on the repository, or
- contact the maintainers by e-mail at the address listed on the maintainers' GitHub profiles.

Include the version, Windows version, steps to reproduce, and the impact you expect. Please do not include
real repository passwords or backup contents. We aim to acknowledge reports within a few days and to
coordinate a fix and disclosure with you.

## Scope

In scope: data loss or modification of repositories or user files, escaping the work area (path traversal,
symlink or junction handling), leaking passwords or other secrets into logs, reports or command lines,
unsafe handling of subprocess output, HTML report injection, unsafe scheduled-task handling.

Out of scope: attacks that already require code execution as the same Windows user (that user can read
their own Credential Manager entries and environment), and vulnerabilities in restic, WebView2 or Windows
themselves (report those upstream).

## Design summary

See [docs/security.md](docs/security.md) for the protections and the password-source trade-offs.
