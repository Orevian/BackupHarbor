## What changed and why

## How it was verified

- [ ] `powershell -File scripts\build.ps1 -SkipInstaller` succeeds (gofmt, vet, tests, build)
- [ ] Tests added or updated for the changed behaviour
- [ ] No path turns "not tested" into "passed"
- [ ] Text added to **both** `en` and `tr` catalogs
- [ ] Nothing logs, stores or prints secrets
- [ ] `CHANGELOG.md` updated (Unreleased)
- [ ] Documentation updated if behaviour or configuration changed
