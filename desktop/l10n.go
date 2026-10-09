package main

import (
	"fmt"
	"strings"
)

// L translates an English format string into the language chosen in the
// preferences (Auto follows the system's) and fills it in.
func L(format string, a ...any) string {
	if lang == "zh" {
		if s, ok := zh[format]; ok {
			format = s
		}
	}
	if len(a) == 0 {
		return format
	}
	return fmt.Sprintf(format, a...)
}

var lang = "en"

// setLanguage picks the catalog: choice is a preference, locale the
// system's, such as "zh-CN".
func setLanguage(choice, locale string) {
	switch choice {
	case "English":
		lang = "en"
	case "中文":
		lang = "zh"
	default:
		lang = "en"
		if strings.HasPrefix(strings.ToLower(locale), "zh") {
			lang = "zh"
		}
	}
}
