# shiki-go

A native Go library and CLI implementing Shiki's TextMate syntax highlighting.
The compatibility target is **Shiki 4.4.3**. All 242 public languages, supporting
grammars, 65 themes, and the Oniguruma WebAssembly binary are embedded.

Building from source requires **Go 1.27.1 or newer**. Highlighting runs
entirely in the Go process, using
[wazero](https://github.com/tetratelabs/wazero) for Oniguruma. No Node.js
installation, cgo, external executables, runtime downloads, or grammar files
are needed.

See [PARITY.md](PARITY.md) for the supported surface, reference comparisons,
and validation limits. Release history is recorded in
[CHANGELOG.md](CHANGELOG.md).

## Stability

Starting with `v1.0.0`, the exported APIs in the `shiki`, `stream`, and
`transformers` packages are stable. Releases within v1 will remain backward
compatible; an incompatible exported API change will require a new major
version.

## Installation

To use shiki-go in another Go program, run the following command from the
directory containing its `go.mod` file:

```console
go get github.com/ralscha/shiki-go@v1.0.0
```

If the program does not have a `go.mod` file yet, create one first with
`go mod init example.com/my-program`. The `go get` command records shiki-go and
its dependencies in the program's `go.mod` and `go.sum` files.

Then import it and highlight code:

```go
package main

import (
    "fmt"
    "log"

    shiki "github.com/ralscha/shiki-go"
)

func main() {
    html, err := shiki.CodeToHTML("const answer = 42", shiki.Options{
        Lang:  "javascript",
        Theme: "github-dark",
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(html)
}
```

Save the example as `main.go` and run it with `go run .`.

No separate grammar, theme, WebAssembly, Node.js, or cgo installation is
required. Go downloads the module dependencies, and shiki-go embeds its runtime
assets in the compiled program.

To install the command-line program instead, use `go install`:

```console
go install github.com/ralscha/shiki-go/cmd/shiki@v1.0.0
```

## Library

The module is published from [github.com/ralscha/shiki-go](https://github.com/ralscha/shiki-go):

```go
import shiki "github.com/ralscha/shiki-go"

html, err := shiki.CodeToHTML("const answer = 42", shiki.Options{
    Lang:  "javascript",
    Theme: "github-dark",
})
```

The package functions load requested assets into a shared highlighter. For
servers and repeated work, create a highlighter with an explicit lifetime:

```go
h, err := shiki.NewHighlighter(shiki.HighlighterOptions{
    Langs:  []string{"go", "typescript", "markdown"},
    Themes: []string{"github-dark", "github-light"},
})
if err != nil {
    return err
}
defer h.Close()

result, err := h.CodeToTokens("package main", shiki.Options{
    Lang:  "go",
    Theme: "github-dark",
})
```

Highlighter operations are safe to call concurrently. Each instance serializes
access to its grammar and regex caches; separate instances can highlight in
parallel. Applications must synchronize mutations of options, custom
transformers, and returned data they share between goroutines.

`LoadLanguage`, `LoadLanguageRegistrations`, and `LoadTheme` add assets after
construction. Custom languages and themes accept the corresponding TextMate
JSON via `encoding/json`. A supplied `RegexEngine` remains caller-owned.

| Shiki API | Go API |
| --- | --- |
| `createHighlighter` | `NewHighlighter` / `CreateHighlighter` |
| `codeToHtml`, `codeToHast` | `CodeToHTML`, `CodeToHAST` |
| `codeToTokens` | `CodeToTokens` |
| `codeToTokensBase` | `CodeToTokensBase` |
| `codeToTokensWithThemes` | `CodeToTokensWithThemes` |
| `getLastGrammarState` | `GetLastGrammarState` / `TokensResult.GrammarState` |
| `createCssVariablesTheme` | `CreateCSSVariablesTheme` |
| `normalizeTheme` | `NormalizeTheme` |
| `hastToHtml` | `HASTToHTML` |
| `splitToken`, `splitTokens` | `SplitToken`, `SplitTokens` |
| `createPositionConverter` | `CreatePositionConverter` |
| `guessEmbeddedLanguages` | `GuessEmbeddedLanguages` |
| `dispose` | `Close` / `Dispose` |

Methods returning tokens or HAST preserve structured data for custom renderers.
Errors are returned using Go's usual `(result, error)` convention.
`HASTToHTML` accepts `HASTOptions` for serialization controls; `SerializeHAST`
returns an explicit error for malformed nodes. `RawStyle` preserves literal CSS
declarations, and token JSON decoding supports both string and object styles.
`Version` reports the shiki-go release (`1.0.0`), while `ShikiVersion` reports
the compatible upstream Shiki release (`4.4.3`).

### Multiple themes

```go
html, err := h.CodeToHTML(code, shiki.Options{
    Lang: "go",
    Themes: map[string]any{
        "light": "github-light",
        "dark":  "github-dark",
    },
    DefaultColor: "light-dark()",
})
```

`DefaultColor` accepts a theme key, `false`, or `"light-dark()"`.
`CSSVariablePrefix` and `ColorsRendering` control CSS variables.
`CreateCSSVariablesTheme` builds a theme whose colors come from CSS variables.

Go maps have no insertion order. Use `ThemeOrder` when the order of custom
theme keys matters. Use `ColorReplacementOrder` when global and theme-specific
replacements overlap. Without an explicit order, global replacements override
theme-specific replacements. Decoding `Options` from JSON preserves both
object orders automatically.

### Transformers and decorations

```go
import "github.com/ralscha/shiki-go/transformers"

html, err := h.CodeToHTML(code, shiki.Options{
    Lang:  "go",
    Theme: "github-dark",
    Transformers: []shiki.Transformer{
        transformers.NotationDiff(),
        transformers.NotationHighlight(),
        transformers.ColorizedBrackets(),
    },
    Decorations: []shiki.Decoration{{
        Start: 0,
        End:   7,
        Properties: map[string]any{"class": "marked"},
    }},
})
```

The `transformers` package includes notation diff/highlight/focus/error/word
markers, metadata highlights, comment and newline removal, whitespace and
indent guides, rendered line numbers, compact line options, style-to-class
conversion, and bracket coloring. `Transformer` exposes preprocess, tokens,
span, line, code, pre, root, and postprocess hooks. Hook errors propagate to
the caller.

Decorations accept absolute offsets or `Position{Line, Character}` coordinates.
Line and character positions are zero-based. Token offsets, columns, ranges,
and lengths use **UTF-16 code units**, matching Shiki. Go strings remain UTF-8.
The lower-level `RegexScanner` interface uses UTF-8 byte offsets.

### Grammar state and streaming

```go
state, err := h.GetLastGrammarState("/* opening comment", shiki.Options{
    Lang: "go", Theme: "github-dark",
})
// Check err before using state.
result, err := h.CodeToTokens("continued */ package main", shiki.Options{
    Lang: "go", Theme: "github-dark", GrammarState: state,
})
```

Reuse grammar state with the same highlighter, language, and registered themes.
The syntax stack is immutable and can be used to tokenize multiple branches.
`GrammarContextCode` supplies preceding code when a saved state is unavailable.

`github.com/ralscha/shiki-go/stream.NewTokenizer(h, options)` implements Shiki's incremental
stable/unstable token protocol. `Enqueue` returns a recall count and new stable
and provisional tokens. Remove the recalled provisional tokens before
appending the returned tokens. `Close` returns the remaining provisional
tokens as stable. `stream.Transform` reads UTF-8 from an `io.Reader`, supports
context cancellation between reads, and emits tokens with optional recalls.

### Limits and special languages

`text`, `txt`, `plain`, and `plaintext` bypass grammar tokenization. `ansi`
interprets ANSI styling using the selected theme's terminal palette.
Theme `none` produces unstyled tokens.

Tokenization defaults to a 500 ms budget per line. Set `TokenizeTimeLimit` to
a pointer to a zero `time.Duration` to disable the limit.
`TokenizeMaxLineLength` skips lines at or above the supplied UTF-16 length.
The time budget is checked between regex searches; it does not interrupt an
individual Oniguruma search.

## CLI

```powershell
go build -o bin/shiki.exe ./cmd/shiki
bin/shiki.exe --lang go --theme github-dark --format html main.go
bin/shiki.exe main.go --theme vitesse-dark
Get-Content main.go -Raw | bin/shiki.exe --lang go --format tokens
bin/shiki.exe --list-langs
bin/shiki.exe --list-themes
```

On Linux and macOS, build with `go build -o bin/shiki ./cmd/shiki`.
Files and HTTP/HTTPS URLs are accepted; `-` reads stdin. Multiple inputs are
rendered in argument order. The language defaults to the file extension, or
plain text for stdin. The default output format is ANSI; `html`, `hast`, and
`tokens` are also available.

ANSI output detects terminal color support and honors `FORCE_COLOR=0|1|2|3`
and `NO_COLOR`. The library's `CodeToANSI` defaults to deterministic true-color
output; set `ANSIColorLevel` to choose another level explicitly.

## Development

One Go command replaces the PowerShell wrappers and individual maintenance
scripts. These commands work on Windows, Linux, and macOS:

```text
go run ./tools check
go run ./tools test ./... -count=1
go run ./tools build
go run ./tools build --os linux --arch arm64
go run ./tools build --tool all --all
go run ./tools manifest --check
go run ./tools dump javascript
```

With [Task](https://taskfile.dev/) installed, the corresponding shortcuts are
available through `task --list`; common entry points are `task check`,
`task build`, `task build:all`, and `task release:check`.

`check` verifies asset checksums and tidy module files, then runs all tests and
vet. `build` writes executables to `bin`; `--tool all --all` builds both the
highlighting CLI and development tool for Windows, Linux, and macOS on amd64
and arm64. Go build caches default to `.cache` inside the checkout, respecting
existing `GOCACHE` and `GOTMPDIR` settings. Ordinary `go test ./...` also works.

The development executable uses only the Go standard library. Once built,
its `manifest`, `dump`, and `diff` commands need no Go, Node, or shell runtime.
It locates the checkout from the current directory or executable location;
use `--root /path/to/shiki-go` when invoking it elsewhere. Run `--help` for
the full command list and see [tools/README.md](tools/README.md) for details.

Reference fixtures and compressed assets are checked in, so tests need no
Node.js or internet access once Go dependencies are cached. Refreshing upstream
data uses the pinned JavaScript implementation as an independent oracle:

```text
npm --prefix tools/reference ci --ignore-scripts
go run ./tools generate
```

Go handles generation, compression, license copying, and checksums. A single
JavaScript bridge evaluates upstream Shiki and exports its bundled assets.
Each reference suite runs in a separate Node process to isolate Shiki caches.
`assets/manifest.json` records source versions and SHA-256 checksums. Expected
results are always produced by upstream Shiki, independently of this Go port.

Shiki and third-party copyright notices are retained in [licenses](licenses).
Browser frameworks, TypeScript compiler services, and JavaScript bundler
integrations are outside the native Go library and CLI target.
