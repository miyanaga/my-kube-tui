package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/miyanaga/my-kube-tui/internal/k8s"
)

// renderApp はアプリ視点。名前空間 → ワークロード → （Pod / ログ / イベント）。
func (m Model) renderApp() string {
	leftW, rightW := m.leftW(), m.rightW()

	left := panel("名前空間", leftW, m.bodyH(), m.focus == paneNS, m.nsContent(leftW-2))
	wl := panel(m.wlTitle(), rightW, m.wlH(), m.focus == paneWL, m.wlContent(rightW-2))
	det := panel(m.detTitle(), rightW, m.appDetH(), m.focus == paneDetail, m.appDetContent(rightW-2))

	return lipgloss.JoinHorizontal(lipgloss.Top, left, lipgloss.JoinVertical(lipgloss.Left, wl, det))
}

func (m Model) nsContent(w int) string {
	items := m.visibleNamespaces()
	lines := make([]string, len(items))
	for i, n := range items {
		mark := " "
		if n == m.nsSel {
			mark = styAccent.Render("▸")
		}
		lines[i] = mark + " " + pad(n, maxInt(1, w-2))
	}
	return rows(lines, m.pNS, m.nsRows(), m.focus == paneNS)
}

func (m Model) wlTitle() string {
	ws := m.workloads
	bad, warn := 0, 0
	for _, w := range ws {
		switch w.Health {
		case k8s.HealthBad:
			bad++
		case k8s.HealthWarn, k8s.HealthProgressing:
			warn++
		}
	}
	t := fmt.Sprintf("ワークロード  %d 件", len(ws))
	if bad > 0 || warn > 0 {
		t += fmt.Sprintf("（異常 %d / 要注意 %d）", bad, warn)
	}
	return t
}

func (m Model) wlContent(w int) string {
	items := m.visibleWorkloads()

	kindW, readyW, ageW := 12, 7, 5
	nameW := maxInt(14, minInt(44, w-2-kindW-readyW-ageW-20))
	noteW := maxInt(6, w-2-kindW-nameW-readyW-ageW-4)

	head := styColHead.Render("  " + pad("種別", kindW) + " " + pad("名前", nameW) + " " +
		padLeft("Ready", readyW) + " " + padLeft("経過", ageW) + " " + pad("イメージ / 補足", noteW))

	lines := make([]string, len(items))
	for i, wl := range items {
		note := wl.Note
		if note == "" && len(wl.Images) > 0 {
			var short []string
			for _, img := range wl.Images {
				short = append(short, k8s.ShortImage(img))
			}
			note = strings.Join(short, ",")
		}
		ready := fmt.Sprintf("%d/%d", wl.Ready, wl.Desired)
		if wl.Ref.Kind == "CronJob" {
			ready = fmt.Sprintf("%d 実行中", wl.Desired)
		}
		lines[i] = healthMark(wl.Health) + " " +
			styDim.Render(pad(wl.Ref.Kind, kindW)) + " " +
			pad(wl.Ref.Name, nameW) + " " +
			readyStyle(wl).Render(padLeft(ready, readyW)) + " " +
			styDim.Render(padLeft(k8s.ShortAge(wl.Created), ageW)+" "+pad(note, noteW))
	}
	return head + "\n" + rows(lines, m.pWL, m.wlRows(), m.focus == paneWL)
}

func readyStyle(w *k8s.Workload) lipgloss.Style {
	if w.Desired > 0 && w.Ready < w.Desired {
		return styWarn
	}
	return lipgloss.NewStyle()
}

func (m Model) detTitle() string {
	w := m.currentWorkload()
	if w == nil {
		return "詳細"
	}
	return w.Ref.Kind + " / " + w.Ref.Name
}

func (m Model) appDetContent(w int) string {
	labels := []string{"Pod", "ログ", "イベント"}
	if m.tab == 1 {
		labels[1] = "ログ" + m.logBadge()
	}
	tabs := renderTabs(labels, m.tab, m.focus == paneDetail)
	switch m.tab {
	case 0:
		return tabs + "\n" + m.podTableContent(w, false)
	case 1:
		return tabs + "\n" + m.logContent(w)
	default:
		return tabs + "\n" + m.eventContent(w)
	}
}

func (m Model) logBadge() string {
	parts := []string{}
	if m.logScope != "" {
		parts = append(parts, "1Pod")
	}
	if m.logPrev {
		parts = append(parts, "直前")
	}
	if !m.logFollow {
		parts = append(parts, "停止")
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, "/") + ")"
}

// podTableContent は Pod 一覧。App では由来が自明なので省き、Infra では名前空間を出す。
func (m Model) podTableContent(w int, withNamespace bool) string {
	pods := m.visibleDetailPods()
	p := m.pDet
	focused := m.focus == paneDetail
	if m.mode == ModeInfra {
		p, focused = m.pNDet, m.focus == paneNDetail
	}

	statusW, readyW, restartW, ageW := 18, 5, 6, 5
	tailW := 0
	if withNamespace {
		tailW = maxInt(12, minInt(28, w/4)) // 由来
	} else {
		tailW = maxInt(10, minInt(20, w/5)) // ノード
	}
	nsW := 0
	if withNamespace {
		nsW = maxInt(10, minInt(22, w/6))
	}
	// 区切りの空白（列間 5 つ ＋ 先頭のマーク 2）を差し引いた残りが名前。
	// 名前だけが際限なく広がると右端の「由来」が切れるので、余りは右端に回す。
	fixed := 2 + nsW + statusW + readyW + restartW + ageW + tailW + 5
	nameW := maxInt(12, w-fixed)
	if extra := nameW - 48; extra > 0 {
		nameW, tailW = 48, tailW+extra
	}

	headTail := "ノード"
	if withNamespace {
		headTail = "由来"
	}
	head := "  "
	if withNamespace {
		head += pad("名前空間", nsW) + " "
	}
	head += pad("Pod", nameW) + " " + pad("状態", statusW) + " " +
		padLeft("Ready", readyW) + " " + padLeft("再起動", restartW) + " " + padLeft("経過", ageW) + " " + pad(headTail, tailW)
	head = styColHead.Render(head)

	lines := make([]string, len(pods))
	for i, pd := range pods {
		tail := pd.Node
		if withNamespace {
			tail = pd.OwnedBy.Kind + "/" + pd.OwnedBy.Name
			if pd.OwnedBy.Empty() {
				tail = "(単体 Pod)"
			}
		}
		restarts := fmt.Sprintf("%d", pd.Restarts)
		rst := lipgloss.NewStyle()
		if pd.Restarts > 0 {
			rst = styWarn
		}
		line := healthMark(pd.Health) + " "
		if withNamespace {
			line += styDim.Render(pad(pd.Namespace, nsW)) + " "
		}
		line += padName(pd.Name, nameW) + " " +
			lipgloss.NewStyle().Foreground(healthColor(pd.Health)).Render(pad(pd.Reason, statusW)) + " " +
			padLeft(fmt.Sprintf("%d/%d", pd.ReadyCount, pd.TotalCount), readyW) + " " +
			rst.Render(padLeft(restarts, restartW)) + " " +
			styDim.Render(padLeft(k8s.ShortAge(pd.Created), ageW)+" "+pad(tail, tailW))
		lines[i] = line
	}
	body := rows(lines, p, m.detRows(), focused)

	// 選択中の Pod の理由を最終行に出す（イベントを見に行かずに済むように）。
	// 行数を一定に保ちたいので、メッセージが無くても 1 行分は確保する。
	msg := ""
	if len(pods) > 0 && p.idx < len(pods) && pods[p.idx].Message != "" {
		msg = truncateStyled(styWarn.Render("↳ "+strings.ReplaceAll(pods[p.idx].Message, "\n", " ")), w)
	}
	return head + "\n" + body + "\n" + msg
}

func (m Model) logContent(w int) string {
	lines := m.visibleLogLines()
	h := m.detRows()
	if len(lines) == 0 {
		if m.logKey == "" {
			return styDim.Render("ログの対象がありません。")
		}
		return styDim.Render("ログを待っています…")
	}
	end := len(lines) - m.logOff
	if end > len(lines) {
		end = len(lines)
	}
	if end < 0 {
		end = 0
	}
	start := maxInt(0, end-h)

	// 同じワークロードの Pod 名は前半が全部同じなので、共通部分を落として
	// 見分けのつく後半だけを出す。ここを削らないと肝心の識別子が切り捨てられる。
	multi := m.logScope == ""
	prefix, nameW := "", 0
	if multi {
		prefix = commonPodPrefix(lines)
		for _, l := range lines[start:end] {
			nameW = maxInt(nameW, lipgloss.Width(trimPodPrefix(l.Pod, prefix)))
		}
		nameW = minInt(maxInt(nameW, 6), 22)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		l := lines[i]
		ts := "        "
		if !l.Time.IsZero() {
			ts = l.Time.Local().Format("15:04:05")
		}
		head := styDim.Render(ts) + " "
		if multi {
			head += lipgloss.NewStyle().Foreground(podColor(l.Pod)).Render(pad(trimPodPrefix(l.Pod, prefix), nameW)) + styDim.Render("▏")
		}
		text := l.Text
		if l.Notice {
			text = styWarn.Render("[" + l.Pod + "] " + text)
		}
		b.WriteString(truncateStyled(head+text, w))
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// commonPodPrefix は流れている Pod 名に共通する接頭辞を「-」区切りで求める。
func commonPodPrefix(lines []k8s.LogLine) string {
	seen := map[string]bool{}
	var names []string
	for _, l := range lines {
		if l.Pod != "" && !seen[l.Pod] {
			seen[l.Pod] = true
			names = append(names, l.Pod)
		}
	}
	if len(names) < 2 {
		return ""
	}
	parts := strings.Split(names[0], "-")
	for _, n := range names[1:] {
		o := strings.Split(n, "-")
		i := 0
		for i < len(parts) && i < len(o) && parts[i] == o[i] {
			i++
		}
		parts = parts[:i]
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "-") + "-"
}

func trimPodPrefix(name, prefix string) string {
	if prefix == "" || name == prefix {
		return name
	}
	if s := strings.TrimPrefix(name, prefix); s != "" {
		return s
	}
	return name
}

func (m Model) eventContent(w int) string {
	evs := m.visibleEvents()
	p := m.pDet
	focused := m.focus == paneDetail
	if m.mode == ModeInfra {
		p, focused = m.pNDet, m.focus == paneNDetail
	}

	ageW, reasonW := 6, 20
	objW := maxInt(14, minInt(30, w/4))
	msgW := maxInt(10, w-2-ageW-reasonW-objW-3)

	head := styColHead.Render("  " + padLeft("経過", ageW) + " " + pad("理由", reasonW) + pad("対象", objW) + pad("内容", msgW))

	lines := make([]string, len(evs))
	for i, e := range evs {
		st := styDim
		mark := styDim.Render("·")
		if e.Type == "Warning" {
			st = styWarn
			mark = styWarn.Render("▲")
		}
		count := ""
		if e.Count > 1 {
			count = fmt.Sprintf(" x%d", e.Count)
		}
		lines[i] = mark + " " + styDim.Render(padLeft(k8s.ShortAge(e.Time), ageW)) + " " +
			st.Render(pad(e.Reason+count, reasonW)) +
			styDim.Render(pad(e.Object, objW)) +
			pad(strings.ReplaceAll(e.Message, "\n", " "), msgW)
	}
	if len(lines) == 0 {
		return head + "\n" + styDim.Render("イベントはありません（直近 1 時間ほどで消えます）")
	}
	return head + "\n" + rows(lines, p, m.detRows(), focused)
}
