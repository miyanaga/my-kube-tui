package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/miyanaga/my-kube-tui/internal/k8s"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.clampAll()
		return m, nil

	case tickMsg:
		m.spinner++
		return m, tea.Batch(m.refreshCmd(), tickCmd())

	case namespacesMsg:
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.loadErr = nil
		m.namespaces = msg.items
		if m.nsSel == "" && len(msg.items) > 0 {
			m.nsSel = pickDefaultNamespace(msg.items)
			m.pNS.idx = indexOf(m.visibleNamespaces(), m.nsSel)
			m.clampAll()
			return m, m.fetchWorkloads()
		}
		m.clampAll()
		return m, nil

	case workloadsMsg:
		if msg.ns != m.nsSel {
			return m, nil // 切り替え中に届いた古い応答
		}
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.loadErr = nil
		m.workloads = msg.items
		m.fetchedAt = time.Now()
		m.clampAll()
		cmd := m.syncLogStream()
		return m, cmd

	case nodesMsg:
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.loadErr = nil
		m.nodes = msg.items
		m.fetchedAt = time.Now()
		m.clampAll()
		return m, nil

	case eventsMsg:
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.events, m.eventsKey = msg.items, msg.key
		m.clampAll()
		return m, nil

	case clientMsg:
		if msg.err != nil {
			m.connErr = msg.err
			return m, nil
		}
		m.stopLogs()
		m.client = msg.client
		m.ctxCurrent = msg.client.ContextName
		m.connErr, m.loadErr = nil, nil
		m.namespaces, m.workloads, m.nodes, m.events = nil, nil, nil, nil
		m.nsSel, m.eventsKey = "", ""
		m.pNS, m.pWL, m.pDet, m.pNode, m.pNDet = pane{}, pane{}, pane{}, pane{}, pane{}
		m.status = "コンテキストを " + msg.client.ContextName + " に切り替えました"
		return m, m.refreshCmd()

	case logBatchMsg:
		if msg.gen != m.logGen {
			return m, nil
		}
		m.appendLogLines(msg.lines)
		return m, waitLogs(m.logStream, m.logGen)

	case logClosedMsg:
		return m, nil

	case logHistoryMsg:
		if msg.gen != m.logGen {
			return m, nil
		}
		m.logLoading = false
		if added := m.mergeLogHistory(msg.lines); added == 0 {
			m.logExhausted = true
			m.status = "これ以上さかのぼれません（Pod のログはここまで）"
		} else {
			m.status = k8s.FormatInt(int64(added)) + " 行さかのぼりました"
		}
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 入力中はフィルタ編集が最優先。
	if m.inputActive {
		switch msg.String() {
		case "esc":
			m.inputActive = false
			m.filters[m.inputTarget] = ""
			m.input.SetValue("")
			m.clampAll()
			return m, nil
		case "enter":
			m.inputActive = false
			m.clampAll()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.filters[m.inputTarget] = m.input.Value()
		m.clampAll()
		return m, cmd
	}

	if m.overlay != overlayNone {
		return m.handleOverlayKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		m.stopLogs()
		return m, tea.Quit
	case "?":
		m.overlay = overlayHelp
		return m, nil
	case "c":
		m.overlay = overlayContexts
		m.ovIdx = maxInt(0, indexOf(m.ctxNames, m.ctxCurrent))
		return m, nil
	case "1":
		if m.mode != ModeApp {
			m.mode, m.focus, m.tab = ModeApp, paneNS, 0
			m.clampAll()
			cmd := tea.Batch(m.refreshCmd(), m.syncLogStream())
			return m, cmd
		}
		return m, nil
	case "2":
		if m.mode != ModeInfra {
			m.mode, m.focus, m.tab = ModeInfra, paneNode, 0
			m.stopLogs()
			m.clampAll()
			return m, m.refreshCmd()
		}
		return m, nil
	case "r":
		m.status = "更新中…"
		return m, m.refreshCmd()
	case "M":
		// マウスを掴んでいるあいだは端末側の範囲選択ができなくなるので、
		// ログをコピーしたいときのために手放せるようにする。
		m.mouse = !m.mouse
		if m.mouse {
			m.status = "マウス: on"
			return m, tea.EnableMouseCellMotion
		}
		m.status = "マウス: off（端末の範囲選択が使えます）"
		return m, tea.DisableMouse
	case "tab":
		m.cycleFocus(1)
		return m, nil
	case "shift+tab":
		m.cycleFocus(-1)
		return m, nil
	case "/":
		m.inputActive = true
		m.inputTarget = m.filterTargetForFocus()
		m.input.SetValue(m.filters[m.inputTarget])
		m.input.CursorEnd()
		m.input.Focus()
		return m, nil
	case "esc":
		if m.filters[m.filterTargetForFocus()] != "" {
			m.filters[m.filterTargetForFocus()] = ""
			m.clampAll()
		}
		return m, nil
	case "[", "]":
		d := 1
		if msg.String() == "[" {
			d = -1
		}
		cmd := m.switchTab(d)
		return m, cmd
	}

	// 詳細ペインに焦点がある間は左右でタブを切り替える。
	if m.focus == paneDetail || m.focus == paneNDetail {
		switch msg.String() {
		case "left", "right":
			d := 1
			if msg.String() == "left" {
				d = -1
			}
			cmd := m.switchTab(d)
			return m, cmd
		}
		if m.mode == ModeApp && m.tab == 1 {
			if cmd, handled := m.handleLogKey(msg); handled {
				return m, cmd
			}
		}
	}

	switch msg.String() {
	case "enter":
		return m.handleEnter()
	}

	return m.handleNav(msg)
}

func (m *Model) handleLogKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "f":
		m.logFollow = !m.logFollow
		if m.logFollow {
			m.logOff = 0
		}
		m.status = map[bool]string{true: "ログ追従: on", false: "ログ追従: off"}[m.logFollow]
		return nil, true
	case "p":
		m.logPrev = !m.logPrev
		m.status = map[bool]string{true: "直前に落ちたコンテナのログを表示", false: "現在のコンテナのログを表示"}[m.logPrev]
		return m.syncLogStream(), true
	case "a":
		if m.logScope == "" {
			// Pod タブでの選択に合わせて 1 Pod に絞る。
			if p := m.selectedPodForScope(); p != nil {
				m.logScope = p.Name
				m.status = "ログの対象: " + p.Name
			}
		} else {
			m.logScope = ""
			m.status = "ログの対象: ワークロード全体"
		}
		return m.syncLogStream(), true
	}
	return nil, false
}

func (m *Model) selectedPodForScope() *k8s.Pod {
	w := m.currentWorkload()
	if w == nil || len(w.Pods) == 0 {
		return nil
	}
	if m.pDet.idx < len(w.Pods) {
		return w.Pods[m.pDet.idx]
	}
	return w.Pods[0]
}

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.focus {
	case paneNS:
		vs := m.visibleNamespaces()
		if len(vs) > 0 {
			m.nsSel = vs[m.pNS.idx]
			m.workloads = nil
			m.pWL, m.pDet = pane{}, pane{}
			m.focus = paneWL
			m.backToPodTab()
			cmd := tea.Batch(m.fetchWorkloads(), m.syncLogStream())
			return m, cmd
		}
	case paneWL:
		m.focus = paneDetail
		m.pDet = pane{}
		cmd := tea.Batch(m.syncLogStream(), m.eventsIfNeeded())
		return m, cmd
	case paneNode:
		m.focus = paneNDetail
		m.pNDet = pane{}
		cmd := m.eventsIfNeeded()
		return m, cmd
	case paneDetail:
		// Pod 一覧で Enter → その Pod のログへ。
		if m.mode == ModeApp && m.tab == 0 {
			pods := m.visibleDetailPods()
			if len(pods) > 0 && m.pDet.idx < len(pods) {
				m.logScope = pods[m.pDet.idx].Name
				m.tab = 1
				m.pDet = pane{}
				m.status = "ログの対象: " + m.logScope
				cmd := m.syncLogStream()
				return m, cmd
			}
		}
	}
	return m, nil
}

func (m *Model) switchTab(d int) tea.Cmd {
	return m.setTab((m.tab + d + 3) % 3)
}

func (m *Model) setTab(i int) tea.Cmd {
	if i == m.tab || i < 0 || i > 2 {
		return nil
	}
	m.tab = i
	m.pDet, m.pNDet = pane{}, pane{}
	m.clampAll()
	return tea.Batch(m.syncLogStream(), m.eventsIfNeeded())
}

func (m *Model) eventsIfNeeded() tea.Cmd {
	if !m.detailTabIsEvents() {
		return nil
	}
	m.events = nil
	return m.fetchEvents()
}

func (m Model) handleNav(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n, h, p := m.focusedList()
	if p == nil {
		return m, nil
	}
	// ログはテキストなので行単位のスクロールを別扱いにする。
	if m.mode == ModeApp && m.focus == paneDetail && m.tab == 1 {
		return m.scrollLogs(msg)
	}
	prev := p.idx
	switch msg.String() {
	case "j", "down":
		p.move(1, n, h)
	case "k", "up":
		p.move(-1, n, h)
	case "ctrl+d", "pgdown":
		p.move(h/2, n, h)
	case "ctrl+u", "pgup":
		p.move(-h/2, n, h)
	case "g", "home":
		p.idx = 0
		p.clamp(n, h)
	case "G", "end":
		p.idx = n - 1
		p.clamp(n, h)
	default:
		return m, nil
	}
	if p.idx == prev {
		return m, nil
	}
	cmd := m.onSelectionChanged()
	return m, cmd
}

// onSelectionChanged は選択が動いたときに、詳細側の付随データを取り直す。
func (m *Model) onSelectionChanged() tea.Cmd {
	switch m.focus {
	case paneNS:
		vs := m.visibleNamespaces()
		if len(vs) > 0 && vs[m.pNS.idx] != m.nsSel {
			m.nsSel = vs[m.pNS.idx]
			m.workloads = nil
			m.pWL, m.pDet = pane{}, pane{}
			m.backToPodTab()
			return tea.Batch(m.fetchWorkloads(), m.syncLogStream(), m.eventsIfNeeded())
		}
	case paneWL:
		m.pDet = pane{}
		m.backToPodTab()
		return tea.Batch(m.syncLogStream(), m.eventsIfNeeded())
	case paneNode:
		m.pNDet = pane{}
		return m.eventsIfNeeded()
	}
	return nil
}

func (m Model) scrollLogs(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.visibleLogLines())
	h := m.detRows()
	maxOff := maxInt(0, n-h)
	switch msg.String() {
	case "j", "down":
		m.logOff = maxInt(0, m.logOff-1)
	case "k", "up":
		m.logOff = minInt(maxOff, m.logOff+1)
	case "ctrl+d", "pgdown":
		m.logOff = maxInt(0, m.logOff-h/2)
	case "ctrl+u", "pgup":
		m.logOff = minInt(maxOff, m.logOff+h/2)
	case "g", "home":
		m.logOff = maxOff
	case "G", "end":
		m.logOff = 0
	default:
		return m, nil
	}
	m.logFollow = m.logOff == 0
	cmd := m.maybeLoadMoreLogs()
	return m, cmd
}

// focusedList は今の焦点に対応する「要素数・表示行数・スクロール位置」を返す。
func (m *Model) focusedList() (int, int, *pane) {
	switch m.focus {
	case paneNS:
		return len(m.visibleNamespaces()), m.nsRows(), &m.pNS
	case paneWL:
		return len(m.visibleWorkloads()), m.wlRows(), &m.pWL
	case paneNode:
		return len(m.visibleNodes()), m.nodeRows(), &m.pNode
	case paneDetail:
		switch m.tab {
		case 0:
			return len(m.visibleDetailPods()), m.detRows(), &m.pDet
		case 2:
			return len(m.visibleEvents()), m.detRows(), &m.pDet
		}
		return 0, m.detRows(), &m.pDet
	case paneNDetail:
		switch m.tab {
		case 0:
			return len(m.visibleDetailPods()), m.detRows(), &m.pNDet
		case 2:
			return len(m.visibleEvents()), m.detRows(), &m.pNDet
		}
		return 0, m.detRows(), &m.pNDet
	}
	return 0, 1, nil
}

func (m *Model) filterTargetForFocus() paneID {
	switch m.focus {
	case paneDetail, paneNDetail:
		return m.detailFilterKey()
	}
	return m.focus
}

func (m *Model) cycleFocus(d int) {
	var order []paneID
	if m.mode == ModeApp {
		order = []paneID{paneNS, paneWL, paneDetail}
	} else {
		order = []paneID{paneNode, paneNDetail}
	}
	i := 0
	for k, p := range order {
		if p == m.focus {
			i = k
		}
	}
	m.focus = order[(i+d+len(order))%len(order)]
}

func (m *Model) clampAll() {
	m.pNS.clamp(len(m.visibleNamespaces()), m.nsRows())
	m.pWL.clamp(len(m.visibleWorkloads()), m.wlRows())
	m.pNode.clamp(len(m.visibleNodes()), m.nodeRows())
	switch m.tab {
	case 0:
		n := len(m.visibleDetailPods())
		m.pDet.clamp(n, m.detRows())
		m.pNDet.clamp(n, m.detRows())
	case 2:
		n := len(m.visibleEvents())
		m.pDet.clamp(n, m.detRows())
		m.pNDet.clamp(n, m.detRows())
	}
}

func (m Model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "?":
		m.overlay = overlayNone
		return m, nil
	}
	if m.overlay != overlayContexts {
		return m, nil
	}
	switch msg.String() {
	case "j", "down":
		m.ovIdx = minInt(len(m.ctxNames)-1, m.ovIdx+1)
	case "k", "up":
		m.ovIdx = maxInt(0, m.ovIdx-1)
	case "enter":
		if m.ovIdx < len(m.ctxNames) {
			name := m.ctxNames[m.ovIdx]
			m.overlay = overlayNone
			m.status = name + " へ接続しています…"
			return m, func() tea.Msg {
				cl, err := k8s.NewClient(name)
				return clientMsg{client: cl, err: err}
			}
		}
	}
	return m, nil
}

// --- ログストリームの生死管理 ---

// logStreamKey は「今どのログを開いているべきか」を表す。
// これが変わったときだけストリームを張り直す。
func (m *Model) logStreamKey() string {
	if m.mode != ModeApp || m.tab != 1 || m.client == nil {
		return ""
	}
	w := m.currentWorkload()
	if w == nil {
		return ""
	}
	pods := m.logTargetPods(w)
	key := fmt.Sprintf("%s|%s|%s|%s|prev=%v", m.ctxCurrent, w.Ref.Namespace, w.Ref.Kind+"/"+w.Ref.Name, m.logScope, m.logPrev)
	for _, p := range pods {
		key += "|" + p.Name
	}
	return key
}

func (m *Model) logTargetPods(w *k8s.Workload) []*k8s.Pod {
	var pods []*k8s.Pod
	for _, p := range w.Pods {
		if m.logScope != "" && p.Name != m.logScope {
			continue
		}
		if p.Phase == "Pending" && p.ReadyCount == 0 && p.Restarts == 0 {
			continue // まだコンテナが起きておらずログが存在しない
		}
		pods = append(pods, p)
	}
	return pods
}

func (m *Model) syncLogStream() tea.Cmd {
	key := m.logStreamKey()
	if key == m.logKey {
		return nil
	}
	m.stopLogs()
	m.logKey = key
	if key == "" {
		return nil
	}
	w := m.currentWorkload()
	if w == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.logCancel = cancel
	m.logGen++
	m.logLines = nil
	m.logSeen = map[string]bool{}
	m.logOff = 0
	m.logTail = initialLogTail
	m.logLoading = false
	m.logExhausted = false
	m.logStream = m.client.StreamPodLogs(ctx, m.logTargetPods(w), k8s.LogOptions{
		TailLines: initialLogTail,
		Previous:  m.logPrev,
		Follow:    !m.logPrev, // 直前のコンテナのログは follow できない
	})
	return waitLogs(m.logStream, m.logGen)
}

func (m *Model) stopLogs() {
	if m.logStream != nil {
		m.logStream.Close()
		m.logStream = nil
	}
	if m.logCancel != nil {
		m.logCancel()
		m.logCancel = nil
	}
	m.logKey = ""
	m.logGen++
}

func waitLogs(s *k8s.LogStream, gen int) tea.Cmd {
	if s == nil {
		return nil
	}
	return func() tea.Msg {
		l, ok := <-s.Lines
		if !ok {
			return logClosedMsg{gen}
		}
		batch := []k8s.LogLine{l}
		for len(batch) < 512 {
			select {
			case l2, ok := <-s.Lines:
				if !ok {
					return logBatchMsg{gen: gen, lines: batch}
				}
				batch = append(batch, l2)
			default:
				return logBatchMsg{gen: gen, lines: batch}
			}
		}
		return logBatchMsg{gen: gen, lines: batch}
	}
}

func indexOf[T comparable](xs []T, v T) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return 0
}

// pickDefaultNamespace は最初に見せる名前空間。kube- 系より業務側を優先する。
func pickDefaultNamespace(ns []string) string {
	for _, n := range ns {
		switch n {
		case "default", "kube-system", "kube-public", "kube-node-lease", "kube-flannel":
			continue
		}
		return n
	}
	if len(ns) > 0 {
		return ns[0]
	}
	return ""
}

// appendLogLines は複数 Pod から届いた行を時刻順に差し込む。
// ストリームは Pod ごとに独立しているので、到着順に積むと最初の tail 分が
// Pod ごとの塊になってしまい、集約して読む意味が無くなる。
const logMergeWindow = 400

func (m *Model) appendLogLines(batch []k8s.LogLine) {
	added := 0
	for _, l := range batch {
		if !m.markSeen(l) {
			continue
		}
		added++
		i := len(m.logLines)
		if !l.Time.IsZero() {
			limit := maxInt(0, i-logMergeWindow)
			for i > limit {
				prev := m.logLines[i-1]
				if prev.Time.IsZero() || !prev.Time.After(l.Time) {
					break
				}
				i--
			}
		}
		if i == len(m.logLines) {
			m.logLines = append(m.logLines, l)
			continue
		}
		m.logLines = append(m.logLines, k8s.LogLine{})
		copy(m.logLines[i+1:], m.logLines[i:])
		m.logLines[i] = l
	}
	// 追従を止めているあいだは、行が増えても見ている位置が動かないようにする
	// （末尾からのオフセットで持っているので、増えた分だけ足す）。
	if !m.logFollow && added > 0 {
		m.logOff = minInt(m.logOff+added, maxInt(0, len(m.logLines)-1))
	}
	if len(m.logLines) > maxLogLines {
		drop := len(m.logLines) - maxLogLines
		m.logLines = m.logLines[drop:]
		m.logOff = maxInt(0, m.logOff-drop)
	}
}

// markSeen は初めて見る行なら true。重複していれば false。
func (m *Model) markSeen(l k8s.LogLine) bool {
	if l.Notice {
		return true
	}
	if m.logSeen == nil {
		m.logSeen = map[string]bool{}
	}
	k := l.Key()
	if m.logSeen[k] {
		return false
	}
	m.logSeen[k] = true
	return true
}

// mergeLogHistory はさかのぼって取り直した行を、既にある行と重複させずに差し込む。
// 末尾からのオフセットで表示位置を持っているので、前に足しても見ている場所は動かない。
func (m *Model) mergeLogHistory(lines []k8s.LogLine) int {
	added := 0
	for _, l := range lines {
		if !m.markSeen(l) {
			continue
		}
		i := len(m.logLines)
		for i > 0 {
			prev := m.logLines[i-1]
			if prev.Time.IsZero() || !prev.Time.After(l.Time) {
				break
			}
			i--
		}
		m.logLines = append(m.logLines, k8s.LogLine{})
		copy(m.logLines[i+1:], m.logLines[i:])
		m.logLines[i] = l
		added++
	}
	return added
}

// maybeLoadMoreLogs は上端に着いていたら、もう一段さかのぼる取得を始める。
func (m *Model) maybeLoadMoreLogs() tea.Cmd {
	if m.mode != ModeApp || m.tab != 1 || m.client == nil {
		return nil
	}
	if m.logLoading || m.logExhausted || m.logKey == "" {
		return nil
	}
	n, h := len(m.visibleLogLines()), m.detRows()
	if m.logOff < maxInt(0, n-h) {
		return nil // まだ上端ではない
	}
	next := m.logTail * 3
	if next > maxLogTail {
		next = maxLogTail
	}
	if next <= m.logTail {
		m.logExhausted = true
		m.status = "これ以上さかのぼれません（1 Pod あたり " + k8s.FormatInt(maxLogTail) + " 行が上限）"
		return nil
	}

	w := m.currentWorkload()
	if w == nil {
		return nil
	}
	pods, cl, gen, prev := m.logTargetPods(w), m.client, m.logGen, m.logPrev
	m.logTail = next
	m.logLoading = true
	m.status = "過去のログを読み込み中…"

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		lines := cl.FetchPodLogs(ctx, pods, k8s.LogOptions{TailLines: next, Previous: prev})
		return logHistoryMsg{gen: gen, lines: lines}
	}
}

// backToPodTab は見ている対象が変わったときに Pod 一覧へ戻す。
// 別のワークロードのログやイベントを開いたまま対象だけ入れ替わると、
// 何を見ているのか分からなくなるため。
func (m *Model) backToPodTab() {
	m.tab = 0
	m.logScope = ""
}
