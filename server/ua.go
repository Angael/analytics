package main

import "strings"

// Enum labels. Index = stored value. Append only, never reorder.
var (
	browsers = []string{"Other", "Chrome", "Firefox", "Safari", "Edge", "Opera", "Samsung", "Brave", "Electron"}
	oses     = []string{"Other", "Windows", "macOS", "Linux", "Android", "iOS", "ChromeOS"}
	devices  = []string{"Desktop", "Mobile", "Tablet"}
)

var botMarkers = []string{"bot", "crawl", "spider", "slurp", "headless", "lighthouse", "preview", "curl/", "wget", "python", "go-http", "httpclient", "java/", "node-fetch", "axios", "monitor", "uptime"}

func isBot(ua string) bool {
	if ua == "" {
		return true
	}
	l := strings.ToLower(stripAppToken(ua))
	for _, m := range botMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// stripAppToken removes the product token Electron inserts for the app itself
// ("... (KHTML, like Gecko) my-app/1.2.0 Chrome/130 ... Electron/33 ..."), so an app
// named e.g. "Uptime Monitor" isn't mistaken for a bot. Non-Electron UAs are returned unchanged.
func stripAppToken(ua string) string {
	if !strings.Contains(ua, "Electron/") {
		return ua
	}
	const gecko = "like Gecko) "
	i := strings.Index(ua, gecko)
	if i < 0 {
		return ua
	}
	rest := ua[i+len(gecko):]
	j := strings.Index(rest, "Chrome/")
	if j < 0 {
		return ua
	}
	// Cut at the start of the token holding "Chrome/" so "HeadlessChrome/" survives intact.
	k := strings.LastIndexByte(rest[:j], ' ') + 1
	return ua[:i+len(gecko)] + rest[k:]
}

// parseUA is a deliberately coarse UA classifier. width is the screen width reported by the client (0 = unknown).
func parseUA(ua string, width int) (browser, os, device int) {
	switch {
	case strings.Contains(ua, "Electron/"):
		browser = 8
	case strings.Contains(ua, "Edg/"):
		browser = 4
	case strings.Contains(ua, "OPR/"):
		browser = 5
	case strings.Contains(ua, "SamsungBrowser"):
		browser = 6
	case strings.Contains(ua, "Brave"):
		browser = 7
	case strings.Contains(ua, "Firefox/") || strings.Contains(ua, "FxiOS"):
		browser = 2
	case strings.Contains(ua, "Chrome/") || strings.Contains(ua, "CriOS"):
		browser = 1
	case strings.Contains(ua, "Safari/"):
		browser = 3
	}
	switch {
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") || strings.Contains(ua, "iPod"):
		os = 5
	case strings.Contains(ua, "Android"):
		os = 4
	case strings.Contains(ua, "CrOS"):
		os = 6
	case strings.Contains(ua, "Windows"):
		os = 1
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		os = 2
	case strings.Contains(ua, "Linux"):
		os = 3
	}
	switch {
	case strings.Contains(ua, "iPad") || strings.Contains(ua, "Tablet") || (os == 4 && !strings.Contains(ua, "Mobile")):
		device = 2
	case strings.Contains(ua, "Mobi") || strings.Contains(ua, "iPhone"):
		device = 1
	case width > 0 && width < 768:
		device = 1
	}
	return
}
