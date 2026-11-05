# Development

Build, test and release workflow of [strspc-manager](../README.md).

## Commands

```bash
make setup   # install the pre-commit hooks
make build   # build the strspc-manager binary
make test    # run the tests
make cover   # run the tests with the race detector and the coverage floor
make lint    # run golangci-lint
make fmt     # format the code
```

CI runs `make cover`, which fails any package under 85% statement coverage (GOTEST-001). See
[`AGENTS.md`](../AGENTS.md) for the agent workflow and the
[SteerSpec contributing guide](https://github.com/SteerSpec/.github/blob/main/CONTRIBUTING.md) for
everything else.

## Branching and releases

`develop` is the default branch and collects the changes for the next release;
`main` only moves when a release is prepared:

1. Pull requests target `develop` and are **squashed**. The pull request title
	becomes the commit release-please reads, so it decides the release note and
	the version bump. A merge commit would list the change twice.
2. A `chore: promote develop to main` pull request carries the bundle to `main`.
	Merge it with a **merge commit**: release-please reads the individual
	conventional commits, and a squash would hide them behind a single `chore:`.
3. release-please opens its release pull request against `main`; merging it tags
	the version and GoReleaser publishes it.

> [!IMPORTANT]
> Promote `develop` before merging a release pull request. release-please stops
> reading at the last release commit, by date, so a `feat:` or `fix:` left on
> `develop` while a release merges is dropped from the next release.

The `Release changelog` workflow catches a dropped commit. After every Release
Please run, and on each release pull request, it lists the commits `main`
gained since the last release by ancestry, keeps the types the changelog
shows, and checks that `CHANGELOG.md` names each one. Its verdict is the
`release-changelog` status on the release pull request; with no release pull
request open, the workflow run itself fails. The status is required on `main`,
so every other pull request into `main`, such as a promotion, gets a passing
`release-changelog` status from the same workflow. To restore a dropped commit, add
it to the body of the latest promotion pull request and re-run Release Please:

```text
BEGIN_COMMIT_OVERRIDE
fix(remote): stop a crafted tar size overflowing the extraction cap (#202)
END_COMMIT_OVERRIDE
```

release-please reads that block in place of the promotion's merge commit, so
list each skipped commit, one per paragraph, ending in its `(#<pr>)`.

The `merge-method-develop` and `merge-method-main` rulesets enforce both merge
methods. `develop` is not back-merged, so it stays behind `main` by the release
commits.
