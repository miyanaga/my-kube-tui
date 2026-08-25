package ui

import (
	"math"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// マウスの当たり判定は、描画と同じ layout.go の値だけから逆算する。
// 描画時に座標を控えておく方式にすると、描いていない状態（初回・リサイズ直後）で
// ずれるので、位置は常に計算で求める。

type hitKind int

const (
	hitNone hitKind = iota
	hitCtx
	hitModeApp
	hitModeInfra
	hitNS
	hitWL
	hitNode
	hitDetailTab
	hitDetailRow
	hitOverlayRow
)

type hit struct {
	kind hitKind
	// hitDetailTab はタブ番号、行のときは一覧上のインデックス
	// （hitDetailRow だけは画面内の相対行）。
	n int
}

const wheelStep = 3

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.wheel(msg.X, msg.Y, -wheelStep)
	case tea.MouseButtonWheelDown:
		return m.wheel(msg.X, msg.Y, wheelStep)
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	return m.click(msg.X, msg.Y)
}

func (m Model) click(x, y int) (tea.Model, tea.Cmd) {
	h := m.hitTest(x, y)
	switch h.kind {
	case hitOverlayRow:
		m.ovIdx = h.n
		return m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})

	case hitCtx:
		m.overlay = overlayContexts
		m.ovIdx = maxInt(0, indexOf(m.ctxNames, m.ctxCurrent))
		return m, nil

	case hitModeApp:
		if m.mode == ModeApp {
			return m, nil
		}
		m.mode, m.focus, m.tab = ModeApp, paneNS, 0
		m.clampAll()
		cmd := tea.Batch(m.refreshCmd(), m.syncLogStream())
		return m, cmd

	case hitModeInfra:
		if m.mode == ModeInfra {
			return m, nil
		}
		m.mode, m.focus, m.tab = ModeInfra, paneNode, 0
		m.stopLogs()
		m.clampAll()
		return m, m.refreshCmd()

	case hitNS:
		m.focus = paneNS
		if h.n >= len(m.visibleNamespaces()) || h.n == m.pNS.idx {
			return m, nil
		}
		m.pNS.idx = h.n
		cmd := m.onSelectionChanged()
		return m, cmd

	case hitWL:
		m.focus = paneWL
		if h.n >= len(m.visibleWorkloads()) || h.n == m.pWL.idx {
			return m, nil
		}
		m.pWL.idx = h.n
		cmd := m.onSelectionChanged()
		return m, cmd

	case hitNode:
		m.focus = paneNode
		if h.n >= len(m.visibleNodes()) || h.n == m.pNode.idx {
			return m, nil
		}
		m.pNode.idx = h.n
		cmd := m.onSelectionChanged()
		return m, cmd

	case hitDetailTab:
		m.focus = m.detailPane()
		cmd := m.setTab(h.n)
		return m, cmd

	case hitDetailRow:
		m.focus = m.detailPane()
		p := m.detailPaneState()
		n, _, _ := m.focusedList()
		if idx := p.off + h.n; idx < n {
			p.idx = idx
		}
		return m, nil
	}
	return m, nil
}

func (m Model) wheel(x, y, d int) (tea.Model, tea.Cmd) {
	if m.overlay != overlayNone {
		return m, nil
	}
	switch m.paneAt(x, y) {
	case paneNS:
		m.focus = paneNS
		m.pNS.scroll(d, len(m.visibleNamespaces()), m.nsRows())
		cmd := m.onSelectionChanged()
		return m, cmd
	case paneWL:
		m.focus = paneWL
		m.pWL.scroll(d, len(m.visibleWorkloads()), m.wlRows())
		cmd := m.onSelectionChanged()
		return m, cmd
	case paneNode:
		m.focus = paneNode
		m.pNode.scroll(d, len(m.visibleNodes()), m.nodeRows())
		cmd := m.onSelectionChanged()
		return m, cmd
	case paneDetail, paneNDetail:
		m.focus = m.detailPane()
		// ログはテキストなので、選択ではなく表示位置を動かす。
		if m.mode == ModeApp && m.tab == 1 {
			n, h := len(m.visibleLogLines()), m.detRows()
			m.logOff = maxInt(0, minInt(maxInt(0, n-h), m.logOff-d))
			m.logFollow = m.logOff == 0
			cmd := m.maybeLoadMoreLogs()
			return m, cmd
		}
		n, h, _ := m.focusedList()
		m.detailPaneState().scroll(d, n, h)
		return m, nil
	}
	return m, nil
}

func (m *Model) detailPane() paneID {
	if m.mode == ModeInfra {
		return paneNDetail
	}
	return paneDetail
}

func (m *Model) detailPaneState() *pane {
	if m.mode == ModeInfra {
		return &m.pNDet
	}
	return &m.pDet
}

// paneAt はホイール用に、その座標がどのペインの上かだけを返す。
func (m *Model) paneAt(x, y int) paneID {
	if y == 0 || y >= 1+m.bodyH() || m.connErr != nil {
		return ""
	}
	if m.mode == ModeApp {
		if x < m.leftW() {
			return paneNS
		}
		if y < 1+m.wlH() {
			return paneWL
		}
		return paneDetail
	}
	if y < 1+m.nodeH() {
		return paneNode
	}
	return paneNDetail
}

func (m *Model) hitTest(x, y int) hit {
	if m.overlay == overlayContexts {
		return m.hitContextOverlay(x, y)
	}
	if m.overlay != overlayNone {
		return hit{}
	}
	if y == 0 {
		return m.hitHeader(x)
	}
	if m.connErr != nil {
		return hit{}
	}

	if m.mode == ModeApp {
		if x < m.leftW() {
			if r := y - 2; r >= 0 && r < m.nsRows() {
				return hit{hitNS, m.pNS.off + r}
			}
			return hit{}
		}
		if y < 1+m.wlH() {
			// 枠 1 行 ＋ 列見出し 1 行のあとが本体。
			if r := y - 3; r >= 0 && r < m.wlRows() {
				return hit{hitWL, m.pWL.off + r}
			}
			return hit{}
		}
		return m.hitDetail(x, y, 1+m.wlH(), m.leftW())
	}

	if y < 1+m.nodeH() {
		if r := y - 3; r >= 0 && r < m.nodeRows() {
			return hit{hitNode, m.pNode.off + r}
		}
		return hit{}
	}
	return m.hitDetail(x, y, 1+m.nodeH(), 0)
}

func (m *Model) hitHeader(x int) hit {
	cx := runewidth.StringWidth(" ctx ") +
		runewidth.StringWidth(" "+m.ctxCurrent+" ") +
		runewidth.StringWidth(" (c) ")
	if x < cx {
		return hit{kind: hitCtx}
	}
	for i, t := range headerTabs {
		w := runewidth.StringWidth(t)
		if x >= cx && x < cx+w {
			if i == 0 {
				return hit{kind: hitModeApp}
			}
			return hit{kind: hitModeInfra}
		}
		cx += w + 1 // タブ間の空白
	}
	return hit{}
}

func (m *Model) hitDetail(x, y, dTop, paneX int) hit {
	tabY := dTop + 1
	if y == tabY {
		cx := paneX + 1
		for i, l := range m.detailTabLabels() {
			w := runewidth.StringWidth(" " + l + " ")
			if x >= cx && x < cx+w {
				return hit{hitDetailTab, i}
			}
			cx += w + 1
		}
		return hit{}
	}
	rowTop := tabY + 1
	if m.tab != 1 {
		rowTop++ // 列見出し
	}
	if r := y - rowTop; r >= 0 && r < m.detRows() {
		return hit{hitDetailRow, r}
	}
	return hit{}
}

// hitContextOverlay は中央寄せしたダイアログの行を割り出す。
// lipgloss.Place と同じ丸めをして位置を合わせる。
func (m *Model) hitContextOverlay(x, y int) hit {
	body := m.contextsBody()
	boxW := minInt(m.w-4, 76)
	boxH := minInt(m.h-2, lipgloss.Height(body)+2)
	x0 := placeOffset(m.w, boxW)
	y0 := placeOffset(m.h, boxH)
	if x < x0 || x >= x0+boxW {
		return hit{}
	}
	r := y - (y0 + 1)
	if r >= 0 && r < len(m.ctxNames) {
		return hit{hitOverlayRow, r}
	}
	return hit{}
}

func placeOffset(outer, inner int) int {
	gap := outer - inner
	if gap <= 0 {
		return 0
	}
	return int(math.Round(float64(gap) * 0.5))
}
