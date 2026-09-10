package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

func (a *app) diff(names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("usage: diff NAME... (from .cache/mismatches/NAME.got.json and NAME.want.json)")
	}
	for _, name := range names {
		if !safeName(name) {
			return fmt.Errorf("invalid mismatch name %q", name)
		}
		values := make([]any, 2)
		for i, suffix := range []string{".got.json", ".want.json"} {
			data, err := os.ReadFile(filepath.Join(a.root, ".cache/mismatches", name+suffix))
			if err != nil {
				return err
			}
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			if err := decoder.Decode(&values[i]); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(a.out, name); err != nil {
			return err
		}
		var writeErr error
		count := diffJSON("$", values[0], values[1], func(path string, got, want any) {
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(want)
			if writeErr == nil {
				_, writeErr = fmt.Fprintf(a.out, "%s\n  got:  %s\n  want: %s\n", path, g, w)
			}
		})
		if writeErr != nil {
			return writeErr
		}
		if count == 0 {
			if _, err := fmt.Fprintln(a.out, "  identical"); err != nil {
				return err
			}
		}
	}
	return nil
}

func diffJSON(path string, got, want any, emit func(string, any, any)) int {
	if reflect.DeepEqual(got, want) {
		return 0
	}
	if g, ok := got.(map[string]any); ok {
		if w, ok := want.(map[string]any); ok {
			keys := map[string]bool{}
			for key := range g {
				keys[key] = true
			}
			for key := range w {
				keys[key] = true
			}
			ordered := make([]string, 0, len(keys))
			for key := range keys {
				ordered = append(ordered, key)
			}
			sort.Strings(ordered)
			count := 0
			for _, key := range ordered {
				gv, gp := g[key]
				wv, wp := w[key]
				child := path + "[" + fmt.Sprintf("%q", key) + "]"
				if gp != wp {
					emit(child+" (key presence)", gp, wp)
					count++
					continue
				}
				count += diffJSON(child, gv, wv, emit)
			}
			return count
		}
	}
	if g, ok := got.([]any); ok {
		if w, ok := want.([]any); ok {
			count := 0
			if len(g) != len(w) {
				emit(path+".length", len(g), len(w))
				count++
			}
			for i := 0; i < min(len(g), len(w)); i++ {
				count += diffJSON(fmt.Sprintf("%s[%d]", path, i), g[i], w[i], emit)
			}
			return count
		}
	}
	emit(path, got, want)
	return 1
}

func (a *app) dump(names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("usage: dump LANGUAGE [LANGUAGE ...]")
	}
	var catalog struct {
		Languages []struct {
			ID      string   `json:"id"`
			Aliases []string `json:"aliases"`
		} `json:"languages"`
	}
	if err := readJSON(filepath.Join(a.root, "assets/catalog.json"), &catalog); err != nil {
		return err
	}
	for _, name := range names {
		if !safeName(name) {
			return fmt.Errorf("invalid language name %q", name)
		}
		id := name
		for _, lang := range catalog.Languages {
			for _, alias := range lang.Aliases {
				if alias == name {
					id = lang.ID
				}
			}
		}
		data, err := readCompressed(filepath.Join(a.root, "assets/languages", id+".json.gz"))
		if err != nil {
			return err
		}
		data, err = indentJSON(data)
		if err != nil {
			return err
		}
		path := filepath.Join(a.root, ".cache", name+".grammar.json")
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(a.out, path); err != nil {
			return err
		}
	}
	return nil
}
