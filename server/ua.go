package main

import "strings"

// Enum labels. Index = stored value. Append only, never reorder.
var (
	browsers = []string{"Other", "Chrome", "Firefox", "Safari", "Edge", "Opera", "Samsung", "Brave"}
	oses     = []string{"Other", "Windows", "macOS", "Linux", "Android", "iOS", "ChromeOS"}
	devices  = []string{"Desktop", "Mobile", "Tablet"}
)

var botMarkers = []string{"bot", "crawl", "spider", "slurp", "headless", "lighthouse", "preview", "curl/", "wget", "python", "go-http", "httpclient", "java/", "node-fetch", "axios", "monitor", "uptime"}

func isBot(ua string) bool {
	if ua == "" {
		return true
	}
	l := strings.ToLower(ua)
	for _, m := range botMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// parseUA is a deliberately coarse UA classifier. width is the screen width reported by the client (0 = unknown).
func parseUA(ua string, width int) (browser, os, device int) {
	switch {
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
