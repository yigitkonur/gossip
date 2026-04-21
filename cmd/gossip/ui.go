package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const banner = `   ____  ___   ____ ____ ___ ____
  / ___|/ _ \ / ___/ ___|_ _|  _ \
 | |  _| | | |\___ \___ \| || |_) |
 | |_| | |_| | ___) |__) | ||  __/
  \____|\___/ |____/____/___|_|`

const bannerTagline = "Claude Code ⇄ Codex — local collaboration bridge"

// ANSI color helpers. Writes escape codes only when stdout looks like a TTY
// that understands colors. Honors NO_COLOR (https://no-color.org/).
type ansi struct {
	enabled bool
}

var ui = newANSI(os.Stdout)

func newANSI(w io.Writer) ansi {
	if os.Getenv("NO_COLOR") != "" {
		return ansi{enabled: false}
	}
	f, ok := w.(*os.File)
	if !ok {
		return ansi{enabled: false}
	}
	info, err := f.Stat()
	if err != nil {
		return ansi{enabled: false}
	}
	return ansi{enabled: info.Mode()&os.ModeCharDevice != 0}
}

func (a ansi) wrap(code, s string) string {
	if !a.enabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (a ansi) bold(s string) string   { return a.wrap("1", s) }
func (a ansi) dim(s string) string    { return a.wrap("2", s) }
func (a ansi) red(s string) string    { return a.wrap("31", s) }
func (a ansi) green(s string) string  { return a.wrap("32", s) }
func (a ansi) yellow(s string) string { return a.wrap("33", s) }
func (a ansi) blue(s string) string   { return a.wrap("34", s) }
func (a ansi) magenta(s string) string { return a.wrap("35", s) }
func (a ansi) cyan(s string) string   { return a.wrap("36", s) }

// paintedBanner returns the ASCII banner, colorized when the terminal supports it.
func paintedBanner() string {
	var b strings.Builder
	b.WriteString(ui.cyan(banner))
	b.WriteString("\n")
	b.WriteString(ui.dim(bannerTagline))
	b.WriteString("\n")
	return b.String()
}

func fprintBanner(w io.Writer) {
	fmt.Fprintln(w, paintedBanner())
}
