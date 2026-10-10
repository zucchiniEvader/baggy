package main

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// A reply's markdown is parsed into blocks of spans, which draw as RichText
// paragraphs, code boxes, lists, quotes, and tables as lines of cells.

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

// markdown draws a reply.
func markdown(c *ui.Context, src string) {
	blocks := parseMarkdown(src)
	ui.Column(c).MinWidth(0).Gap(10).LineHeight(1.38).Children(func() { drawBlocks(c, blocks, false) })
}

func drawBlocks(c *ui.Context, blocks []mdBlock, nested bool) {
	for _, b := range blocks {
		drawBlock(c, b, nested)
	}
}

func drawSpans(c *ui.Context, spans []mdSpan, bold bool) ui.Element {
	t := c.Theme()
	return ui.RichText(c).MinWidth(0).Children(func() {
		for _, s := range spans {
			el := ui.Text(c, s.text)
			if s.bold || bold {
				el.FontWeight(600)
			}
			if s.italic {
				el.Italic()
			}
			if s.strike {
				el.Strikethrough()
			}
			if s.code {
				el.Font(monoFont).TextBackground(t.Surface)
			}
			if s.link != "" {
				href := s.link
				el.Underline().TextColor(t.Accent).Cursor(ui.CursorPointer).Tooltip(href)
				if el.Clicked() {
					openLink(href)
				}
			}
		}
	})
}

func drawBlock(c *ui.Context, b mdBlock, nested bool) {
	t := c.Theme()
	switch b.kind {
	case blockParagraph:
		drawSpans(c, b.spans, false)
	case blockHeading:
		h := drawSpans(c, b.spans, true).FontWeight(700)
		if b.level <= 2 {
			h.FontSize(16)
		}
	case blockCode:
		ui.Column(c).Padding(8, 10).Radius(7).Background(t.Surface).MinWidth(0).Children(func() {
			s := b.text
			if s == "" {
				s = " "
			}
			ui.Text(c, s).Font(monoFont).FontSize(12)
		})
	case blockList:
		ui.Column(c).Gap(2).MinWidth(0).Children(func() {
			for i, item := range b.items {
				marker := "•"
				if b.ordered {
					marker = strconv.Itoa(b.start+i) + "."
				}
				if box := b.checked[i]; box != nil {
					marker = "☐"
					if *box {
						marker = "☑"
					}
				}
				ui.Row(c).AlignItems(ui.Start).MinWidth(0).Children(func() {
					ui.Text(c, marker).MinWidth(18).Padding(0, 4, 0, 0).NoWrap().Unselectable()
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() { drawBlocks(c, item, true) })
				})
			}
		})
	case blockQuote:
		ui.Row(c).Gap(8).MinWidth(0).Children(func() {
			ui.Column(c).Width(2).Radius(1).Background(t.Border).Height(16)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(6).TextColor(t.TextMuted).Children(func() { drawBlocks(c, b.blocks, true) })
		})
	case blockTable:
		ui.Column(c).Gap(2).MinWidth(0).Children(func() {
			for i, cells := range b.rows {
				var line []mdSpan
				for j, cell := range cells {
					if j > 0 {
						line = append(line, mdSpan{text: "   "})
					}
					line = append(line, cell...)
				}
				drawSpans(c, line, i == 0)
			}
		})
	case blockRule:
		ui.Divider(c)
	}
}
