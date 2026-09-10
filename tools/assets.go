package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

type assetManifest struct {
	Shiki        string            `json:"shiki"`
	SourceCommit string            `json:"sourceCommit"`
	Grammars     string            `json:"grammars"`
	Themes       string            `json:"themes"`
	Oniguruma    string            `json:"oniguruma"`
	Files        map[string]string `json:"files"`
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func prettyJSON(value any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	err := e.Encode(value)
	return b.Bytes(), err
}

// Reindent raw JSON without changing property order, HTML escaping, or numbers.
func indentJSON(raw []byte) ([]byte, error) {
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", "  "); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

func writeAtomic(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".shiki-dev-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(0644); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (a *app) writeOutputs(outputs map[string][]byte) error {
	paths := make([]string, 0, len(outputs))
	for path := range outputs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !filepath.IsLocal(filepath.FromSlash(path)) {
			return fmt.Errorf("invalid output path %q", path)
		}
		if err := writeAtomic(filepath.Join(a.root, filepath.FromSlash(path)), outputs[path]); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) currentManifest() (*assetManifest, error) {
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := readJSON(filepath.Join(a.root, "tools/reference/package.json"), &pkg); err != nil {
		return nil, err
	}
	var source struct {
		SourceCommit string `json:"sourceCommit"`
	}
	if err := readJSON(filepath.Join(a.root, "tools/reference/source.json"), &source); err != nil {
		return nil, err
	}
	if len(source.SourceCommit) != 40 {
		return nil, fmt.Errorf("source.json must identify the upstream commit")
	}
	for _, name := range []string{"shiki", "tm-grammars", "tm-themes", "vscode-oniguruma"} {
		if pkg.Dependencies[name] == "" {
			return nil, fmt.Errorf("missing pinned dependency %s", name)
		}
	}
	var catalog struct {
		Version string `json:"version"`
	}
	if err := readJSON(filepath.Join(a.root, "assets/catalog.json"), &catalog); err != nil {
		return nil, err
	}
	if catalog.Version != pkg.Dependencies["shiki"] {
		return nil, fmt.Errorf("catalog version %s does not match pinned Shiki %s; regenerate assets", catalog.Version, pkg.Dependencies["shiki"])
	}
	paths := []string{"assets/catalog.json", "assets/hast-properties.json", "internal/oniguruma/onig.wasm"}
	for _, directory := range []string{"assets/languages", "assets/themes"} {
		entries, err := os.ReadDir(filepath.Join(a.root, filepath.FromSlash(directory)))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				return nil, fmt.Errorf("unexpected directory in assets: %s/%s", directory, entry.Name())
			}
			paths = append(paths, directory+"/"+entry.Name())
		}
	}
	m := &assetManifest{Shiki: pkg.Dependencies["shiki"], SourceCommit: source.SourceCommit, Grammars: "tm-grammars@" + pkg.Dependencies["tm-grammars"], Themes: "tm-themes@" + pkg.Dependencies["tm-themes"], Oniguruma: "vscode-oniguruma@" + pkg.Dependencies["vscode-oniguruma"], Files: map[string]string{}}
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(a.root, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		m.Files[path] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return m, nil
}

func (a *app) manifest(check bool) error {
	current, err := a.currentManifest()
	if err != nil {
		return err
	}
	path := filepath.Join(a.root, "assets/manifest.json")
	if check {
		var recorded assetManifest
		if err := readJSON(path, &recorded); err != nil {
			return err
		}
		if !reflect.DeepEqual(current, &recorded) {
			return fmt.Errorf("asset manifest is stale or assets were modified; inspect changes and run manifest to record them")
		}
		_, err = fmt.Fprintf(a.out, "Verified %d asset checksums\n", len(current.Files))
		return err
	}
	data, err := prettyJSON(current)
	if err != nil {
		return err
	}
	if err := writeAtomic(path, data); err != nil {
		return err
	}
	_, err = fmt.Fprintf(a.out, "Recorded %d asset checksums\n", len(current.Files))
	return err
}

func compressJSON(data []byte) ([]byte, error) {
	if !json.Valid(data) {
		return nil, fmt.Errorf("upstream asset is not JSON")
	}
	var b bytes.Buffer
	w, err := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func readCompressed(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	r, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}
