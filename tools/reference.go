package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// Validate actual installed versions against the lockfile before running the oracle.
func (a *app) referenceVersion() (string, error) {
	base := filepath.Join(a.root, "tools/reference")
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := readJSON(filepath.Join(base, "package.json"), &pkg); err != nil {
		return "", err
	}
	var lock struct {
		Packages map[string]struct {
			Version      string            `json:"version"`
			Dependencies map[string]string `json:"dependencies"`
		} `json:"packages"`
	}
	if err := readJSON(filepath.Join(base, "package-lock.json"), &lock); err != nil {
		return "", err
	}
	if !reflect.DeepEqual(pkg.Dependencies, lock.Packages[""].Dependencies) {
		return "", fmt.Errorf("package.json dependencies differ from package-lock.json")
	}
	paths := make([]string, 0, len(lock.Packages))
	for path := range lock.Packages {
		if path != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !strings.HasPrefix(path, "node_modules/") || !filepath.IsLocal(filepath.FromSlash(path)) {
			return "", fmt.Errorf("unsupported lockfile package path %q", path)
		}
		var installed struct {
			Version string `json:"version"`
		}
		err := readJSON(filepath.Join(base, filepath.FromSlash(path), "package.json"), &installed)
		if err != nil || installed.Version != lock.Packages[path].Version {
			return "", fmt.Errorf("reference dependency %s is missing or differs from the lockfile; run npm ci --ignore-scripts in tools/reference", path)
		}
	}
	version := pkg.Dependencies["shiki"]
	if version == "" || lock.Packages["node_modules/shiki"].Version != version {
		return "", fmt.Errorf("shiki must be pinned to an exact version")
	}
	return version, nil
}

func (a *app) oracle(operation string, input []byte) ([]byte, error) {
	cmd := exec.CommandContext(a.ctx, "node", filepath.Join(a.root, "tools/reference/bridge.mjs"), operation)
	var output bytes.Buffer
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = a.root, bytes.NewReader(input), &output, a.errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("upstream %s (requires Node.js and pinned reference dependencies): %w", operation, err)
	}
	if !json.Valid(output.Bytes()) {
		return nil, fmt.Errorf("upstream %s returned invalid JSON", operation)
	}
	return output.Bytes(), nil
}

func (a *app) generate(part string) error {
	if part == "manifest" {
		return a.manifest(false)
	}
	if part != "all" && part != "assets" && part != "fixtures" && part != "notices" {
		return fmt.Errorf("unknown generation target %q", part)
	}
	version, err := a.referenceVersion()
	if err != nil {
		return err
	}
	// Compute everything before replacing checked-in files. An oracle failure leaves them intact.
	outputs := map[string][]byte{}
	if part == "all" || part == "assets" {
		if err := a.importAssets(version, outputs); err != nil {
			return err
		}
	}
	if part == "all" || part == "fixtures" {
		entries, err := os.ReadDir(filepath.Join(a.root, "tools/reference/cases"))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			if _, err := fmt.Fprintln(a.out, "Generating reference suite", entry.Name()); err != nil {
				return err
			}
			input, err := os.ReadFile(filepath.Join(a.root, "tools/reference/cases", entry.Name()))
			if err != nil {
				return err
			}
			raw, err := a.oracle("suite", input)
			if err != nil {
				return fmt.Errorf("%s: %w", entry.Name(), err)
			}
			data, err := indentJSON(raw)
			if err != nil {
				return err
			}
			outputs["testdata/"+entry.Name()] = data
		}
	}
	if part == "all" || part == "notices" {
		if err := a.importNotices(outputs); err != nil {
			return err
		}
	}
	if err := a.writeOutputs(outputs); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(a.out, "Generated %d files\n", len(outputs)); err != nil {
		return err
	}
	if part == "all" || part == "assets" {
		return a.manifest(false)
	}
	return nil
}

func (a *app) importAssets(version string, outputs map[string][]byte) error {
	input, _ := json.Marshal(map[string]string{"version": version})
	raw, err := a.oracle("assets", input)
	if err != nil {
		return err
	}
	var result struct {
		Catalog    json.RawMessage   `json:"catalog"`
		Properties json.RawMessage   `json:"properties"`
		Languages  map[string]string `json:"languages"`
		Themes     map[string]string `json:"themes"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if len(result.Languages) == 0 || len(result.Themes) == 0 || len(result.Properties) == 0 {
		return fmt.Errorf("upstream returned empty assets")
	}
	for kind, values := range map[string]map[string]string{"languages": result.Languages, "themes": result.Themes} {
		for name, value := range values {
			if !safeName(name) {
				return fmt.Errorf("invalid %s asset name %q", kind, name)
			}
			data, err := compressJSON([]byte(value))
			if err != nil {
				return fmt.Errorf("%s/%s: %w", kind, name, err)
			}
			outputs["assets/"+kind+"/"+name+".json.gz"] = data
		}
		// Do not silently retain grammars removed by a future upstream release.
		entries, err := os.ReadDir(filepath.Join(a.root, "assets", kind))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if _, ok := outputs["assets/"+kind+"/"+entry.Name()]; !ok {
				return fmt.Errorf("obsolete asset assets/%s/%s; review and remove it before importing", kind, entry.Name())
			}
		}
	}
	outputs["assets/catalog.json"], err = indentJSON(result.Catalog)
	if err != nil {
		return err
	}
	outputs["assets/hast-properties.json"] = append([]byte(result.Properties), '\n')
	wasm, err := os.ReadFile(filepath.Join(a.root, "tools/reference/node_modules/shiki/dist/onig.wasm"))
	if err != nil {
		return err
	}
	if len(wasm) < 8 || !bytes.Equal(wasm[:8], []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) {
		return fmt.Errorf("invalid upstream Oniguruma WebAssembly binary")
	}
	outputs["internal/oniguruma/onig.wasm"] = wasm
	_, err = fmt.Fprintf(a.out, "Imported %d grammars and %d themes\n", len(result.Languages), len(result.Themes))
	return err
}

func (a *app) importNotices(outputs map[string][]byte) error {
	base := filepath.Join(a.root, "tools/reference/node_modules")
	copyNotice := func(name, filename, destination string) error {
		data, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(name), filename))
		if err != nil {
			return err
		}
		outputs["licenses/"+destination] = data
		return nil
	}
	label := func(name string) string { return strings.ReplaceAll(strings.TrimPrefix(name, "@"), "/", "-") }
	for _, name := range []string{"shiki", "@shikijs/langs", "@shikijs/themes", "@shikijs/vscode-textmate", "@shikijs/engine-oniguruma"} {
		filename := "LICENSE"
		if name == "@shikijs/vscode-textmate" {
			filename = "LICENSE.md"
		}
		if err := copyNotice(name, filename, label(name)+".txt"); err != nil {
			return err
		}
	}
	for _, item := range [][2]string{{"tm-grammars", "LICENSE"}, {"tm-grammars", "NOTICE"}, {"tm-themes", "LICENSE"}, {"tm-themes", "NOTICE"}, {"vscode-oniguruma", "LICENSE.txt"}, {"vscode-oniguruma", "NOTICES.txt"}, {"@shikijs/transformers", "LICENSE"}, {"@shikijs/colorized-brackets", "LICENSE"}, {"@shikijs/stream", "LICENSE"}, {"@shikijs/cli", "LICENSE"}} {
		if err := copyNotice(item[0], item[1], label(item[0])+"-"+item[1]+".txt"); err != nil {
			return err
		}
	}
	for _, name := range []string{"property-information", "hast-util-to-html", "stringify-entities", "ansis"} {
		if err := copyNotice(name, "license", name+".txt"); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			if err := copyNotice(name, "LICENSE", name+".txt"); err != nil {
				return err
			}
		}
	}
	// Ask Go for the selected versions and directories, including any module replacements.
	cmd := exec.CommandContext(a.ctx, "go", "list", "-m", "-json", "github.com/tetratelabs/wazero", "golang.org/x/sys")
	var err error
	cmd.Env, err = a.goEnv(nil)
	if err != nil {
		return err
	}
	cmd.Dir, cmd.Stderr = a.root, a.errOut
	data, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("resolve Go dependency licenses: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	labels := map[string]string{"github.com/tetratelabs/wazero": "wazero", "golang.org/x/sys": "go-x-sys"}
	for {
		var module struct {
			Path, Dir, Version string
			Replace            *struct{ Dir string }
		}
		if err := decoder.Decode(&module); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		directory := module.Dir
		if module.Replace != nil {
			directory = module.Replace.Dir
		}
		if directory == "" {
			return fmt.Errorf("module %s %s is not downloaded; run go mod download", module.Path, module.Version)
		}
		name, ok := labels[module.Path]
		if !ok {
			return fmt.Errorf("unexpected Go module %s", module.Path)
		}
		license, err := os.ReadFile(filepath.Join(directory, "LICENSE"))
		if err != nil {
			return err
		}
		outputs["licenses/"+name+".txt"] = license
		delete(labels, module.Path)
	}
	if len(labels) != 0 {
		return fmt.Errorf("go did not resolve all dependency licenses")
	}
	_, err = fmt.Fprintln(a.out, "Imported upstream and selected Go dependency notices")
	return err
}
