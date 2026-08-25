package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/miyanaga/my-kube-tui/internal/k8s"
	"k8s.io/apimachinery/pkg/api/resource"
)

// renderInfra はインフラ視点。ノード一覧 → （Pod / 概要 / イベント）。
func (m Model) renderInfra() string {
	w := m.w
	top := panel(m.nodeTitle(), w, m.nodeH(), m.focus == paneNode, m.nodeContent(w-2))
	det := panel(m.nodeDetTitle(), w, m.infraDetH(), m.focus == paneNDetail, m.infraDetContent(w-2))
	return lipgloss.JoinVertical(lipgloss.Left, top, det)
}

func (m Model) nodeTitle() string {
	ready, cordoned, bad := 0, 0, 0
	for _, n := range m.nodes {
		if n.Ready {
			ready++
		} else {
			bad++
		}
		if n.Unschedulable {
			cordoned++
		}
	}
	t := fmt.Sprintf("ノード  %d 台（Ready %d", len(m.nodes), ready)
	if bad > 0 {
		t += fmt.Sprintf(" / NotReady %d", bad)
	}
	if cordoned > 0 {
		t += fmt.Sprintf(" / cordon %d", cordoned)
	}
	return t + "）"
}

func (m Model) nodeContent(w int) string {
	items := m.visibleNodes()

	nameW := maxInt(14, minInt(26, w/5))
	stateW := 24
	podsW := 11
	resW := 15 // バー 8 ＋ 数値
	verW := maxInt(0, w-2-nameW-stateW-podsW-resW*2-2)

	head := styColHead.Render("  " + pad("ノード", nameW) + pad("状態", stateW) + padLeft("Pod", podsW) + "  " +
		pad("CPU 要求", resW) + pad("メモリ要求", resW) + pad("バージョン", maxInt(1, verW)))

	lines := make([]string, len(items))
	for i, n := range items {
		state := nodeStateText(n)
		podsTxt := fmt.Sprintf("%d", n.ActivePods)
		if n.AllocPod > 0 {
			podsTxt = fmt.Sprintf("%d/%d", n.ActivePods, n.AllocPod)
		}
		if n.ProblemPods > 0 {
			podsTxt += fmt.Sprintf(" !%d", n.ProblemPods)
		}
		cpuPct := k8s.Pct(n.ReqCPU, n.AllocCPU)
		memPct := k8s.Pct(n.ReqMem, n.AllocMem)

		lines[i] = healthMark(n.Health) + " " +
			pad(n.Name, nameW) +
			lipgloss.NewStyle().Foreground(healthColor(n.Health)).Render(pad(state, stateW)) +
			padLeft(podsTxt, podsW) + "  " +
			padStyled(bar(cpuPct, 8)+" "+pctText(cpuPct), resW) +
			padStyled(bar(memPct, 8)+" "+pctText(memPct), resW) +
			styDim.Render(pad(n.Version, maxInt(1, verW)))
	}
	return head + "\n" + rows(lines, m.pNode, m.nodeRows(), m.focus == paneNode)
}

func nodeStateText(n *k8s.Node) string {
	parts := []string{}
	if n.Ready {
		parts = append(parts, "Ready")
	} else {
		s := "NotReady"
		if n.ReadyReason != "" {
			s = "NotReady:" + n.ReadyReason
		}
		parts = append(parts, s)
	}
	if n.Unschedulable {
		if n.EvictablePods > 0 {
			parts = append(parts, fmt.Sprintf("cordon(残%d)", n.EvictablePods))
		} else {
			parts = append(parts, "cordon(空)")
		}
	}
	if len(n.Problems) > 0 {
		parts = append(parts, strings.Join(n.Problems, ","))
	}
	return strings.Join(parts, " ")
}

func (m Model) nodeDetTitle() string {
	n := m.currentNode()
	if n == nil {
		return "詳細"
	}
	return "ノード / " + n.Name
}

func (m Model) infraDetContent(w int) string {
	tabs := renderTabs(m.detailTabLabels(), m.tab, m.focus == paneNDetail)
	switch m.tab {
	case 0:
		return tabs + "\n" + m.podTableContent(w, true)
	case 1:
		return tabs + "\n" + m.nodeSummary(w)
	default:
		return tabs + "\n" + m.eventContent(w)
	}
}

// nodeSummary は「本当に抜けたか」「詰め込めているか」を 1 画面で見せる。
func (m Model) nodeSummary(w int) string {
	n := m.currentNode()
	if n == nil {
		return styDim.Render("ノードを選んでください。")
	}
	var b strings.Builder
	line := func(label, value string) {
		b.WriteString(styDim.Render(pad(label, 14)) + value + "\n")
	}

	roles := strings.Join(n.Roles, ",")
	if roles == "" {
		roles = "-"
	}
	line("ノード", fmt.Sprintf("%s   %s   %s   稼働 %s   role: %s",
		n.Name, n.InternalIP, n.Version, k8s.ShortAge(n.Created), roles))
	line("状態", lipgloss.NewStyle().Foreground(healthColor(n.Health)).Render(nodeStateText(n)))

	if n.Unschedulable {
		done, text := n.DrainStatus()
		st := styWarn
		if done {
			st = styOK
		}
		line("drain", st.Render(text))
	} else {
		line("drain", styDim.Render("cordon されていません（スケジュール可）"))
	}

	podCap := fmt.Sprintf("%d", n.ActivePods)
	if n.AllocPod > 0 {
		podCap = fmt.Sprintf("%d / %d", n.ActivePods, n.AllocPod)
	}
	line("Pod", fmt.Sprintf("%s   内訳: DaemonSet %d / static %d / 退避対象 %d   異常 %s",
		podCap, n.DaemonSetPods, n.StaticPods, n.EvictablePods, warnNum(n.ProblemPods)))

	cpuPct := k8s.Pct(n.ReqCPU, n.AllocCPU)
	memPct := k8s.Pct(n.ReqMem, n.AllocMem)
	line("CPU 要求", fmt.Sprintf("%s %s   %s / %s   上限合計 %s",
		bar(cpuPct, 16), pctText(cpuPct), fmtCPU(n.ReqCPU), fmtCPU(n.AllocCPU), fmtCPU(n.LimCPU)))
	line("メモリ要求", fmt.Sprintf("%s %s   %s / %s   上限合計 %s",
		bar(memPct, 16), pctText(memPct), fmtMem(n.ReqMem), fmtMem(n.AllocMem), fmtMem(n.LimMem)))

	if n.UsedCPU != nil && n.UsedMem != nil {
		uc := k8s.Pct(*n.UsedCPU, n.AllocCPU)
		um := k8s.Pct(*n.UsedMem, n.AllocMem)
		line("実測 CPU", fmt.Sprintf("%s %s   %s", bar(uc, 16), pctText(uc), fmtCPU(*n.UsedCPU)))
		line("実測 メモリ", fmt.Sprintf("%s %s   %s", bar(um, 16), pctText(um), fmtMem(*n.UsedMem)))
	} else {
		line("実測", styDim.Render("metrics-server がないため要求量のみ表示しています"))
	}

	if len(n.Problems) > 0 {
		line("異常な条件", styWarn.Render(strings.Join(n.Problems, ", ")))
	}

	// このノードに載っているものの内訳。何がこの台を占めているかを掴む。
	byNS := map[string]int{}
	for _, p := range n.Pods {
		if !p.Terminal() {
			byNS[p.Namespace]++
		}
	}
	if len(byNS) > 0 {
		var parts []string
		for ns, c := range byNS {
			parts = append(parts, fmt.Sprintf("%s:%d", ns, c))
		}
		sortStrings(parts)
		line("名前空間別", strings.Join(parts, "  "))
	}
	return b.String()
}

func warnNum(n int) string {
	if n == 0 {
		return "0"
	}
	return styWarn.Render(fmt.Sprintf("%d", n))
}

func fmtCPU(q resource.Quantity) string {
	m := q.MilliValue()
	if m == 0 {
		return "0"
	}
	if m%1000 == 0 {
		return fmt.Sprintf("%d", m/1000)
	}
	if m < 10000 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%.1f", float64(m)/1000)
}

func fmtMem(q resource.Quantity) string {
	v := q.Value()
	switch {
	case v == 0:
		return "0"
	case v >= 1<<30:
		return fmt.Sprintf("%.1fGi", float64(v)/float64(1<<30))
	case v >= 1<<20:
		return fmt.Sprintf("%.0fMi", float64(v)/float64(1<<20))
	default:
		return fmt.Sprintf("%dKi", v/1024)
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
