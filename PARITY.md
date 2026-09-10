# Shiki compatibility

The implemented target is the **native Go library and CLI surface of Shiki
4.4.3**, using reference checkout
`48cd2cc695ed2e3357c3f9c370578ea843d6d9a3`.

The Go implementation runs without Node.js or cgo. It uses the upstream
Oniguruma WebAssembly binary through wazero and embeds the complete language
and theme catalogs. A portable Go development tool handles builds, validation,
generation, and diagnostics. Node.js is used only by its upstream reference
bridge when refreshing assets or fixtures.

## Implemented surface

- [x] 242 public languages, 260 grammar assets, aliases, dependencies, lazy embeddings
- [x] 65 bundled themes and custom TextMate / VS Code themes
- [x] Oniguruma scanning, Unicode byte offsets, and anchor handling
- [x] TextMate repositories, captures, retokenization, injections, begin/end/while, backreferences, and grammar states
- [x] Theme scope matching, parent selectors, font styles, explanations, token types, and non-hex CSS colors
- [x] Base tokens, multi-theme variants and alignment, color replacements, CSS variables, and `light-dark()`
- [x] HTML and HAST, classic and inline structure, whitespace controls, and adjacent-token merging
- [x] Transformer lifecycle and every transformer in `@shikijs/transformers`
- [x] Bracket coloring from `@shikijs/colorized-brackets`, including scopes, language overrides, palettes, and multiple themes
- [x] Decorations, nested and multiline ranges, position conversion, and token splitting
- [x] ANSI input and terminal rendering at 16-color, 256-color, and true-color levels
- [x] Stateful tokenization and the `@shikijs/stream` stable/unstable recall protocol
- [x] HAST serialization options, HTML/SVG attributes, comments, raw nodes, templates, optional tags, and character references
- [x] CLI files, stdin, HTTP/HTTPS inputs, language inference, catalogs, and output formats
- [x] Custom regex-engine interface, explicit resource disposal, shared engine ownership, and concurrent highlighter calls
- [x] Runnable examples, API usage documentation, asset checksums, and third-party notices

The registry preserves the reference's lazy-load injection-cache behavior.
Token and position offsets use UTF-16 code units. The regex-engine interface
uses UTF-8 byte offsets. JSON-decoded options preserve theme, replacement,
metadata, attribute, and decoration property ordering where output depends on it.

## Differential verification

The included fixtures were regenerated from the pinned npm packages. Tests
compare token JSON, HTML bytes, HAST JSON, CSS class registries, grammar-state
metadata, or streaming events as appropriate.

| Fixture set | Cases | Coverage |
| --- | ---: | --- |
| `catalog.json` | 242 | Every public language: tokens and HTML |
| `parity.json` | 142 | Core languages, all 65 themes, render options, explanations, decorations |
| `realworld.json` | 40 | Larger samples across 20 languages and special inputs, with scope explanations |
| `advanced.json` | 40 | 25 custom grammar/theme/state cases, 3 normalization cases, 12 ANSI output cases |
| `transformers.json` | 37 | Bundled transformers, bracket coloring, custom options, inline combinations |
| `stream.json` | 12 | Chunk boundaries, recalls, Markdown, multiple themes, Unicode, and CRLF |
| `hast.json` | 336 | Serialization formatting and HTML/SVG node/property combinations |
| **Total** | **849** | All pass |

Additional native tests cover ownership and disposal, errors, concurrent use,
state isolation, transformer hooks, token JSON round trips, HTTP CLI inputs,
reader chunking, and executable examples.

## Validation performed

On Windows amd64 with Go 1.27.1:

- `go test ./... -count=1`: passed after clean fixture regeneration.
- `go vet ./...`: passed.
- CLI and development-tool builds with `CGO_ENABLED=0`: Windows, Linux, and
  macOS on amd64 and arm64.
- Windows executable: highlighted JavaScript with an empty `PATH`.
- Fixture/asset regeneration: passed using `go run ./tools generate`.
- Tooling migration: all seven reference fixture files remain byte-identical;
  all 325 decompressed grammar/theme files, catalog, HAST properties, and WASM
  retain their original content. Go's gzip output has updated checksums.
- Portable development tool: builds with `CGO_ENABLED=0` for Windows, Linux,
  and macOS on amd64 and arm64; local data commands work with an empty `PATH`.

Linux and macOS binaries were cross-compiled, not executed on those operating
systems in this workspace. The local Go race detector was not run because no C
compiler is installed. The included CI workflow runs tests and vet on Windows,
Linux, and macOS, plus the race detector on Linux; that remote workflow has not
been executed here.

## Native API adaptations and boundaries

Go uses explicit errors, structs, interfaces, and `Close` rather than promises,
JavaScript function overloads, and garbage-collection hooks. The highlighter's
syntax stack remains in memory; JSON grammar-state output is diagnostic metadata,
not a serialized resumable stack. A state is tied to its originating grammar
instance and must be refreshed after that grammar is reloaded.

Go maps do not have insertion order. Native callers can specify `ThemeOrder`
and `ColorReplacementOrder`; JSON input preserves the source order. See the
README for deterministic defaults. CSS style strings use `RawStyle`.

The default 500 ms line budget is checked between regex searches. It cannot
interrupt a single Oniguruma search, matching the reference's budget model.
Differential tests disable that budget to avoid machine-load-dependent results.

JavaScript regex-engine emulation, JavaScript bundle entry points, browser
framework adapters, Monaco integration, bundler plugins, and TypeScript compiler
services such as Twoslash are outside the native library and CLI target. The
grammars for those languages and frameworks are included.

The suite establishes compatibility for the tested inputs; it is not an
exhaustive proof over every possible third-party grammar or transformer.
