# Changelog

All notable changes to shiki-go are documented in this file.

## 1.0.0 - 2026-09-12

The first stable release establishes the exported APIs in the `shiki`,
`stream`, and `transformers` packages as the backward-compatible v1 contract.

### Highlights

- Native Go syntax highlighting based on Shiki 4.4.3, with selected subsequent
  transformer fixes and line-number rendering.
- The complete catalog of 242 public languages, 260 grammar assets, 65 themes,
  and the Oniguruma WebAssembly binary embedded in the module.
- HTML, HAST, token, multi-theme, CSS-variable, and ANSI output.
- Decorations, bundled transformers, colorized brackets, grammar-state reuse,
  and incremental streaming tokenization.
- A command-line program supporting files, standard input, and HTTP/HTTPS URLs.
- No runtime dependency on Node.js, cgo, external executables, downloads, or
  separately installed grammar and theme files.
- Differential fixtures against the pinned Shiki packages, asset checksums,
  multi-platform CI, and Linux race-detector coverage.

### Requirements

- Go 1.27.1 or newer.

### Installation

Add the library to another Go module:

```console
go get github.com/ralscha/shiki-go@v1.0.0
```

Install the command-line program:

```console
go install github.com/ralscha/shiki-go/cmd/shiki@v1.0.0
```

See the [README](README.md) for a complete library example and CLI usage.
