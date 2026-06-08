mj releases are tag-driven.

CI runs on ubicloud for every push and pull request. It runs `gofmt`, `go vet`,
offline `go test`, and builds both binaries.

GoReleaser owns release creation. A pushed `vX.Y.Z` tag builds `mj` and
`mj-mcp` for darwin/linux amd64/arm64, publishes a GitHub release, and uploads:

- four binary tarballs
- `checksums.txt`
- `Formula/mj.rb`

After GoReleaser succeeds, the workflow renders `Formula/mj.rb` from
`dist/checksums.txt`, uploads that formula to the GitHub release, and copies it
into `github.com/ehmo/homebrew-mj` using the `HOMEBREW_TAP_GITHUB_TOKEN`
repository secret.

The release body comes from the matching `## vX.Y.Z` section in `CHANGELOG.md`.

The repositories at `github.com/ehmo/mj` and `github.com/ehmo/homebrew-mj` stay
private until the release is ready for public visibility. Flip both before
announcing the release.

```bash
gh repo edit ehmo/mj --visibility public
gh repo edit ehmo/homebrew-mj --visibility public
```

## Local dry run

Run this before tagging:

```bash
rm -rf dist
gofmt -l .
go vet ./...
go test ./...
goreleaser release --snapshot --clean
```

The snapshot build writes archives to `./dist` and does not upload anything.

## Cut a release

1. Update `CHANGELOG.md`.
2. Commit on `main`.
3. Tag and push:

```bash
git tag -a vX.Y.Z -m "mj vX.Y.Z"
git push origin main
git push origin vX.Y.Z
```

The tag push starts `.github/workflows/release.yml`. Do not create the GitHub
release by hand. If the release run fails after creating assets, delete the
failed release or conflicting assets, then push a corrected tag.

## Homebrew

The tap is `github.com/ehmo/homebrew-mj`. The formula is rendered from release
checksums and copied into the tap on each tag. Users install with:

```bash
brew install ehmo/mj/mj
mj doctor
mj login --i-understand
```
