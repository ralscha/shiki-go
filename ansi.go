package shiki

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var ansiNames = []string{"Black", "Red", "Green", "Yellow", "Blue", "Magenta", "Cyan", "White", "BrightBlack", "BrightRed", "BrightGreen", "BrightYellow", "BrightBlue", "BrightMagenta", "BrightCyan", "BrightWhite"}
var ansiDefaults = []string{"#000000", "#cd3131", "#0DBC79", "#E5E510", "#2472C8", "#BC3FBC", "#11A8CD", "#E5E5E5", "#666666", "#F14C4C", "#23D18B", "#F5F543", "#3B8EEA", "#D670D6", "#29B8DB", "#FFFFFF"}

func ansiColor(index int, palette []string) string {
	if index < 0 || index > 255 {
		return ""
	}
	if index < 16 {
		return palette[index]
	}
	if index >= 232 {
		v := 8 + (index-232)*10
		return fmt.Sprintf("#%02x%02x%02x", v, v, v)
	}
	index -= 16
	levels := []int{0, 95, 135, 175, 215, 255}
	return fmt.Sprintf("#%02x%02x%02x", levels[index/36], levels[index/6%6], levels[index%6])
}
func dimColor(c string) string {
	if strings.HasPrefix(c, "#") {
		v := c[1:]
		if len(v) == 3 || len(v) == 4 {
			var b strings.Builder
			for _, r := range v {
				b.WriteRune(r)
				b.WriteRune(r)
			}
			v = b.String()
		}
		if len(v) == 6 {
			return "#" + v + "80"
		}
		if len(v) == 8 {
			a, err := strconv.ParseUint(v[6:], 16, 8)
			if err == nil {
				return fmt.Sprintf("#%s%02x", v[:6], (a+1)/2)
			}
		}
	}
	if strings.HasPrefix(c, "var(--") && strings.Contains(c, "-ansi-") && strings.HasSuffix(c, ")") {
		return c[:len(c)-1] + "-dim)"
	}
	return c
}
func tokenizeANSI(code string, theme *Theme, o Options) [][]Token {
	palette := append([]string(nil), ansiDefaults...)
	for i, name := range ansiNames {
		if c, ok := theme.Colors["terminal.ansi"+name].(string); ok && c != "" {
			palette[i] = c
		}
	}
	fg, bg := "", ""
	var font FontStyle
	dim, reverse := false, false
	r := colorReplacements(theme, o)
	lines := SplitLines(code)
	out := make([][]Token, len(lines))
	for i, line := range lines {
		out[i] = []Token{}
		text := line.Content
		start := 0
		emit := func(end int) {
			if end <= start {
				return
			}
			color, background := fg, bg
			if color == "" {
				color = theme.FG
			}
			if reverse {
				color = bg
				if color == "" {
					color = theme.BG
				}
				background = fg
				if background == "" {
					background = theme.FG
				}
			}
			color = applyReplacement(color, r)
			background = applyReplacement(background, r)
			if dim {
				color = dimColor(color)
			}
			out[i] = append(out[i], Token{Content: text[start:end], Offset: line.Offset, Color: color, BGColor: background, FontStyle: font, styled: true})
		}
		for pos := 0; pos < len(text); {
			if text[pos] != 27 {
				pos++
				continue
			}
			if pos+1 >= len(text) || text[pos+1] != '[' || !strings.Contains(text[pos+2:], "m") {
				break
			}
			emit(pos)
			end := pos + 1
			if end < len(text) && text[end] == '[' {
				end++
				paramsStart := end
				for end < len(text) && text[end] != 'm' {
					end++
				}
				if end < len(text) && text[end] == 'm' {
					params := strings.Split(text[paramsStart:end], ";")
					numbers := make([]int, len(params))
					for j, s := range params {
						value, err := strconv.Atoi(ansiIntegerPrefix.FindString(s))
						if err != nil {
							value = -1
						}
						numbers[j] = value
					}
					for pass := range 2 {
						for j := 0; j < len(numbers); j++ {
							n := numbers[j]
							if n < 0 {
								continue
							}
							reset := n == 0 || n >= 21 && n <= 29 || n == 39 || n == 49
							if (pass == 0) != reset {
								if (n == 38 || n == 48) && j+1 < len(numbers) {
									switch numbers[j+1] {
									case 2:
										j += 4
									case 5:
										j += 2
									}
								}
								continue
							}
							switch {
							case n == 0:
								fg = ""
								bg = ""
								font = 0
								dim = false
								reverse = false
							case n == 1:
								font |= FontStyleBold
							case n == 2:
								dim = true
							case n == 3:
								font |= FontStyleItalic
							case n == 4:
								font |= FontStyleUnderline
							case n == 7:
								reverse = true
							case n == 9:
								font |= FontStyleStrikethrough
							case n == 22:
								font &^= FontStyleBold
								dim = false
							case n == 21:
								font &^= FontStyleBold
							case n == 23:
								font &^= FontStyleItalic
							case n == 24:
								font &^= FontStyleUnderline
							case n == 27:
								reverse = false
							case n == 29:
								font &^= FontStyleStrikethrough
							case n == 39:
								fg = ""
							case n == 49:
								bg = ""
							case n >= 30 && n <= 37:
								fg = palette[n-30]
							case n >= 40 && n <= 47:
								bg = palette[n-40]
							case n >= 90 && n <= 97:
								fg = palette[n-90+8]
							case n >= 100 && n <= 107:
								bg = palette[n-100+8]
							case n == 38 || n == 48:
								color := ""
								if j+2 < len(numbers) && numbers[j+1] == 5 {
									color = ansiColor(numbers[j+2], palette)
									j += 2
								} else if j+4 < len(numbers) && numbers[j+1] == 2 {
									color = fmt.Sprintf("#%02x%02x%02x", max(0, min(255, numbers[j+2])), max(0, min(255, numbers[j+3])), max(0, min(255, numbers[j+4])))
									j += 4
								}
								if n == 38 {
									fg = color
								} else {
									bg = color
								}
							}
						}
					}
				}
				if end < len(text) {
					end++
				}
			} else if end < len(text) && text[end] == ']' {
				end++
				for end < len(text) && text[end] != 7 && (text[end] != 27 || end+1 >= len(text) || text[end+1] != '\\') {
					end++
				}
				if end < len(text) {
					if text[end] == 27 {
						end += 2
					} else {
						end++
					}
				}
			}
			pos = end
			start = end
		}
		emit(len(text))
	}
	return out
}

var ansiIntegerPrefix = regexp.MustCompile(`^[+-]?\d+`)

func (h *Highlighter) CodeToANSI(code string, o Options) (string, error) {
	lines, err := h.CodeToTokensBase(code, o)
	if err != nil {
		return "", err
	}
	theme, err := h.GetTheme(o.Theme)
	if err != nil {
		return "", err
	}
	level := 3
	if o.ANSIColorLevel != nil {
		level = *o.ANSIColorLevel
	}
	var b strings.Builder
	for _, line := range lines {
		for _, token := range line {
			text := token.Content
			if text == "" {
				continue
			}
			color := token.Color
			if color == "" {
				color = theme.FG
			}
			if level > 0 {
				if color != "" {
					r, g, blue := ansiRGB(color, theme.Type)
					text = wrapANSI(text, ansiColorCode(r, g, blue, level), "39")
				}
				for _, style := range []struct {
					bit         FontStyle
					open, close string
				}{{FontStyleBold, "1", "22"}, {FontStyleItalic, "3", "23"}, {FontStyleUnderline, "4", "24"}, {FontStyleStrikethrough, "9", "29"}} {
					if token.FontStyle&style.bit != 0 {
						text = wrapANSI(text, style.open, style.close)
					}
				}
			}
			b.WriteString(text)
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func wrapANSI(text, open, close string) string {
	start, end := "\x1b["+open+"m", "\x1b["+close+"m"
	return start + strings.ReplaceAll(text, end, start) + end
}

// The reference CLI applies alpha before passing the result to ansis. Its
// hexadecimal conversion keeps fractional components; reproduce that behavior
// so themes containing alpha colors have the same terminal output.
func hexFloat(value float64) string {
	whole := math.Floor(value)
	s := strconv.FormatInt(int64(whole), 16)
	fraction := value - whole
	if fraction != 0 {
		s += "."
		for fraction != 0 {
			fraction *= 16
			digit := math.Floor(fraction)
			s += string("0123456789abcdef"[int(digit)])
			fraction -= digit
		}
	}
	if len(s) == 1 {
		s = "0" + s
	}
	return s
}

var ansiHexPattern = regexp.MustCompile(`(?i)[a-f\d]{3,6}`)

func ansiRGB(color, themeType string) (int, int, int) {
	hex := strings.Replace(color, "#", "", 1)
	if len(hex) == 3 || len(hex) == 4 {
		var expanded strings.Builder
		for _, c := range hex {
			expanded.WriteRune(c)
			expanded.WriteRune(c)
		}
		hex = expanded.String()
	}
	if len(hex) == 6 {
		hex += "ff"
	}
	if len(hex) < 8 {
		return 0, 0, 0
	}
	value, err := strconv.ParseUint(hex[:8], 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	alpha := float64(value&255) / 255
	var adjusted strings.Builder
	for _, shift := range []int{24, 16, 8} {
		component := float64(value>>shift&255) * alpha
		if themeType == "light" {
			component += 255 * (1 - alpha)
		}
		adjusted.WriteString(hexFloat(component))
	}
	match := ansiHexPattern.FindString(adjusted.String())
	if len(match) == 3 {
		match = string([]byte{match[0], match[0], match[1], match[1], match[2], match[2]})
	}
	if len(match) != 6 {
		return 0, 0, 0
	}
	n, _ := strconv.ParseUint(match, 16, 32)
	return int(n >> 16), int(n >> 8 & 255), int(n & 255)
}
func ansiColorCode(r, g, b, level int) string {
	if level >= 3 {
		return fmt.Sprintf("38;2;%d;%d;%d", r, g, b)
	}
	index := 0
	if r == g && g == b {
		if r < 8 {
			index = 16
		} else if r > 248 {
			index = 231
		} else {
			index = int(math.Round(24*float64(r-8)/247)) + 232
		}
	} else {
		index = 16 + 36*int(math.Round(float64(r)/51)) + 6*int(math.Round(float64(g)/51)) + int(math.Round(float64(b)/51))
	}
	if level == 2 {
		return fmt.Sprintf("38;5;%d", index)
	}
	code := 30
	if index < 8 {
		code += index
	} else if index < 16 {
		code = 82 + index
	} else if index > 231 {
		if index > 243 {
			code = 37
		}
	} else {
		i := index - 16
		red, green, blue := i/36, (i%36)/6, i%6
		if red > 2 {
			code++
		}
		if green > 2 {
			code += 2
		}
		if blue > 2 {
			code += 4
		}
		if max(red, green, blue) > 4 {
			code += 60
		}
	}
	return strconv.Itoa(code)
}
func CodeToANSI(code string, o Options) (string, error) {
	h, err := shorthand(code, o)
	if err != nil {
		return "", err
	}
	return h.CodeToANSI(code, o)
}
