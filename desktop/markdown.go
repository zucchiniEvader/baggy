package main

import (
	"net/url"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	mdview "github.com/egoist/mygo/ui/markdown"
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// A reply shows through MyGo's markdown view (ui/markdown); its markdown is
// also parsed here into blocks of spans, which plainText writes out as the
// text Copy Text and the sidebar's previews read.

type blockKind int

const (
	blockParagraph blockKind = iota
	blockHeading
	blockCode
	blockList
	blockQuote
	blockTable
	blockRule
)

type mdBlock struct {
	kind    blockKind
	level   int // heading
	spans   []mdSpan
	text    string // code
	ordered bool
	start   int
	items   [][]mdBlock // list items
	blocks  []mdBlock   // quote
	rows    [][][]mdSpan
	checked []*bool // list items' task boxes
}

type mdSpan struct {
	text                       string
	bold, italic, code, strike bool
	link                       string
}

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

func parseMarkdown(src string) []mdBlock {
	source := []byte(src)
	doc := md.Parser().Parse(text.NewReader(source))
	return blocksOf(doc, source)
}

func blocksOf(n gast.Node, src []byte) []mdBlock {
	var out []mdBlock
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if b, ok := blockOf(c, src); ok {
			out = append(out, b)
		}
	}
	return out
}

func blockOf(n gast.Node, src []byte) (mdBlock, bool) {
	switch n := n.(type) {
	case *gast.Paragraph, *gast.TextBlock:
		return mdBlock{kind: blockParagraph, spans: spansOf(n, src, mdSpan{})}, true
	case *gast.Heading:
		return mdBlock{kind: blockHeading, level: n.Level, spans: spansOf(n, src, mdSpan{})}, true
	case *gast.FencedCodeBlock, *gast.CodeBlock:
		var b strings.Builder
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			b.Write(seg.Value(src))
		}
		return mdBlock{kind: blockCode, text: strings.TrimRight(b.String(), "\n")}, true
	case *gast.List:
		b := mdBlock{kind: blockList, ordered: n.IsOrdered(), start: n.Start}
		for it := n.FirstChild(); it != nil; it = it.NextSibling() {
			item := blocksOf(it, src)
			var checked *bool
			if len(item) > 0 && len(item[0].spans) > 0 {
				if box := taskBox(it); box != nil {
					v := box.IsChecked
					checked = &v
				}
			}
			b.items = append(b.items, item)
			b.checked = append(b.checked, checked)
		}
		return b, true
	case *gast.Blockquote:
		return mdBlock{kind: blockQuote, blocks: blocksOf(n, src)}, true
	case *gast.ThematicBreak:
		return mdBlock{kind: blockRule}, true
	case *east.Table:
		b := mdBlock{kind: blockTable}
		for r := n.FirstChild(); r != nil; r = r.NextSibling() {
			var cells [][]mdSpan
			for cell := r.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells = append(cells, spansOf(cell, src, mdSpan{}))
			}
			b.rows = append(b.rows, cells)
		}
		return b, true
	case *gast.HTMLBlock:
		var s strings.Builder
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			s.Write(seg.Value(src))
		}
		return mdBlock{kind: blockParagraph, spans: []mdSpan{{text: strings.TrimSpace(s.String())}}}, true
	}
	return mdBlock{}, false
}

func taskBox(item gast.Node) *east.TaskCheckBox {
	first := item.FirstChild()
	if first == nil || first.FirstChild() == nil {
		return nil
	}
	box, _ := first.FirstChild().(*east.TaskCheckBox)
	return box
}

func spansOf(n gast.Node, src []byte, style mdSpan) []mdSpan {
	var out []mdSpan
	add := func(s string, st mdSpan) {
		if s == "" {
			return
		}
		if k := len(out); k > 0 {
			last := out[k-1]
			if last.bold == st.bold && last.italic == st.italic && last.code == st.code && last.strike == st.strike && last.link == st.link {
				out[k-1].text += s
				return
			}
		}
		st.text = s
		out = append(out, st)
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *gast.Text:
			add(string(c.Segment.Value(src)), style)
			if c.HardLineBreak() {
				add("\n", style)
			} else if c.SoftLineBreak() {
				add(" ", style)
			}
		case *gast.String:
			add(string(c.Value), style)
		case *gast.CodeSpan:
			st := style
			st.code = true
			var b strings.Builder
			for t := c.FirstChild(); t != nil; t = t.NextSibling() {
				if tx, ok := t.(*gast.Text); ok {
					b.Write(tx.Segment.Value(src))
				}
			}
			add(b.String(), st)
		case *gast.Emphasis:
			st := style
			if c.Level >= 2 {
				st.bold = true
			} else {
				st.italic = true
			}
			for _, s := range spansOf(c, src, st) {
				add(s.text, s)
			}
		case *east.Strikethrough:
			st := style
			st.strike = true
			for _, s := range spansOf(c, src, st) {
				add(s.text, s)
			}
		case *gast.Link:
			st := style
			st.link = string(c.Destination)
			for _, s := range spansOf(c, src, st) {
				add(s.text, s)
			}
		case *gast.AutoLink:
			st := style
			st.link = string(c.URL(src))
			if c.AutoLinkType == gast.AutoLinkEmail && !strings.HasPrefix(st.link, "mailto:") {
				st.link = "mailto:" + st.link
			}
			add(string(c.Label(src)), st)
		case *gast.Image:
			add("["+string(c.Destination)+"]", style)
		case *gast.RawHTML:
			segs := c.Segments
			for i := 0; i < segs.Len(); i++ {
				seg := segs.At(i)
				add(string(seg.Value(src)), style)
			}
		case *east.TaskCheckBox:
			// shown as the item's marker
		default:
			for _, s := range spansOf(c, src, style) {
				add(s.text, s)
			}
		}
	}
	return out
}

// openLink opens a web or mail link from a reply, and nothing else.
func openLink(href string) {
	u, err := url.Parse(href)
	if err != nil {
		return
	}
	switch u.Scheme {
	case "http", "https":
		if u.Host == "" {
			return
		}
	case "mailto":
	default:
		return
	}
	go mygo.Shell.OpenExternal(u.String())
}

// replyOptions are how replies show, one for each appearance: MyGo's
// markdown view in the conversation's sizes and the palette's code color.
var replyOptions = [2]*mdview.Options{replyTheme(&lightPalette), replyTheme(&darkPalette)}

func replyTheme(p *palette) *mdview.Options {
	th := &mdview.Theme{
		FontSize:            13,
		LineHeight:          1.38,
		CodeFont:            monoFont,
		CodeBlockBackground: p.codeBg,
		CodeBlockBorder:     p.codeBg, // areas part by their background
		CodeBlockRadius:     7,
		BlockSpacing:        0.75,
	}
	for i := range th.Headings {
		th.Headings[i] = mdview.HeadingStyle{Size: 1, Weight: 700, Top: 1, Bottom: 0.4}
	}
	th.Headings[0].Size, th.Headings[1].Size = 16.0/13, 16.0/13
	return &mdview.Options{Theme: th, OnLink: openLink, ShareSelection: true}
}

// markdown draws a reply: as MyGo's markdown view once it is finished,
// and while it streams, as the stream view, which parses only what came
// and reveals it a word at a time.
func markdown(c *ui.Context, src string, streaming bool) {
	o := replyOptions[0]
	if c.Theme().Dark {
		o = replyOptions[1]
	}
	if streaming {
		mdview.Stream(c, src, o).MinWidth(0)
		return
	}
	mdview.View(c, src, o).MinWidth(0)
}
