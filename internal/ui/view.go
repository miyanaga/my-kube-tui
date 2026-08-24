package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/miyanaga/my-kube-tui/internal/k8s"
)

func (m Model) View() string {
	if m.w == 0 || m.h == 0 {
		return "起動中…"
	}
	if m.overlay == overlayHelp {
		return m.renderCentered(helpBody(), "キー操作")
	}
	if m.overlay == overlayContexts {
		return m.renderCentered(m.contextsBody(), "接続先（kubeconfig のコンテキスト）")
	}

	var body string
	switch {
	case m.connErr != nil:
		body = m.renderConnError()
	case m.mode == ModeApp:
		body = m.renderApp()
	default:
		body = m.renderInfra()
	}
	return m.renderHeader() + "\n" + body + "\n" + m.renderFooter()
}

func (m Model) renderHeader() string {
	tabs := []string{" 1 アプリ ", " 2 インフラ "}
	sel := 0
	if m.mode == ModeInfra {
		sel = 1
	}
	var tb strings.Builder
	for i, t := range tabs {
		if i == sel {
			tb.WriteString(styTabOn.Render(t))
		} else {
			tb.WriteString(styHeader.Render(t))
		}
		tb.WriteString(styHeader.Render(" "))
	}

	left := styHeader.Render(" ctx ") + lipgloss.NewStyle().Background(lipgloss.Color("60")).Foreground(lipgloss.Color("231")).Bold(true).Render(" "+m.ctxCurrent+" ") + styHeader.Render(" (c) ")
	right := ""
	if !m.fetchedAt.IsZero() {
		right = k8s.ShortAge(m.fetchedAt) + "前に更新 "
	}
	if m.mode == ModeApp && m.nsSel != "" {
		right = "ns: " + m.nsSel + "   " + right
	}
	right = styHeader.Render(right)

	mid := tb.String()
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(mid) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return left + mid + styHeader.Render(strings.Repeat(" ", gap)) + right
}

func (m Model) renderFooter() string {
	if m.inputActive {
		return styAccent.Render("絞り込み ") + m.input.View() + styDim.Render("   Enter 確定 / Esc 解除")
	}
	if m.loadErr != nil {
		return truncateStyled(styBad.Render("取得に失敗: "+m.loadErr.Error()), m.w)
	}
	hint := "tab 移動  ↑↓ 選択  enter 決定  [ ] タブ  / 絞込  r 更新  c 接続先  ? ヘルプ  q 終了"
	if m.mode == ModeApp && m.tab == 1 && m.focus == paneDetail {
		hint = "f 追従  p 直前のログ  a 全Pod/1Pod  / 絞込  [ ] タブ  ? ヘルプ  q 終了"
	}
	if m.status != "" {
		hint = m.status + styDim.Render("   —   ") + hint
	}
	if f := m.filters[m.filterTargetForFocus()]; f != "" {
		hint = styAccent.Render("絞込: "+f) + "  " + hint
	}
	return truncateStyled(styDim.Render(hint), m.w)
}

func (m Model) renderConnError() string {
	msg := fmt.Sprintf("クラスタに接続できません。\n\n%v\n\nc キーで別のコンテキストを選べます。", m.connErr)
	return lipgloss.NewStyle().Width(m.w).Height(m.bodyH()).Padding(2, 4).Render(styBad.Render(msg))
}

// renderCentered は全画面のオーバーレイ（ヘルプ・接続先選択）。
func (m Model) renderCentered(body, title string) string {
	w := minInt(m.w-4, 76)
	box := panel(title, w, minInt(m.h-2, lipgloss.Height(body)+2), true, body)
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) contextsBody() string {
	if len(m.ctxNames) == 0 {
		return "kubeconfig にコンテキストがありません。"
	}
	var b strings.Builder
	for i, n := range m.ctxNames {
		line := "  " + n
		if n == m.ctxCurrent {
			line = "* " + n
		}
		if i == m.ovIdx {
			b.WriteString(stySelected.Render(pad(line, 70)))
		} else {
			b.WriteString(pad(line, 70))
		}
		b.WriteString("\n")
	}
	b.WriteString(styDim.Render("\n↑↓ 選択  Enter 接続  Esc 取消"))
	return b.String()
}

func helpBody() string {
	return strings.TrimSpace(`
このツールは参照専用です。クラスタへの変更操作は持ちません。

  全体
    1 / 2        アプリ視点 / インフラ視点の切り替え
    c            接続先（kubeconfig のコンテキスト）を切り替える
    tab / S-tab  ペインの移動        ↑↓ jk  選択
    [ ]  ← →     詳細タブの切り替え
    /            絞り込み（Esc で解除）
    enter        決定（名前空間→ワークロード→詳細）
    r            今すぐ更新（3 秒ごとに自動更新）
    ? / q        ヘルプ / 終了

  アプリ視点（何由来のどの Pod が動いているか）
    Pod    ワークロード配下の Pod と、動いていない理由
    ログ   配下の Pod のログを 1 本に集約して表示
             f 追従の on/off   p 直前に落ちたコンテナのログ
             a 全 Pod ⇔ 選択した 1 Pod
    イベント 起動する前に落ちている理由（イメージ・スケジュール失敗など）

  インフラ視点（ノードが健全か、抜けきったか）
    Pod    そのノードに載っている全 Pod（名前空間をまたぐ）
    概要   cordon / drain の残り、要求量と割当量、異常な条件
    イベント ノード自身に出ているイベント
`)
}

// bar は使用率の横棒。閾値で色を変える。
func bar(pct float64, w int) string {
	if pct < 0 {
		return styDim.Render(strings.Repeat("·", w))
	}
	filled := int(pct/100*float64(w) + 0.5)
	if filled > w {
		filled = w
	}
	if filled < 0 {
		filled = 0
	}
	col := colOK
	switch {
	case pct >= 90:
		col = colBad
	case pct >= 75:
		col = colWarn
	}
	return lipgloss.NewStyle().Foreground(col).Render(strings.Repeat("█", filled)) +
		styDim.Render(strings.Repeat("░", w-filled))
}

func pctText(pct float64) string {
	if pct < 0 {
		return "  - "
	}
	return fmt.Sprintf("%3.0f%%", pct)
}

// renderTabs は詳細ペインの上端に置くタブ行。
func renderTabs(labels []string, sel int, focused bool) string {
	var b strings.Builder
	for i, l := range labels {
		switch {
		case i == sel && focused:
			b.WriteString(styTabOn.Render(" " + l + " "))
		case i == sel:
			b.WriteString(lipgloss.NewStyle().Bold(true).Underline(true).Render(" " + l + " "))
		default:
			b.WriteString(styTabOff.Render(" " + l + " "))
		}
		b.WriteString(" ")
	}
	return b.String()
}

// rows は選択位置つきの行リストを 1 つの文字列にする。
func rows(lines []string, p pane, h int, focused bool) string {
	if len(lines) == 0 {
		return styDim.Render("（該当なし）")
	}
	end := minInt(len(lines), p.off+h)
	var b strings.Builder
	for i := p.off; i < end; i++ {
		if i == p.idx && focused {
			b.WriteString(stySelected.Render(stripANSI(lines[i])))
		} else if i == p.idx {
			b.WriteString(lipgloss.NewStyle().Bold(true).Render(lines[i]))
		} else {
			b.WriteString(lines[i])
		}
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// stripANSI は選択行の反転表示のために既存の色を落とす。
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
