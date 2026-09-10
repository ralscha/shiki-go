// shiki-dev is the portable development tool for this repository.
// It uses only the Go standard library; upstream reference refreshes also use Node.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
)

const usage = `Usage: shiki-dev [--root directory] command [arguments]

  test [go test arguments]        Run tests (default: ./...)
  vet [go vet arguments]          Run vet (default: ./...)
  check                          Verify assets and module files, run tests and vet
  build [options]                Build a standalone executable
    --os OS --arch ARCH          Target platform (default: current host)
    --all                       Windows, Linux, macOS; amd64 and arm64
    --tool shiki|dev|all         Executable(s) to build (default: shiki)
    --out directory             Output directory (default: bin)
  manifest [--check]             Write or verify asset checksums
  generate [all|assets|fixtures|notices|manifest]
                                Refresh pinned upstream data (default: all)
  diff NAME...                  Explain saved .cache/mismatches token differences
  dump LANGUAGE...              Export bundled grammars to .cache

Run from any directory inside the checkout, or supply --root.
Go is needed for test, vet, check, build, and Go dependency notices.
Node plus tools/reference/node_modules is needed only for upstream refreshes.
`

type app struct {
	root        string
	out, errOut io.Writer
	ctx         context.Context
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "shiki-dev:", err)
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() > 0 {
			os.Exit(exit.ExitCode())
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("shiki-dev", flag.ContinueOnError)
	fs.SetOutput(errOut)
	root := fs.String("root", "", "checkout directory")
	fs.Usage = func() { _, _ = fmt.Fprint(out, usage) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	args = fs.Args()
	if len(args) == 0 || args[0] == "help" {
		_, err := fmt.Fprint(out, usage)
		return err
	}
	resolved, err := findRoot(*root)
	if err != nil {
		return err
	}
	a := &app{root: resolved, out: out, errOut: errOut, ctx: ctx}
	command, args := args[0], args[1:]
	switch command {
	case "test", "vet":
		if len(args) == 0 {
			args = []string{"./..."}
		}
		return a.goCommand(nil, append([]string{command}, args...)...)
	case "check":
		if len(args) != 0 {
			return fmt.Errorf("check takes no arguments")
		}
		if err := a.manifest(true); err != nil {
			return err
		}
		if err := a.goCommand(nil, "mod", "tidy", "-diff"); err != nil {
			return err
		}
		if err := a.goCommand(nil, "test", "./..."); err != nil {
			return err
		}
		return a.goCommand(nil, "vet", "./...")
	case "build":
		return a.build(args)
	case "manifest":
		if len(args) == 0 {
			return a.manifest(false)
		}
		if len(args) == 1 && args[0] == "--check" {
			return a.manifest(true)
		}
		return fmt.Errorf("usage: manifest [--check]")
	case "generate":
		if len(args) > 1 {
			return fmt.Errorf("usage: generate [all|assets|fixtures|notices|manifest]")
		}
		part := "all"
		if len(args) == 1 {
			part = args[0]
		}
		return a.generate(part)
	case "diff":
		return a.diff(args)
	case "dump":
		return a.dump(args)
	default:
		return fmt.Errorf("unknown command %q; use --help", command)
	}
}

func findRoot(explicit string) (string, error) {
	valid := func(path string) bool {
		for _, name := range []string{"go.mod", "assets/catalog.json", "tools/reference/package.json"} {
			info, err := os.Stat(filepath.Join(path, filepath.FromSlash(name)))
			if err != nil || info.IsDir() {
				return false
			}
		}
		return true
	}
	if explicit != "" {
		path, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		if !valid(path) {
			return "", fmt.Errorf("%s is not a shiki-go checkout", path)
		}
		return path, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	starts := []string{wd}
	if executable, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(executable))
	}
	for _, start := range starts {
		for path := start; ; path = filepath.Dir(path) {
			if valid(path) {
				return path, nil
			}
			if filepath.Dir(path) == path {
				break
			}
		}
	}
	return "", fmt.Errorf("cannot find the shiki-go checkout; supply --root directory")
}

// envWith replaces keys rather than appending duplicates (case-insensitive on Windows).
func envWith(base []string, overrides map[string]string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		replaced := false
		for target := range overrides {
			if key == target || (runtime.GOOS == "windows" && strings.EqualFold(key, target)) {
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func (a *app) goEnv(extra map[string]string) ([]string, error) {
	env := map[string]string{}
	for key, directory := range map[string]string{"GOCACHE": "go-build", "GOTMPDIR": "go-tmp"} {
		path := os.Getenv(key)
		if path == "" {
			path = filepath.Join(a.root, ".cache", directory)
			env[key] = path
		}
		if path != "off" {
			if err := os.MkdirAll(path, 0755); err != nil {
				return nil, err
			}
		}
	}
	maps.Copy(env, extra)
	return envWith(os.Environ(), env), nil
}

func (a *app) goCommand(extra map[string]string, args ...string) error {
	env, err := a.goEnv(extra)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(a.ctx, "go", args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr, cmd.Stdin = a.root, env, a.out, a.errOut, os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (a *app) build(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	targetOS := fs.String("os", runtime.GOOS, "target operating system")
	targetArch := fs.String("arch", runtime.GOARCH, "target architecture")
	all := fs.Bool("all", false, "build six common platforms")
	tool := fs.String("tool", "shiki", "shiki, dev, or all")
	outDir := fs.String("out", "bin", "output directory, relative to the checkout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("build accepts flags only")
	}
	if *tool != "shiki" && *tool != "dev" && *tool != "all" {
		return fmt.Errorf("unknown tool %q", *tool)
	}
	if !safeName(*targetOS) || !safeName(*targetArch) {
		return fmt.Errorf("invalid target OS or architecture")
	}
	if *all {
		conflict := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "os" || f.Name == "arch" {
				conflict = true
			}
		})
		if conflict {
			return fmt.Errorf("--all cannot be combined with --os or --arch")
		}
	}
	targets := [][2]string{{*targetOS, *targetArch}}
	if *all {
		targets = [][2]string{{"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}}
	}
	types := []string{*tool}
	if *tool == "all" {
		types = []string{"shiki", "dev"}
	}
	output := *outDir
	if !filepath.IsAbs(output) {
		output = filepath.Join(a.root, output)
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	for _, target := range targets {
		for _, kind := range types {
			name, pkg := "shiki", "./cmd/shiki"
			if kind == "dev" {
				name, pkg = "shiki-dev", "./tools"
			}
			name += "-" + target[0] + "-" + target[1]
			if target[0] == "windows" {
				name += ".exe"
			}
			path := filepath.Join(output, name)
			if err := a.goCommand(map[string]string{"CGO_ENABLED": "0", "GOOS": target[0], "GOARCH": target[1]}, "build", "-trimpath", "-ldflags=-s -w", "-o", path, pkg); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(a.out, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func safeName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' {
			continue
		}
		return false
	}
	return true
}
