// Package textmate implements TextMate grammar compilation and tokenization.
package textmate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// StringList accepts both forms used by TextMate JSON files.
type StringList []string

func (s *StringList) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = strings.Split(v, ",")
		return nil
	}
	return json.Unmarshal(b, (*[]string)(s))
}

type Rule struct {
	Name                string           `json:"name,omitempty"`
	ContentName         string           `json:"contentName,omitempty"`
	Match               string           `json:"match,omitempty"`
	Begin               *string          `json:"begin,omitempty"`
	End                 *string          `json:"end,omitempty"`
	While               *string          `json:"while,omitempty"`
	Include             string           `json:"include,omitempty"`
	Patterns            []*Rule          `json:"patterns,omitempty"`
	Repository          map[string]*Rule `json:"repository,omitempty"`
	Captures            CaptureMap       `json:"captures,omitempty"`
	BeginCaptures       CaptureMap       `json:"beginCaptures,omitempty"`
	EndCaptures         CaptureMap       `json:"endCaptures,omitempty"`
	WhileCaptures       CaptureMap       `json:"whileCaptures,omitempty"`
	ApplyEndPatternLast bool             `json:"applyEndPatternLast,omitempty"`
}

type CaptureMap map[string]*Rule

func (c *CaptureMap) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '[' {
		var rules []*Rule
		if err := json.Unmarshal(data, &rules); err != nil {
			return err
		}
		*c = CaptureMap{}
		for i, r := range rules {
			(*c)[strconv.Itoa(i)] = r
		}
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		return nil
	}
	*c = CaptureMap{}
	for key, b := range raw {
		if _, err := strconv.Atoi(key); err != nil {
			continue
		}
		if len(b) == 0 || b[0] != '{' {
			continue
		}
		var rule *Rule
		if err := json.Unmarshal(b, &rule); err != nil {
			return err
		}
		(*c)[key] = rule
	}
	return nil
}

func (r *Rule) UnmarshalJSON(data []byte) error {
	// TextMate treats primitive and array-valued repository entries as empty
	// include rules. Several bundled grammars contain these legacy entries.
	if len(data) == 0 || data[0] != '{' {
		return nil
	}
	type alias Rule
	value := struct {
		*alias
		ApplyEndPatternLast json.RawMessage `json:"applyEndPatternLast"`
	}{alias: (*alias)(r)}
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("TextMate rule %.160s: %w", data, err)
	}
	if len(value.ApplyEndPatternLast) > 0 {
		r.ApplyEndPatternLast = string(value.ApplyEndPatternLast) == "true" || string(value.ApplyEndPatternLast) == "1"
	}
	return nil
}

type Grammar struct {
	Name                       string           `json:"name"`
	ScopeName                  string           `json:"scopeName"`
	DisplayName                string           `json:"displayName,omitempty"`
	Aliases                    []string         `json:"aliases,omitempty"`
	EmbeddedLangs              []string         `json:"embeddedLangs,omitempty"`
	EmbeddedLanguages          []string         `json:"embeddedLanguages,omitempty"`
	EmbeddedLangsLazy          []string         `json:"embeddedLangsLazy,omitempty"`
	Patterns                   []*Rule          `json:"patterns"`
	Repository                 map[string]*Rule `json:"repository,omitempty"`
	Injections                 map[string]*Rule `json:"injections,omitempty"`
	InjectionOrder             []string         `json:"injectionOrder,omitempty"`
	InjectionSelector          string           `json:"injectionSelector,omitempty"`
	InjectTo                   []string         `json:"injectTo,omitempty"`
	BalancedBracketSelectors   []string         `json:"balancedBracketSelectors,omitempty"`
	UnbalancedBracketSelectors []string         `json:"unbalancedBracketSelectors,omitempty"`
}

func (g *Grammar) UnmarshalJSON(data []byte) error {
	type alias Grammar
	if err := json.Unmarshal(data, (*alias)(g)); err != nil {
		return err
	}
	if len(g.InjectionOrder) > 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(fields["injections"]))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil
	}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return err
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return err
		}
		g.InjectionOrder = append(g.InjectionOrder, key.(string))
	}
	return nil
}
