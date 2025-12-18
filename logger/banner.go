package logger

import (
	"regexp"
	"strings"
)

import (
	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

import (
	appinfo "boltcache/appinfo"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func colorStrip(s string) string {
	return ansi.ReplaceAllString(s, "")
}

// StartupMessage prints a stylized startup banner with the application version.
func StartupMessage(version string) {
	banner := []string{
		"██████   ██████  ██      ████████  ██████  █████   ██████ ██   ██ ███████ ",
		"██   ██ ██    ██ ██         ██    ██      ██   ██ ██      ██   ██ ██      ",
		"██████  ██    ██ ██         ██    ██      ███████ ██      ███████ █████   ",
		"██   ██ ██    ██ ██         ██    ██      ██   ██ ██      ██   ██ ██      ",
		"██████   ██████  ███████    ██     ██████ ██   ██  ██████ ██   ██ ███████ ",
		"",                                                                
	}

	title := appinfo.Title
	desc := appinfo.Description

	// Colors
	borderColor := color.New(color.FgHiBlack, color.Bold)
	titleColor := color.New(color.FgHiCyan, color.Bold)
	versionColor := color.New(color.FgHiYellow, color.Bold)
	descColor := color.New(color.FgHiBlack)

	titleWithVersion := titleColor.Sprintf("%s", title) + versionColor.Sprintf(" v%s", version)

	// Calculate max width for the banner
	maxLen := 0
	for _, line := range banner {
		l := runewidth.StringWidth(line)
		if l > maxLen {
			maxLen = l
		}
	}
	if runewidth.StringWidth(title) > maxLen {
		maxLen = runewidth.StringWidth(titleWithVersion)
	}
	if runewidth.StringWidth(desc) > maxLen {
		maxLen = runewidth.StringWidth(desc)
	}

	padding := 4
	totalWidth := maxLen + padding*2

	border := "─"

	borderColor.Println("┌" + strings.Repeat(border, totalWidth) + "┐")

	for _, line := range banner {
		lineLen := runewidth.StringWidth(line)
		leftPad := padding
		rightPad := totalWidth - lineLen - leftPad
		borderColor.Printf("│%s%s%s│\n", strings.Repeat(" ", leftPad), line, strings.Repeat(" ", rightPad))
	}

	// Title with version
	lineLen := runewidth.StringWidth(colorStrip(titleWithVersion))
	leftPad := (totalWidth - lineLen) / 2
	rightPad := totalWidth - lineLen - leftPad
	borderColor.Printf("│%s%s%s│\n", strings.Repeat(" ", leftPad), titleWithVersion, strings.Repeat(" ", rightPad))

	// Description
	lineLen = runewidth.StringWidth(desc)
	leftPad = (totalWidth - lineLen) / 2
	rightPad = totalWidth - lineLen - leftPad
	borderColor.Printf("│%s%s%s│\n", strings.Repeat(" ", leftPad), descColor.Sprint(desc), strings.Repeat(" ", rightPad))

	borderColor.Println("└" + strings.Repeat(border, totalWidth) + "┘")
}
