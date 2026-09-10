package shiki

import "strings"

type CSSVariablesThemeOptions struct {
	Name             string
	VariablePrefix   string
	VariableDefaults map[string]string
	FontStyle        *bool
}

func CreateCSSVariablesTheme(options ...CSSVariablesThemeOptions) *Theme {
	var o CSSVariablesThemeOptions
	if len(options) > 0 {
		o = options[0]
	}
	if o.Name == "" {
		o.Name = "css-variables"
	}
	if o.VariablePrefix == "" {
		o.VariablePrefix = "--shiki-"
	}
	variable := func(name string) string {
		value := "var(" + o.VariablePrefix + name
		if d := o.VariableDefaults[name]; d != "" {
			value += ", " + d
		}
		return value + ")"
	}
	t := &Theme{Name: o.Name, Type: "dark", Colors: map[string]any{"editor.foreground": variable("foreground"), "editor.background": variable("background")}}
	for _, name := range ansiNames {
		suffix := strings.ToLower(name)
		if strings.HasPrefix(name, "Bright") {
			suffix = "bright-" + strings.ToLower(name[6:])
		}
		t.Colors["terminal.ansi"+name] = variable("ansi-" + suffix)
	}
	add := func(scopes []string, color, font string) {
		style := ThemeStyle{}
		if color != "" {
			style.Foreground = variable(color)
		}
		if font != "" && (o.FontStyle == nil || *o.FontStyle) {
			style.FontStyle = new(font)
		}
		t.TokenColors = append(t.TokenColors, ThemeSetting{Scope: scopes, Settings: style})
	}
	add([]string{"keyword.operator.accessor", "meta.group.braces.round.function.arguments", "meta.template.expression", "markup.fenced_code meta.embedded.block"}, "foreground", "")
	add([]string{"emphasis"}, "", "italic")
	add([]string{"strong", "markup.heading.markdown", "markup.bold.markdown"}, "", "bold")
	add([]string{"markup.italic.markdown"}, "", "italic")
	add([]string{"meta.link.inline.markdown"}, "token-link", "underline")
	add([]string{"string", "markup.fenced_code", "markup.inline"}, "token-string", "")
	add([]string{"comment", "string.quoted.docstring.multi"}, "token-comment", "")
	add([]string{"constant.numeric", "constant.language", "constant.other.placeholder", "constant.character.format.placeholder", "variable.language.this", "variable.other.object", "variable.other.class", "variable.other.constant", "meta.property-name", "meta.property-value", "support"}, "token-constant", "")
	add([]string{"keyword", "storage.modifier", "storage.type", "storage.control.clojure", "entity.name.function.clojure", "entity.name.tag.yaml", "support.function.node", "support.type.property-name.json", "punctuation.separator.key-value", "punctuation.definition.template-expression"}, "token-keyword", "")
	add([]string{"variable.parameter.function"}, "token-parameter", "")
	add([]string{"support.function", "entity.name.type", "entity.other.inherited-class", "meta.function-call", "meta.instance.constructor", "entity.other.attribute-name", "entity.name.function", "constant.keyword.clojure"}, "token-function", "")
	add([]string{"entity.name.tag", "string.quoted", "string.regexp", "string.interpolated", "string.template", "string.unquoted.plain.out.yaml", "keyword.other.template"}, "token-string-expression", "")
	add([]string{"punctuation.definition.arguments", "punctuation.definition.dict", "punctuation.separator", "meta.function-call.arguments"}, "token-punctuation", "")
	add([]string{"markup.underline.link", "punctuation.definition.metadata.markdown"}, "token-link", "")
	add([]string{"beginning.punctuation.definition.list.markdown"}, "token-string", "")
	add([]string{"punctuation.definition.string.begin.markdown", "punctuation.definition.string.end.markdown", "string.other.link.title.markdown", "string.other.link.description.markdown"}, "token-keyword", "")
	add([]string{"markup.inserted", "meta.diff.header.to-file", "punctuation.definition.inserted"}, "token-inserted", "")
	add([]string{"markup.deleted", "meta.diff.header.from-file", "punctuation.definition.deleted"}, "token-deleted", "")
	add([]string{"markup.changed", "punctuation.definition.changed"}, "token-changed", "")
	return t
}
