package log

import "regexp"

var uncolor = regexp.MustCompile("\x1b\\[([0-9]+;)*[0-9]+m")

func Uncolor(text string) string {
	return uncolor.ReplaceAllString(text, "")
}
