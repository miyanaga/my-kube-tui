package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/miyanaga/my-kube-tui/internal/k8s"
)

var (
	colOK     = lipgloss.Color("42")
	colProg   = lipgloss.Color("39")
	colWarn   = lipgloss.Color("214")
	colBad    = lipgloss.Color("203")
	colDone   = lipgloss.Color("245")
	colDim    = lipgloss.Color("244")
	colHeadBg = lipgloss.Color("236")
	colSelBg  = lipgloss.Color("24")
	colSelFg  = lipgloss.Color("231")
	colAccent = lipgloss.Color("39")

	styDim      = lipgloss.NewStyle().Foreground(colDim)
	styHeader   = lipgloss.NewStyle().Background(colHeadBg).Foreground(lipgloss.Color("252"))
	styTabOn    = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("232")).Bold(true)
	styTabOff   = lipgloss.NewStyle().Foreground(colDim)
	stySelected = lipgloss.NewStyle().Background(colSelBg).Foreground(colSelFg)
	styColHead  = lipgloss.NewStyle().Foreground(colDim).Underline(true)
	styBad      = lipgloss.NewStyle().Foreground(colBad)
	styWarn     = lipgloss.NewStyle().Foreground(colWarn)
	styOK       = lipgloss.NewStyle().Foreground(colOK)
	styAccent   = lipgloss.NewStyle().Foreground(colAccent)
)

func healthColor(h k8s.Health) lipgloss.Color {
	switch h {
	case k8s.HealthOK:
		return colOK
	case k8s.HealthProgressing:
		return colProg
	case k8s.HealthWarn:
		return colWarn
	case k8s.HealthBad:
		return colBad
	}
	return colDone
}

func healthMark(h k8s.Health) string {
	return lipgloss.NewStyle().Foreground(healthColor(h)).Render(h.Symbol())
}

// ログの Pod 名を色分けするための固定パレット。行の出所を目で追えるようにする。
var podPalette = []lipgloss.Color{"79", "215", "141", "115", "209", "111", "180", "84", "175", "117"}

func podColor(name string) lipgloss.Color {
	var sum int
	for _, r := range name {
		sum = sum*31 + int(r)
	}
	if sum < 0 {
		sum = -sum
	}
	return podPalette[sum%len(podPalette)]
}

// panel は角丸の枠と、上辺に埋め込んだ見出しを描く。
func panel(title string, w, h int, focused bool, content string) string {
	if w < 4 {
		w = 4
	}
	if h < 3 {
		h = 3
	}
	border := lipgloss.RoundedBorder()
	bc := colDim
	if focused {
		bc = colAccent
	}
	box := lipgloss.NewStyle().
		Border(border).
		BorderForeground(bc).
		Width(w - 2).
		Height(h - 2).
		MaxHeight(h).
		Render(clipLines(content, w-2, h-2))

	lines := strings.Split(box, "\n")
	if len(lines) > 0 && title != "" {
		lines[0] = overlayTitle(lines[0], " "+title+" ", bc, focused)
	}
	return strings.Join(lines, "\n")
}

// overlayTitle は枠の上辺に見出しを差し込む（枠線を消さずに置き換える）。
func overlayTitle(top, title string, bc lipgloss.Color, focused bool) string {
	plain := lipgloss.NewStyle().Foreground(bc)
	st := plain
	if focused {
		st = lipgloss.NewStyle().Foreground(bc).Bold(true)
	}
	// top は装飾済みなので、幅だけを頼りに描き直す。
	w := lipgloss.Width(top)
	tw := runewidth.StringWidth(title)
	if tw > w-4 {
		title = runewidth.Truncate(title, maxInt(0, w-4), "…")
		tw = runewidth.StringWidth(title)
	}
	left := plain.Render(string([]rune(lipgloss.RoundedBorder().TopLeft)) + strings.Repeat(lipgloss.RoundedBorder().Top, 1))
	mid := st.Render(title)
	rest := w - 2 - 1 - tw
	if rest < 0 {
		rest = 0
	}
	right := plain.Render(strings.Repeat(lipgloss.RoundedBorder().Top, rest) + lipgloss.RoundedBorder().TopRight)
	return left + mid + right
}

// clipLines は content を w×h に収める（横は切り詰め、縦は先頭 h 行）。
func clipLines(content string, w, h int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			lines[i] = truncateStyled(l, w)
		}
	}
	return strings.Join(lines, "\n")
}

// truncateStyled は ANSI 装飾を保ったまま表示幅で切る。
func truncateStyled(s string, w int) string {
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	width := 0
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		if inEsc {
			b.WriteRune(r)
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		rw := runewidth.RuneWidth(r)
		if width+rw > w {
			break
		}
		width += rw
		b.WriteRune(r)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

func pad(s string, w int) string {
	return runewidth.FillRight(runewidth.Truncate(s, w, "…"), w)
}

// padName は Pod 名のように「末尾だけが違う」名前を幅に収める。
// 右から切ると識別子が消えてしまうので、真ん中を省略する。
func padName(s string, w int) string {
	if runewidth.StringWidth(s) <= w || w < 10 {
		return pad(s, w)
	}
	tail := 6
	head := w - tail - 1
	r := []rune(s)
	return runewidth.FillRight(runewidth.Truncate(string(r), head, "")+"…"+string(r[len(r)-tail:]), w)
}

// padStyled は ANSI 装飾を含む文字列を表示幅で右詰めする。
func padStyled(s string, w int) string {
	d := w - lipgloss.Width(s)
	if d <= 0 {
		return truncateStyled(s, w)
	}
	return s + strings.Repeat(" ", d)
}

func padLeft(s string, w int) string {
	return runewidth.FillLeft(runewidth.Truncate(s, w, "…"), w)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
