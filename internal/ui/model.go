package ui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/miyanaga/my-kube-tui/internal/k8s"
)

// Mode は最上位の分岐。アプリから見るか、インフラから見るか。
type Mode int

const (
	ModeApp Mode = iota
	ModeInfra
)

// paneID は焦点とフィルタの単位。
type paneID string

const (
	paneNS      paneID = "ns"
	paneWL      paneID = "wl"
	paneDetail  paneID = "detail"
	paneNode    paneID = "node"
	paneNDetail paneID = "ndetail"
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayContexts
	overlayHelp
)

// pane はスクロールする一覧の位置。
type pane struct {
	idx int
	off int
}

func (p *pane) clamp(n, h int) {
	if n == 0 {
		p.idx, p.off = 0, 0
		return
	}
	if p.idx >= n {
		p.idx = n - 1
	}
	if p.idx < 0 {
		p.idx = 0
	}
	if h <= 0 {
		h = 1
	}
	if p.idx < p.off {
		p.off = p.idx
	}
	if p.idx >= p.off+h {
		p.off = p.idx - h + 1
	}
	if p.off > n-h {
		p.off = n - h
	}
	if p.off < 0 {
		p.off = 0
	}
}

func (p *pane) move(d, n, h int) {
	p.idx += d
	p.clamp(n, h)
}

// scroll は表示位置だけを動かし、選択は画面内に引き戻す。
// ホイールで一覧を眺めるときに、選択が勝手に飛ばないようにするため。
func (p *pane) scroll(d, n, h int) {
	if n == 0 {
		p.idx, p.off = 0, 0
		return
	}
	p.off += d
	if p.off > n-h {
		p.off = n - h
	}
	if p.off < 0 {
		p.off = 0
	}
	if p.idx < p.off {
		p.idx = p.off
	}
	if p.idx >= p.off+h {
		p.idx = p.off + h - 1
	}
	if p.idx >= n {
		p.idx = n - 1
	}
	if p.idx < 0 {
		p.idx = 0
	}
}

// Model は TUI 全体の状態。
type Model struct {
	ctxNames   []string
	ctxCurrent string
	client     *k8s.Client
	connErr    error

	mode  Mode
	focus paneID
	tab   int // 詳細ペインのタブ（モードごとに意味が変わる）

	w, h int

	namespaces []string
	nsSel      string
	workloads  []*k8s.Workload
	nodes      []*k8s.Node
	events     []k8s.Event
	eventsKey  string
	fetchedAt  time.Time
	loadErr    error

	pNS, pWL, pDet, pNode, pNDet pane
	filters                      map[paneID]string

	// ログ
	logStream    *k8s.LogStream
	logCancel    context.CancelFunc
	logLines     []k8s.LogLine
	logSeen      map[string]bool // さかのぼり読み込みと追従の重複を弾く
	logKey       string
	logGen       int
	logFollow    bool
	logPrev      bool
	logOff       int // 末尾からの行数（0 = 追従）
	logScope     string
	logTail      int64 // 今 Pod あたり何行までさかのぼっているか
	logLoading   bool
	logExhausted bool

	mouse bool

	input       textinput.Model
	inputActive bool
	inputTarget paneID

	overlay overlayKind
	ovIdx   int
	status  string
	spinner int
}

const (
	maxLogLines = 20000

	// ログは最初に直近 initialLogTail 行だけ読み、上端に着いたら 3 倍ずつ
	// さかのぼる。Kubernetes のログ API に「途中から」は無く tailLines を
	// 増やして引き直すしかないので、一気に取らずに段階的に広げる。
	initialLogTail = 100
	maxLogTail     = 20000
)

// New は起動時のモデルを作る。接続に失敗しても TUI は立ち上げ、
// コンテキスト切り替えでやり直せるようにする。
func New(initialContext string) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 120

	m := Model{
		mode:      ModeApp,
		focus:     paneNS,
		filters:   map[paneID]string{},
		input:     ti,
		logFollow: true,
		logTail:   initialLogTail,
		mouse:     true,
	}
	names, current, err := k8s.Contexts()
	m.ctxNames = names
	if initialContext != "" {
		current = initialContext
	}
	m.ctxCurrent = current
	if err != nil {
		m.connErr = err
		return m
	}
	cl, err := k8s.NewClient(current)
	if err != nil {
		m.connErr = err
		return m
	}
	m.client = cl
	m.ctxCurrent = cl.ContextName
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd())
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type namespacesMsg struct {
	items []string
	err   error
}

type workloadsMsg struct {
	ns    string
	items []*k8s.Workload
	err   error
}

type nodesMsg struct {
	items []*k8s.Node
	err   error
}

type eventsMsg struct {
	key   string
	items []k8s.Event
	err   error
}

type clientMsg struct {
	client *k8s.Client
	err    error
}

type logBatchMsg struct {
	gen   int
	lines []k8s.LogLine
}

type logClosedMsg struct{ gen int }

// logHistoryMsg はさかのぼって取り直した結果。
type logHistoryMsg struct {
	gen   int
	lines []k8s.LogLine
}

func withTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 12*time.Second)
}

func (m Model) fetchNamespaces() tea.Cmd {
	cl := m.client
	if cl == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := withTimeout()
		defer cancel()
		items, err := cl.Namespaces(ctx)
		return namespacesMsg{items: items, err: err}
	}
}

func (m Model) fetchWorkloads() tea.Cmd {
	cl, ns := m.client, m.nsSel
	if cl == nil || ns == "" {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := withTimeout()
		defer cancel()
		items, err := cl.Workloads(ctx, ns)
		return workloadsMsg{ns: ns, items: items, err: err}
	}
}

func (m Model) fetchNodes() tea.Cmd {
	cl := m.client
	if cl == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := withTimeout()
		defer cancel()
		items, err := cl.Nodes(ctx)
		return nodesMsg{items: items, err: err}
	}
}

func (m Model) fetchEvents() tea.Cmd {
	cl := m.client
	if cl == nil {
		return nil
	}
	if m.mode == ModeApp {
		w := m.currentWorkload()
		if w == nil {
			return nil
		}
		ns, uids := w.Ref.Namespace, w.RelatedUIDs
		key := "wl:" + ns + "/" + w.Ref.Kind + "/" + w.Ref.Name
		return func() tea.Msg {
			ctx, cancel := withTimeout()
			defer cancel()
			items, err := cl.EventsForUIDs(ctx, ns, uids)
			return eventsMsg{key: key, items: items, err: err}
		}
	}
	n := m.currentNode()
	if n == nil {
		return nil
	}
	name := n.Name
	key := "node:" + name
	return func() tea.Msg {
		ctx, cancel := withTimeout()
		defer cancel()
		items, err := cl.EventsForNode(ctx, name)
		return eventsMsg{key: key, items: items, err: err}
	}
}

// refreshCmd は今のモードに必要なものだけを取りに行く。
func (m Model) refreshCmd() tea.Cmd {
	if m.client == nil {
		return nil
	}
	cmds := []tea.Cmd{}
	if m.mode == ModeApp {
		cmds = append(cmds, m.fetchNamespaces(), m.fetchWorkloads())
	} else {
		cmds = append(cmds, m.fetchNodes())
	}
	if m.detailTabIsEvents() {
		cmds = append(cmds, m.fetchEvents())
	}
	return tea.Batch(cmds...)
}

func (m *Model) currentWorkload() *k8s.Workload {
	ws := m.visibleWorkloads()
	if len(ws) == 0 || m.pWL.idx >= len(ws) {
		return nil
	}
	return ws[m.pWL.idx]
}

func (m *Model) currentNode() *k8s.Node {
	ns := m.visibleNodes()
	if len(ns) == 0 || m.pNode.idx >= len(ns) {
		return nil
	}
	return ns[m.pNode.idx]
}

func (m *Model) detailTabIsEvents() bool {
	return m.tab == 2
}
