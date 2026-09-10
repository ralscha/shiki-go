# Portable development tool

Run `go run ./tools --help` from the checkout, or build a standalone executable:

```text
go run ./tools build --tool dev
go run ./tools build --tool dev --all
```

Outputs are named `bin/shiki-dev-OS-ARCH` (`.exe` on Windows). `--all` targets
Windows, Linux, and macOS, each on amd64 and arm64. `--tool all` also builds the
highlighting CLI. Both executables use `CGO_ENABLED=0` and need no shared libraries.

The development tool finds the checkout by walking up from its working
directory and then its executable directory. `shiki-dev --root PATH command`
selects a checkout explicitly; paths containing spaces are supported.

| Command | Purpose | External programs needed |
| --- | --- | --- |
| `test [arguments]` | Run `go test`; defaults to `./...` | Go |
| `vet [arguments]` | Run `go vet`; defaults to `./...` | Go |
| `check` | Verify asset checksums and module files, test, and vet | Go |
| `build [options]` | Build `shiki`, `dev`, or both; optionally cross-compile | Go |
| `manifest` | Record asset versions and SHA-256 checksums | None |
| `manifest --check` | Fail if an asset was changed, added, or removed | None |
| `dump LANGUAGE...` | Expand bundled grammar JSON into `.cache`; accepts aliases | None |
| `diff NAME...` | Compare `.cache/mismatches/NAME.got.json` and `.want.json` | None |
| `generate assets` | Import upstream grammars, themes, WASM, HAST metadata; update manifest | Node |
| `generate fixtures` | Refresh all upstream reference fixture suites | Node |
| `generate notices` | Copy npm and selected Go module license notices | Go |
| `generate manifest` | Same as `manifest` | None |
| `generate` | Refresh assets, fixtures, notices, and manifest | Go and Node |

The highlighting library, CLI, builds, and tests never invoke Node. The
development tool's Go commands use checkout-local caches by default and preserve
explicit `GOCACHE` and `GOTMPDIR` settings. Build outputs default to `bin`; use
`--out DIRECTORY` to choose another destination. `--os` and `--arch` accept Go's
target names, subject to the dependencies' platform support. Commands propagate
child-process failures and stop on interruption.

## Updating the upstream reference

An upstream upgrade has two parts: regenerate assets and reference results,
then port any changed behavior into Go. Grammar and theme updates can often be
handled through regeneration. Changes to tokenization, rendering, transformers,
or APIs generally require manual Go changes.

Run the following commands from the repository root. Updating requires Go,
Node.js supported by the selected upstream release, and npm.

1. **Review the upstream release.** Compare its changelog and source changes
   against the version in [reference/package.json](reference/package.json) and
   commit in [reference/source.json](reference/source.json). Identify new APIs,
   changed defaults, bug fixes, removed features, and grammar/theme changes.
   Establish a passing baseline with `go run ./tools check` before changing pins.

2. **Update the exact dependency pins and source commit.** Edit
   `tools/reference/package.json` for the selected Shiki release and its matching
   CLI, transformer, bracket-coloring, and streaming packages. Dependencies such
   as `@shikijs/vscode-textmate`, `tm-grammars`, `tm-themes`, and
   `vscode-oniguruma` have independent versions; select the versions appropriate
   to the upstream release instead of assigning them Shiki's version number.
   Record the release's full source commit in `tools/reference/source.json`.
   Then update installed packages and the lockfile:

   ```text
   npm --prefix tools/reference install --ignore-scripts
   ```

   Review the resulting `package-lock.json` changes. `npm ci` reinstalls the
   existing lockfile; it does not upgrade it. The generator reads the installed
   npm packages, so changing a separate Shiki source checkout alone does not
   update this library.

3. **Regenerate upstream assets and expected results.**

   ```text
   go run ./tools generate
   ```

   This imports grammars, themes, WASM, HAST metadata, license notices, and
   reference fixtures, then updates the asset manifest. Catalog coverage expands
   automatically to new upstream languages and themes. Review the generated
   changes. If generation reports obsolete assets, confirm the upstream removal,
   remove the reported files, and retry. Update explicit sample inputs if their
   languages, themes, or options were removed upstream.

4. **Port changed behavior and extend coverage.** Run `go run ./tools check`
   and fix Go differences against the new upstream results. Add inputs under
   `tools/reference/cases` for new features and upstream regression fixes, and
   extend `reference/bridge.mjs` and the Go tests when a new API needs coverage.
   Regenerate after changing reference inputs or the bridge. Existing tests can
   pass while a newly added upstream API is still missing, so review the release
   independently of test results. Expected outputs must come from upstream Shiki;
   do not replace them with the Go implementation's output to make tests pass.

5. **Validate the completed upgrade.**

   ```text
   go run ./tools check
   go run ./tools build --tool all --all
   ```

   `check` verifies asset checksums and `go mod tidy -diff`, runs the Go tests,
   and runs vet. The build command cross-compiles both executables for Windows,
   Linux, and macOS on amd64 and arm64. Cross-compilation does not execute tests
   on those platforms: also run the repository's CI matrix and Linux
   race-detector job before release.

6. **Record and release the new compatibility target.** Update
   [README.md](../README.md), [PARITY.md](../PARITY.md), and [NOTICE](../NOTICE)
   with the target version, source commit, supported features, fixture counts,
   and validation results where applicable. Commit the dependency pins and
   lockfile, source metadata, generated assets and manifest, notices, reference
   fixtures, tests, and Go changes together. Rebuild release binaries so they
   embed the updated assets.

To reproduce the already pinned reference without upgrading, use:

```text
npm --prefix tools/reference ci --ignore-scripts
go run ./tools generate
go run ./tools check
```

## How generation works

Asset, fixture, and notice refreshes require installed npm dependencies matching
`reference/package-lock.json`. The tool verifies installed package versions
before importing anything. The Go notice importer resolves actual module
versions and replacement directories through `go list -m -json`; it does not
hardcode Go dependency versions or module-cache paths.

`reference/cases/*.json` contains only test inputs and suite setup. The catalog
suite expands over every upstream language; the core suite also expands over
every upstream theme. `reference/bridge.mjs` invokes the pinned upstream APIs
and returns JSON over standard input/output. It performs no file generation and
does not invoke this Go implementation. Each suite gets a fresh process because
Shiki's loading order and mutable caches can affect results.

The bridge remains JavaScript so expected outputs come from the original Shiki
implementation. Replacing that oracle with this Go port would make the parity
checks circular. It is the only JavaScript program retained in the tools tree.

Go owns JSON formatting, deterministic gzip compression, WASM copying, license
copying, and the asset manifest. Generation computes all requested outputs
before replacing files, so a failed oracle leaves checked-in files intact.
Each file is replaced via a temporary file in its destination directory; the
whole refresh is not a filesystem transaction. Removed upstream assets cause an
error for review instead of silently surviving a version update.

Routine testing uses the checked-in fixtures and requires no npm packages.
