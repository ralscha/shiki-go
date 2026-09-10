package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testApp(t *testing.T) *app {
	t.Helper()
	a := &app{root: filepath.Join(t.TempDir(), "checkout with spaces"), ctx: context.Background(), out: io.Discard, errOut: io.Discard}
	files := map[string]string{
		"go.mod":                       "module shiki-go\n",
		"tools/reference/package.json": `{"dependencies":{"shiki":"4.4.3","tm-grammars":"1.32.3","tm-themes":"1.12.3","vscode-oniguruma":"1.7.0"}}`,
		"tools/reference/source.json":  `{"sourceCommit":"48cd2cc695ed2e3357c3f9c370578ea843d6d9a3"}`,
		"assets/catalog.json":          `{"version":"4.4.3","languages":[{"id":"javascript","aliases":["js"]}]}`,
		"assets/hast-properties.json":  `{}`,
		"internal/oniguruma/onig.wasm": "\x00asm\x01\x00\x00\x00",
	}
	for path, data := range files {
		if err := writeAtomic(filepath.Join(a.root, filepath.FromSlash(path)), []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	for path, data := range map[string]string{"assets/languages/javascript.json.gz": `{"name":"javascript","patterns":[],"ordered":{"z":1,"a":2}}`, "assets/themes/dark.json.gz": `{"name":"dark"}`} {
		compressed, err := compressJSON([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		if err := writeAtomic(filepath.Join(a.root, filepath.FromSlash(path)), compressed); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestManifestDetectsChangedMissingAndExtraAssets(t *testing.T) {
	for _, change := range []string{"changed", "missing", "extra", "version"} {
		t.Run(change, func(t *testing.T) {
			a := testApp(t)
			if err := a.manifest(false); err != nil {
				t.Fatal(err)
			}
			if err := a.manifest(true); err != nil {
				t.Fatal(err)
			}
			var err error
			switch change {
			case "changed":
				err = os.WriteFile(filepath.Join(a.root, "assets/hast-properties.json"), []byte(`{"changed":true}`), 0644)
			case "missing":
				err = os.Remove(filepath.Join(a.root, "assets/languages/javascript.json.gz"))
			case "extra":
				err = os.WriteFile(filepath.Join(a.root, "assets/languages/extra.json.gz"), []byte("extra"), 0644)
			case "version":
				err = os.WriteFile(filepath.Join(a.root, "assets/catalog.json"), []byte(`{"version":"5.0.0"}`), 0644)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := a.manifest(true); err == nil {
				t.Fatal("modified assets passed verification")
			}
		})
	}
}

func TestToolRunsWithoutNodeOrGoForLocalOperations(t *testing.T) {
	a := testApp(t)
	t.Setenv("PATH", "")
	var out bytes.Buffer
	for _, command := range [][]string{{"manifest"}, {"manifest", "--check"}, {"dump", "js"}} {
		args := append([]string{"--root", a.root}, command...)
		if err := run(a.ctx, args, &out, &out); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(out.String(), "Verified 5 asset checksums") {
		t.Fatal(out.String())
	}
	data, err := os.ReadFile(filepath.Join(a.root, ".cache/js.grammar.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"name": "javascript"`)) {
		t.Fatal(string(data))
	}
	for _, command := range [][]string{{"dump", "../escape"}, {"diff", "../escape"}, {"build", "--os", "../escape"}, {"build", "--all", "--arch", "amd64"}, {"build", "--tool", "unknown"}, {"manifest", "--unknown"}, {"generate", "unknown"}, {"unknown"}} {
		if err := run(a.ctx, append([]string{"--root", a.root}, command...), io.Discard, io.Discard); err == nil {
			t.Fatalf("invalid arguments accepted: %v", command)
		}
	}
}

func TestFindRootFromNestedDirectory(t *testing.T) {
	a := testApp(t)
	nested := filepath.Join(a.root, "tools", "reference", "cases")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	root, err := findRoot("")
	if err != nil || root != a.root {
		t.Fatalf("root=%q err=%v", root, err)
	}
	if _, err := findRoot(t.TempDir()); err == nil {
		t.Fatal("invalid checkout accepted")
	}
}

func TestReferenceRejectsWrongDependenciesBeforeWriting(t *testing.T) {
	a := testApp(t)
	lock := `{"packages":{"":{"dependencies":{"shiki":"4.4.3","tm-grammars":"1.32.3","tm-themes":"1.12.3","vscode-oniguruma":"1.7.0"}},"node_modules/shiki":{"version":"4.4.3"}}}`
	if err := writeAtomic(filepath.Join(a.root, "tools/reference/package-lock.json"), []byte(lock)); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(filepath.Join(a.root, "testdata/parity.json"), []byte("original fixture")); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"4.4.2", "4.4.3"} {
		data, _ := json.Marshal(map[string]string{"version": version})
		if err := writeAtomic(filepath.Join(a.root, "tools/reference/node_modules/shiki/package.json"), data); err != nil {
			t.Fatal(err)
		}
		got, err := a.referenceVersion()
		if version == "4.4.3" {
			if err != nil || got != version {
				t.Fatalf("%q %v", got, err)
			}
		} else if err == nil {
			t.Fatal("incorrect upstream version accepted")
		}
	}
	t.Setenv("PATH", "")
	if err := a.generate("assets"); err == nil {
		t.Fatal("generation unexpectedly succeeded without Node")
	}
	data, err := os.ReadFile(filepath.Join(a.root, "testdata/parity.json"))
	if err != nil || string(data) != "original fixture" {
		t.Fatalf("failed generation modified fixture: %q %v", data, err)
	}
}

func TestJSONDiffReportsMissingNullAndTrailingLines(t *testing.T) {
	var got, want any
	if err := json.Unmarshal([]byte(`{"tokens":[[{"content":"hi","color":"red"}]],"onlyGot":null}`), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"tokens":[[{"content":"hi","color":"blue"}],[]]}`), &want); err != nil {
		t.Fatal(err)
	}
	var paths []string
	count := diffJSON("$", got, want, func(path string, _, _ any) { paths = append(paths, path) })
	expected := []string{`$["onlyGot"] (key presence)`, `$["tokens"].length`, `$["tokens"][0][0]["color"]`}
	if count != 3 || !reflect.DeepEqual(paths, expected) {
		t.Fatalf("count=%d paths=%v", count, paths)
	}
}

func TestCompressionAndJSONPreserveGrammarSemantics(t *testing.T) {
	raw := []byte(`{"z":"<>&😀\\1","a":9007199254740993}`)
	first, err := compressJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compressJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("compression is nondeterministic")
	}
	path := filepath.Join(t.TempDir(), "grammar.json.gz")
	if err := writeAtomic(path, first); err != nil {
		t.Fatal(err)
	}
	decoded, err := readCompressed(path)
	if err != nil || !bytes.Equal(decoded, raw) {
		t.Fatalf("grammar changed: %q %v", decoded, err)
	}
	pretty, err := indentJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, pretty); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compact.Bytes(), raw) {
		t.Fatal("reformatting changed JSON")
	}
	if _, err := compressJSON([]byte(`invalid`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
