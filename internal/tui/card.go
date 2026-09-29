package tui

import "strings"

// Cards, after the web app's (apps/web/components/ui/card.tsx): a bordered
// box with a small muted title, and content inset from the edge.
//
// The title sits in the top border rather than on a line of its own — a
// terminal has maybe forty rows, and the web's header padding would spend a
// quarter of them on titles. The border can also carry a figure on the
// right: a total, a count, which filters are on.
//
// The terminal theme draws rounded corners in the terminal's own colours.
// Cypherpunk draws it as the web does: square corners, the card surface a
// shade off the page, and the hard offset shadow its buttons and cards cast —
// here a column of shadow to the right and a half-block row beneath.

// cardExtraRows is how much a card adds around its content: two border rows,
// and Cypherpunk's shadow row. Every theme reserves the shadow row, so a list
// pages by the same number of rows whichever theme draws it.
const cardExtraRows = 3

// cardInner is the content width of a card `width` columns wide overall
// (shadow column included).
func (p palette) cardInner(width int) int {
	// Border and one space of padding each side, and the shadow column.
	return max(1, width-5)
}

// card draws `body` in a card `width` columns wide (shadow included), with
// `title` in the top border and `right` at the border's other end. Body
// lines are padded or cut to fit; the card is len(body)+3 lines tall.
func (p palette) card(title, right string, width int, body []string) []string {
	inner := p.cardInner(width)
	// The box is width-1 wide; the last column is the shadow's.
	box := inner + 4

	tl, tr, bl, br, h, v := "╭", "╮", "╰", "╯", "─", "│"
	if p.square {
		tl, tr, bl, br = "┌", "┐", "└", "┘"
	}

	edge := func(s string) string { return p.cardBorder + s + p.reset }

	// Top: ─ Title ───────── right ─
	titleText := ""
	if title != "" {
		titleText = " " + Truncate(title, max(1, box-6)) + " "
	}
	rightText := ""
	if right != "" {
		room := box - 4 - VisibleWidth(titleText)
		if room > 4 {
			rightText = " " + Truncate(right, room-2) + " "
		}
	}
	fill := max(0, box-2-1-VisibleWidth(titleText)-VisibleWidth(rightText)-1)
	top := edge(tl+h) + p.cardTitle + titleText + p.reset + edge(strings.Repeat(h, fill)) + p.cardTitle + rightText + p.reset + edge(h+tr)

	lines := []string{p.onCard(top) + p.shadowGap()}
	for _, b := range body {
		cell := Pad(Truncate(b, inner), inner)
		lines = append(lines, p.onCard(edge(v)+" "+cell+" "+edge(v))+p.shadowCell())
	}
	lines = append(lines, p.onCard(edge(bl+strings.Repeat(h, box-2)+br))+p.shadowCell())
	lines = append(lines, p.shadowRow(box))
	return lines
}

// onCard makes every reset inside a card line return to the card's surface,
// not the page's — or the rest of the row after a coloured word would drop
// back to the page colour.
func (p palette) onCard(line string) string {
	if p.cardBase == "" {
		return line
	}
	const mark = "\x00"
	line = strings.ReplaceAll(line, p.reset, mark)
	line = strings.ReplaceAll(line, reset, mark)
	return p.cardBase + strings.ReplaceAll(line, mark, reset+p.cardBase) + p.reset
}

// The shadow: nothing on the top row (it's offset down), a cell of shadow on
// every row after, and a row of upper half-blocks beneath, offset right.
func (p palette) shadowGap() string {
	return " "
}

func (p palette) shadowCell() string {
	if p.shadow == "" {
		return " "
	}
	return p.shadowBg + " " + p.reset
}

func (p palette) shadowRow(box int) string {
	if p.shadow == "" {
		return ""
	}
	return " " + p.shadow + strings.Repeat("▀", box) + p.reset
}

// hjoin sets blocks of lines side by side, each padded to its width, with
// `gap` spaces between. Shorter blocks are padded with blank lines.
func hjoin(gap int, widths []int, blocks ...[]string) []string {
	height := 0
	for _, b := range blocks {
		height = max(height, len(b))
	}
	out := make([]string, height)
	for row := 0; row < height; row++ {
		var line strings.Builder
		for i, b := range blocks {
			if i > 0 {
				line.WriteString(strings.Repeat(" ", gap))
			}
			cell := ""
			if row < len(b) {
				cell = b[row]
			}
			line.WriteString(Pad(Truncate(cell, widths[i]), widths[i]))
		}
		out[row] = line.String()
	}
	return out
}

// split divides `width` into columns by weight, with `gap` between them,
// giving any remainder to the first.
func split(width, gap int, weights ...int) []int {
	total := 0
	for _, w := range weights {
		total += w
	}
	avail := width - gap*(len(weights)-1)
	out := make([]int, len(weights))
	used := 0
	for i, w := range weights {
		out[i] = avail * w / total
		used += out[i]
	}
	out[0] += avail - used
	return out
}

// padLines pads or cuts a block to exactly n lines.
func padLines(lines []string, n int) []string {
	if len(lines) > n {
		return lines[:max(0, n)]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}
