package box

import "bytes"
import "fmt"
import "io"
import "regexp"
import "strings"

import "github.com/mattn/go-runewidth"

import "../conio"

var ansiCutter = regexp.MustCompile("\x1B[^a-zA-Z]*[A-Za-z]")

func Print(nodes []string, out io.Writer) {
	width, _ := conio.GetScreenSize()
	maxLen := 1
	for _, finfo := range nodes {
		length := runewidth.StringWidth(ansiCutter.ReplaceAllString(finfo, ""))
		if length > maxLen {
			maxLen = length
		}
	}
	nodePerLine := (width - 1) / (maxLen + 1)
	if nodePerLine <= 0 {
		nodePerLine = 1
	}
	nlines := (len(nodes) + nodePerLine - 1) / nodePerLine

	lines := make([]bytes.Buffer, nlines)
	for i, finfo := range nodes {
		lines[i%nlines].WriteString(finfo)
		lines[i%nlines].WriteString(
			strings.Repeat(" ", maxLen+1-
				runewidth.StringWidth(ansiCutter.ReplaceAllString(finfo, ""))))
	}
	for _, line := range lines {
		fmt.Fprintln(out, strings.TrimSpace(line.String()))
	}
}
