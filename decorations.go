package shiki

import (
	"encoding/json"
	"fmt"
	"sort"
)

type ResolvedPosition struct {
	Position
	Offset int `json:"offset"`
}
type PositionConverter struct {
	Lines  []SourceLine
	Length int
}

func CreatePositionConverter(source string) *PositionConverter {
	return &PositionConverter{SplitLines(source), UTF16Len(source)}
}
func (c *PositionConverter) IndexToPos(offset int) (Position, error) {
	if offset < 0 || offset > c.Length {
		return Position{}, fmt.Errorf("shiki: invalid offset %d (length %d)", offset, c.Length)
	}
	i := max(sort.Search(len(c.Lines), func(i int) bool { return c.Lines[i].Offset > offset })-1, 0)
	return Position{i, offset - c.Lines[i].Offset}, nil
}
func (c *PositionConverter) PosToIndex(p Position) (int, error) {
	if p.Line < 0 || p.Line >= len(c.Lines) {
		return 0, fmt.Errorf("shiki: invalid line %d", p.Line)
	}
	length := UTF16Len(c.Lines[p.Line].Content + c.Lines[p.Line].Ending)
	if p.Character < 0 {
		p.Character += length
	}
	if p.Character < 0 || p.Character > length {
		return 0, fmt.Errorf("shiki: invalid character %d on line %d", p.Character, p.Line)
	}
	return c.Lines[p.Line].Offset + p.Character, nil
}
func (c *PositionConverter) resolve(value any) (ResolvedPosition, error) {
	var p Position
	offset := -1
	numeric := false
	switch v := value.(type) {
	case int:
		offset = v
		numeric = true
	case int64:
		offset = int(v)
		numeric = true
	case float64:
		numeric = true
		if v != float64(int(v)) {
			return ResolvedPosition{}, fmt.Errorf("shiki: decoration offset must be an integer")
		}
		offset = int(v)
	case Position:
		p = v
	case *Position:
		if v == nil {
			return ResolvedPosition{}, fmt.Errorf("shiki: nil decoration position")
		}
		p = *v
	case map[string]any:
		b, _ := json.Marshal(v)
		if err := json.Unmarshal(b, &p); err != nil {
			return ResolvedPosition{}, err
		}
	default:
		return ResolvedPosition{}, fmt.Errorf("shiki: invalid decoration position %v", value)
	}
	var err error
	if numeric {
		p, err = c.IndexToPos(offset)
	} else {
		offset, err = c.PosToIndex(p)
		if err == nil {
			p, err = c.IndexToPos(offset)
		}
	}
	if err != nil {
		return ResolvedPosition{}, err
	}
	return ResolvedPosition{p, offset}, nil
}

type resolvedDecoration struct {
	Decoration
	start, end ResolvedPosition
}

func resolveDecorations(source string, decorations []Decoration) ([]resolvedDecoration, error) {
	converter := CreatePositionConverter(source)
	out := make([]resolvedDecoration, len(decorations))
	for i, d := range decorations {
		s, err := converter.resolve(d.Start)
		if err != nil {
			return nil, err
		}
		e, err := converter.resolve(d.End)
		if err != nil {
			return nil, err
		}
		if s.Offset > e.Offset {
			return nil, fmt.Errorf("shiki: invalid decoration range %d..%d", s.Offset, e.Offset)
		}
		out[i] = resolvedDecoration{d, s, e}
	}
	for i, a := range out {
		for _, b := range out[i+1:] {
			aStart, aEnd, bStart, bEnd := a.start.Offset, a.end.Offset, b.start.Offset, b.end.Offset
			if aStart < bStart && bStart < aEnd && aEnd < bEnd || bStart < aStart && aStart < bEnd && bEnd < aEnd {
				return nil, fmt.Errorf("shiki: decorations at %d and %d intersect", aStart, bStart)
			}
		}
	}
	return out, nil
}
func TransformerDecorations() Transformer {
	return Transformer{Name: "shiki:decorations", Tokens: func(ctx *TransformerContext, tokens [][]Token) ([][]Token, error) {
		if len(ctx.Options.Decorations) == 0 {
			return nil, nil
		}
		decorations, err := resolveDecorations(ctx.Source, ctx.Options.Decorations)
		if err != nil {
			return nil, err
		}
		ctx.Meta["shiki:decorations"] = decorations
		var breaks []int
		for _, d := range decorations {
			breaks = append(breaks, d.start.Offset, d.end.Offset)
		}
		return SplitTokens(tokens, breaks), nil
	}, Code: func(ctx *TransformerContext, code *Node) (*Node, error) {
		if len(ctx.Options.Decorations) == 0 {
			return nil, nil
		}
		decorations, ok := ctx.Meta["shiki:decorations"].([]resolvedDecoration)
		if !ok {
			var err error
			decorations, err = resolveDecorations(ctx.Source, ctx.Options.Decorations)
			if err != nil {
				return nil, err
			}
		}
		var lines []*Node
		for _, line := range code.Children {
			if line.Type == "element" && line.TagName == "span" {
				lines = append(lines, line)
			}
		}
		if len(lines) != len(SplitLines(ctx.Source)) {
			return nil, fmt.Errorf("shiki: rendered line count does not match source for decorations")
		}
		apply := func(node *Node, d Decoration, kind string) *Node {
			tag := d.TagName
			if tag == "" {
				tag = "span"
			}
			node.TagName = tag
			keys := []string{}
			for key := range d.Properties {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if len(d.propertyOrder) > 0 {
				ordered := []string{}
				for _, key := range d.propertyOrder {
					if hasString(keys, key) {
						ordered = append(ordered, key)
					}
				}
				for _, key := range keys {
					if !hasString(ordered, key) {
						ordered = append(ordered, key)
					}
				}
				keys = ordered
			}
			for _, key := range keys {
				if key == "class" {
					if _, exists := node.Properties[key]; !exists {
						node.SetProperty(key, nil)
					}
				} else {
					node.SetProperty(key, d.Properties[key])
				}
			}
			if classes, ok := d.Properties["class"]; ok {
				switch v := classes.(type) {
				case string:
					AddClassToHAST(node, v)
				case []string:
					AddClassToHAST(node, v...)
				case []any:
					for _, v := range v {
						AddClassToHAST(node, fmt.Sprint(v))
					}
				}
			}
			if d.Transform != nil {
				if n := d.Transform(node, kind); n != nil {
					node = n
				}
			}
			return node
		}
		section := func(line, start, end int, d Decoration) error {
			node := lines[line]
			a, b := -1, -1
			if start == 0 {
				a = 0
			}
			if end == 0 {
				b = 0
			}
			if end < 0 {
				b = len(node.Children)
			}
			offset := 0
			for i, child := range node.Children {
				offset += UTF16Len(TextContent(child))
				if a == -1 && offset == start {
					a = i + 1
				}
				if b == -1 && offset == end {
					b = i + 1
				}
			}
			if a == -1 || b == -1 {
				return fmt.Errorf("shiki: cannot locate decoration %d..%d on line %d", start, end, line)
			}
			children := append([]*Node(nil), node.Children[a:b]...)
			if !d.AlwaysWrap && len(children) == len(node.Children) {
				replacement := apply(node, d, "line")
				*node = *replacement
			} else if !d.AlwaysWrap && len(children) == 1 && children[0].Type == "element" {
				node.Children[a] = apply(children[0], d, "token")
			} else {
				wrapper := apply(Element("span", nil, children...), d, "wrapper")
				updated := append([]*Node(nil), node.Children[:a]...)
				updated = append(updated, wrapper)
				updated = append(updated, node.Children[b:]...)
				node.Children = updated
			}
			return nil
		}
		sort.SliceStable(decorations, func(i, j int) bool {
			a, b := decorations[i], decorations[j]
			if a.start.Offset != b.start.Offset {
				return a.start.Offset > b.start.Offset
			}
			return a.end.Offset < b.end.Offset
		})
		type fullLine struct {
			line int
			d    Decoration
		}
		var full []fullLine
		for _, d := range decorations {
			s, e := d.start, d.end
			if s.Line == e.Line {
				if err := section(s.Line, s.Character, e.Character, d.Decoration); err != nil {
					return nil, err
				}
			} else {
				if err := section(s.Line, s.Character, -1, d.Decoration); err != nil {
					return nil, err
				}
				for i := s.Line + 1; i < e.Line; i++ {
					full = append([]fullLine{{i, d.Decoration}}, full...)
				}
				if err := section(e.Line, 0, e.Character, d.Decoration); err != nil {
					return nil, err
				}
			}
		}
		for _, f := range full {
			replacement := apply(lines[f.line], f.d, "line")
			*lines[f.line] = *replacement
		}
		return nil, nil
	}}
}
