package main

import (
	"fmt"
	"strings"
	"time"
)

// plainText is a reply's markdown as plain text, as it reads on screen:
// no emphasis marks or fences, list items with their bullets or numbers,
// quotes and nested lists indented, table cells apart by tabs, and a link
// with its address after its words when they differ.
func plainText(src string) string {
	var b strings.Builder
	writeBlocks(&b, parseMarkdown(src), "")
	return strings.TrimRight(b.String(), "\n")
}

func writeBlocks(b *strings.Builder, blocks []mdBlock, indent string) {
	for i, bl := range blocks {
		if i > 0 && indent == "" {
			b.WriteString("\n") // blocks apart by a blank line at the top level
		}
		writeBlock(b, bl, indent)
	}
}

func writeBlock(b *strings.Builder, bl mdBlock, indent string) {
	line := func(s string) {
		for _, l := range strings.Split(s, "\n") {
			b.WriteString(strings.TrimRight(indent+l, " ") + "\n")
		}
	}
	switch bl.kind {
	case blockParagraph, blockHeading:
		line(spansText(bl.spans))
	case blockCode:
		line(bl.text)
	case blockRule:
		line("———")
	case blockQuote:
		writeBlocks(b, bl.blocks, indent+"  ")
	case blockTable:
		for _, row := range bl.rows {
			cells := make([]string, len(row))
			for i, c := range row {
				cells[i] = spansText(c)
			}
			line(strings.Join(cells, "\t"))
		}
	case blockList:
		for i, item := range bl.items {
			marker := "• "
			if bl.ordered {
				marker = fmt.Sprintf("%d. ", bl.start+i)
			}
			if c := bl.checked[i]; c != nil {
				marker = "☐ "
				if *c {
					marker = "☑ "
				}
			}
			for j, sub := range item {
				var sb strings.Builder
				writeBlock(&sb, sub, "")
				text := strings.TrimRight(sb.String(), "\n")
				for k, l := range strings.Split(text, "\n") {
					prefix := strings.Repeat(" ", len([]rune(marker)))
					if j == 0 && k == 0 {
						prefix = marker
					}
					b.WriteString(strings.TrimRight(indent+prefix+l, " ") + "\n")
				}
			}
		}
	}
}

func spansText(spans []mdSpan) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.text)
		if s.link != "" && s.link != s.text && strings.TrimPrefix(s.link, "mailto:") != s.text {
			b.WriteString(" (" + s.link + ")")
		}
	}
	return b.String()
}

// copyReply puts a reply on the clipboard, as plain text or as its
// markdown, and marks its button copied for a moment.
func (a *app) copyReply(r row, asMarkdown bool) {
	if a.copyText == nil {
		return
	}
	text := r.text
	if !asMarkdown {
		text = plainText(r.text)
	}
	a.copyText(text)
	key := copiedKey(r, asMarkdown)
	a.copied = key
	post := a.post
	time.AfterFunc(1500*time.Millisecond, func() {
		post(func() {
			if a.copied == key {
				a.copied = ""
			}
		})
	})
}

func copiedKey(r row, asMarkdown bool) string {
	return fmt.Sprintf("%d-%s-%v", r.seq, r.turn, asMarkdown)
}
