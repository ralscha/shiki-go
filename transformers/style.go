package transformers

import (
	"maps"
	shiki "shiki-go"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
)

type StyleToClassOptions struct {
	ClassPrefix, ClassSuffix string
	ClassReplacer            func(string) string
}
type StyleToClassTransformer struct {
	shiki.Transformer
	mu       sync.Mutex
	registry map[string]string
	order    []string
	options  StyleToClassOptions
}

func StyleToClass(options ...StyleToClassOptions) *StyleToClassTransformer {
	var o StyleToClassOptions
	if len(options) > 0 {
		o = options[0]
	}
	if o.ClassPrefix == "" {
		o.ClassPrefix = "__shiki_"
	}
	s := &StyleToClassTransformer{registry: map[string]string{}, options: o}
	s.Transformer = shiki.Transformer{Name: "@shikijs/transformers:style-to-class", Pre: func(_ *shiki.TransformerContext, n *shiki.Node) (*shiki.Node, error) {
		if style, ok := n.Properties["style"].(string); ok && style != "" {
			class := s.register(style)
			delete(n.Properties, "style")
			shiki.AddClassToHAST(n, class)
		}
		return nil, nil
	}, Tokens: func(_ *shiki.TransformerContext, lines [][]shiki.Token) ([][]shiki.Token, error) {
		for i, line := range lines {
			for j := range line {
				t := &lines[i][j]
				if t.HTMLStyle == nil {
					continue
				}
				class := s.register(shiki.StringifyTokenStyle(t.HTMLStyle))
				t.HTMLStyle = shiki.Style{}
				attrs := map[string]string{}
				maps.Copy(attrs, t.HTMLAttrs)
				if attrs["class"] == "" {
					attrs["class"] = class
				} else {
					attrs["class"] += " " + class
				}
				t.HTMLAttrs = attrs
			}
		}
		return nil, nil
	}}
	return s
}
func (s *StyleToClassTransformer) register(style string) string {
	class := s.options.ClassPrefix + cyrb53(style) + s.options.ClassSuffix
	if s.options.ClassReplacer != nil {
		class = s.options.ClassReplacer(class)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.registry[class]; !ok {
		s.order = append(s.order, class)
		s.registry[class] = style
	}
	return class
}
func (s *StyleToClassTransformer) GetCSS() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for _, class := range s.order {
		b.WriteString("." + class + "{" + s.registry[class] + "}")
	}
	return b.String()
}
func (s *StyleToClassTransformer) GetClassRegistry() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := map[string]string{}
	maps.Copy(m, s.registry)
	return m
}
func (s *StyleToClassTransformer) ClearRegistry() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry = map[string]string{}
	s.order = nil
}
func cyrb53(s string) string {
	h1, h2 := uint32(0xdeadbeef), uint32(0x41c6ce57)
	for _, c := range utf16.Encode([]rune(s)) {
		h1 = (h1 ^ uint32(c)) * 2654435761
		h2 = (h2 ^ uint32(c)) * 1597334677
	}
	h1 = (h1 ^ (h1 >> 16)) * 2246822507
	h1 ^= (h2 ^ (h2 >> 13)) * 3266489909
	h2 = (h2 ^ (h2 >> 16)) * 2246822507
	h2 ^= (h1 ^ (h1 >> 13)) * 3266489909
	value := uint64(h2&2097151)*4294967296 + uint64(h1)
	out := strconv.FormatUint(value, 36)
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}
