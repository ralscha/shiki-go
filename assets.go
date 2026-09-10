package shiki

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

//go:embed assets/catalog.json assets/languages/*.json.gz assets/themes/*.json.gz
var assets embed.FS
var catalog struct {
	Version   string         `json:"version"`
	Languages []LanguageInfo `json:"languages"`
	Themes    []ThemeInfo    `json:"themes"`
}

func init() {
	b, err := assets.ReadFile("assets/catalog.json")
	if err != nil {
		panic(err)
	}
	if err = json.Unmarshal(b, &catalog); err != nil {
		panic(err)
	}
}
func BundledLanguages() []LanguageInfo {
	out := append([]LanguageInfo(nil), catalog.Languages...)
	for i := range out {
		out[i].Aliases = append([]string(nil), out[i].Aliases...)
	}
	return out
}
func BundledThemes() []ThemeInfo { return append([]ThemeInfo(nil), catalog.Themes...) }
func readAsset(kind, name string, target any) error {
	if name == "" || strings.ContainsAny(name, "/\\.") {
		return fmt.Errorf("shiki: invalid %s name %q", kind, name)
	}
	data, err := assets.ReadFile("assets/" + kind + "/" + name + ".json.gz")
	if err != nil {
		return fmt.Errorf("shiki: %s %q is not bundled", kind, name)
	}
	z, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() { _ = z.Close() }()
	b, err := io.ReadAll(z)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}
func BundledLanguage(name string) (*Language, error) {
	for _, info := range catalog.Languages {
		if slices.Contains(info.Aliases, name) {
			name = info.ID
		}
	}
	var l Language
	if err := readAsset("languages", name, &l); err != nil {
		return nil, err
	}
	return &l, nil
}
func BundledTheme(name string) (*Theme, error) {
	var t Theme
	if err := readAsset("themes", name, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
