package logger

import (
	"fmt"
	// "net/http"
	// "time"
	// "strings"
)

import (
	"github.com/fatih/color"
)

const pathWidth = 35

// Returns the formatted server URL with cyan color for console output.
func GetServerHost(host string, port string) string {
	serverUrlColor := color.New(color.FgCyan).SprintFunc()
	serverUrl := fmt.Sprintf("http://%s%s", host, port)

	return serverUrlColor(serverUrl)
}

// Prints a standardized success message when the server starts.
func LogServerStart(port string) {
	LogSuccess(fmt.Sprintf("BoltCache REST API started on %s", GetServerHost("localhost", port)), 1)
}

func LogServerStartWithMsg(msg, addr string) {
	host, port, _ := splitHostPort(addr)
	port_with_colon := ":" + port
	Log(msg, GetServerHost(host, port_with_colon))
}

// LogRoute logs detailed information about a single HTTP request.
// It includes method, path, IP, status code, response time, and optional prefix.
func LogRoute(method, path, desc string) {
	methodColors := map[string]*color.Color{
		"GET":     color.New(color.FgHiGreen),
		"POST":    color.New(color.FgHiCyan),
		"PUT":     color.New(color.FgYellow),
		"DELETE":  color.New(color.FgHiRed),
		"PATCH":   color.New(color.FgMagenta),
		"OPTIONS": color.New(color.FgHiWhite),
	}

	methodColor, ok := methodColors[method]
	if !ok {
		methodColor = color.New(color.FgWhite, color.Bold)
	}

	prefixLog := bannerStyle.Sprintf("→")
	pathColor := color.New(color.FgHiBlack)
	descColor := color.New(color.FgWhite)

	msg := fmt.Sprintf(
		"%s %s %-*s - %s",
		prefixLog,
		methodColor.Sprintf("%-7s", method),
		pathWidth,
		pathColor.Sprint(path),
		descColor.Sprint(desc),
	)

	fmt.Println(msg)
}


// --- Log Helpers --- //
//
// This section provides standardized logging utilities.
// All helpers delegate to `logWithType`, which handles consistent formatting and colorization.
// - LogSuccess → prints success messages (green).
// - LogError   → prints error messages (red).
// - LogWarn    → prints warning messages (yellow).
// - LogInfo    → prints informational messages (blue).
//
// Each function accepts the log message and optional empty line padding (addEmptyLines).
// Designed to keep console output clean, color-coded, and developer-friendly.

func LogSuccess(msg string, addEmptyLines ...int) {
	logWithType("OK", successStyle, msg, addEmptyLines...)
}

func LogError(msg string, addEmptyLines ...int) {
	logWithType("ERROR", errorStyle, msg, addEmptyLines...)
}

func LogWarn(msg string, addEmptyLines ...int) {
	logWithType("WARN", warnStyle, msg, addEmptyLines...)
}

func LogInfo(msg string, addEmptyLines ...int) {
	logWithType("INFO", infoStyle, msg, addEmptyLines...)
}

func Log(msg string, args ...any) {
	fmt.Print(printTimestamp(true))
	fmt.Println(messageStyle.Sprintf(msg, args...))
}
