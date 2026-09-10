package main

import (
	"encoding/json"
	"flag"
	"fmt"
	shiki "github.com/ralscha/shiki-go"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) (err error) {
	flags := flag.NewFlagSet("shiki", flag.ContinueOnError)
	flags.SetOutput(stderr)
	theme := flags.String("theme", "vitesse-dark", "Color theme")
	lang := flags.String("lang", "", "Language (otherwise inferred from file extension)")
	format := flags.String("format", "ansi", "Output format: ansi, html, hast, tokens")
	listThemes := flags.Bool("list-themes", false, "List available themes")
	listLangs := flags.Bool("list-langs", false, "List available languages")
	version := flags.Bool("version", false, "Print version")
	flags.BoolVar(version, "v", false, "Print version")
	parsed, err := interspersedArgs(flags, args)
	if err != nil {
		return err
	}
	if err := flags.Parse(parsed); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *version {
		_, err = fmt.Fprintln(stdout, shiki.Version)
		return err
	}
	if *listThemes {
		for _, t := range shiki.BundledThemes() {
			if _, err = fmt.Fprintln(stdout, t.ID); err != nil {
				return err
			}
		}
		return nil
	}
	if *listLangs {
		for _, l := range shiki.BundledLanguages() {
			if _, err = fmt.Fprintln(stdout, l.ID); err != nil {
				return err
			}
		}
		for _, l := range shiki.BundledLanguages() {
			for _, alias := range l.Aliases {
				if _, err = fmt.Fprintln(stdout, alias); err != nil {
					return err
				}
			}
		}
		return nil
	}
	switch *format {
	case "ansi", "html", "hast", "tokens":
	default:
		return fmt.Errorf("unknown format %q", *format)
	}
	h, err := shiki.NewHighlighter(shiki.HighlighterOptions{Themes: []string{*theme}})
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := h.Close(); err == nil {
			err = closeErr
		}
	}()
	files := flags.Args()
	if len(files) == 0 {
		if file, ok := stdin.(*os.File); ok {
			if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
				flags.Usage()
				return nil
			}
		}
		files = []string{"-"}
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for _, file := range files {
		var content []byte
		extension := ""
		if file == "-" {
			content, err = io.ReadAll(stdin)
		} else if strings.HasPrefix(file, "https://") || strings.HasPrefix(file, "http://") {
			var response *http.Response
			response, err = client.Get(file)
			if err == nil {
				if response.StatusCode < 200 || response.StatusCode >= 300 {
					_ = response.Body.Close()
					return fmt.Errorf("fetch %s: %s", file, response.Status)
				}
				content, err = io.ReadAll(response.Body)
				if closeErr := response.Body.Close(); err == nil {
					err = closeErr
				}
			}
			u, e := url.Parse(file)
			if e == nil {
				extension = strings.TrimPrefix(filepath.Ext(u.Path), ".")
			}
		} else {
			content, err = os.ReadFile(file)
			extension = strings.TrimPrefix(filepath.Ext(file), ".")
		}
		if err != nil {
			return err
		}
		language := *lang
		if language == "" {
			language = strings.ToLower(extension)
			if language == "" {
				language = "text"
			}
		}
		if err = h.LoadLanguage(append([]string{language}, shiki.GuessEmbeddedLanguages(string(content), true)...)...); err != nil {
			return err
		}
		colorLevel := terminalColorLevel(stdout)
		options := shiki.Options{Lang: language, Theme: *theme, ANSIColorLevel: &colorLevel}
		var output string
		switch *format {
		case "html":
			output, err = h.CodeToHTML(string(content), options)
		case "ansi":
			output, err = h.CodeToANSI(string(content), options)
		case "tokens", "hast":
			var value any
			if *format == "tokens" {
				value, err = h.CodeToTokens(string(content), options)
			} else {
				value, err = h.CodeToHAST(string(content), options)
			}
			if err == nil {
				var b []byte
				b, err = json.Marshal(value)
				output = string(b)
			}
		}
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintln(stdout, output); err != nil {
			return err
		}
	}
	return nil
}

func interspersedArgs(flags *flag.FlagSet, args []string) ([]string, error) {
	var options, files []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			files = append(files, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			files = append(files, arg)
			continue
		}
		options = append(options, arg)
		name, _, assigned := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if option := flags.Lookup(name); option != nil && !assigned {
			if b, ok := option.Value.(interface{ IsBoolFlag() bool }); !ok || !b.IsBoolFlag() {
				i++
				if i >= len(args) {
					return nil, fmt.Errorf("flag needs an argument: %s", arg)
				}
				options = append(options, args[i])
			}
		}
	}
	return append(append(options, "--"), files...), nil
}

func terminalColorLevel(stdout io.Writer) int {
	if force, ok := os.LookupEnv("FORCE_COLOR"); ok {
		if force == "false" {
			return 0
		}
		if n, err := strconv.Atoi(force); err == nil && n >= 0 && n <= 3 {
			return n
		}
		return 1
	}
	if os.Getenv("NO_COLOR") != "" {
		return 0
	}
	switch os.Getenv("COLORTERM") {
	case "24bit", "truecolor":
		return 3
	case "ansi256":
		return 2
	case "ansi":
		return 1
	}
	if os.Getenv("CI") != "" {
		if os.Getenv("GITHUB_ACTIONS") != "" {
			return 3
		}
		return 1
	}
	if file, ok := stdout.(*os.File); ok && os.Getenv("TERM") != "dumb" {
		if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			if runtime.GOOS == "windows" {
				return 3
			}
			if strings.Contains(os.Getenv("TERM"), "-256") {
				return 2
			}
			return 1
		}
	}
	return 0
}
func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
