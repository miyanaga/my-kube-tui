package k8s

import (
	"bufio"
	"context"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// LogLine は集約ログの 1 行。どの Pod・どのコンテナから来たかを必ず持つ。
type LogLine struct {
	Pod       string
	Container string
	Time      time.Time
	Text      string
	Notice    bool // ストリームの開始・終了・エラーを伝える内部行
}

// Key は重複排除の鍵。さかのぼり読み込みは追従中の行と必ず重なるので、
// 同じ行を二度積まないためにこれで突き合わせる。
func (l LogLine) Key() string {
	return l.Pod + "\x00" + l.Container + "\x00" + l.Time.Format(time.RFC3339Nano) + "\x00" + l.Text
}

// LogOptions はログ取得の条件。
type LogOptions struct {
	TailLines int64
	Previous  bool // 直前に落ちたコンテナのログ（CrashLoopBackOff の原因を見る）
	Follow    bool
}

// LogStream は複数 Pod のログを 1 本にまとめたもの。
type LogStream struct {
	Lines  chan LogLine
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
}

// Close はすべてのストリームを止める。
func (s *LogStream) Close() {
	s.once.Do(func() {
		s.cancel()
		go func() {
			s.wg.Wait()
			close(s.Lines)
		}()
	})
}

// StreamPodLogs は渡された Pod 群のログを 1 本のチャンネルに集約して流す。
// ワークロード単位でも、Pod 1 個に絞っても同じ入口を使う。
func (c *Client) StreamPodLogs(parent context.Context, pods []*Pod, opts LogOptions) *LogStream {
	ctx, cancel := context.WithCancel(parent)
	s := &LogStream{Lines: make(chan LogLine, 4096), cancel: cancel}

	if opts.TailLines <= 0 {
		opts.TailLines = 100
	}

	for _, p := range pods {
		for _, cn := range containersOf(p) {
			s.wg.Add(1)
			go c.streamOne(ctx, s, p, cn, opts)
		}
	}
	if len(pods) == 0 {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.emit(ctx, LogLine{Notice: true, Time: time.Now(), Text: "対象の Pod がありません"})
		}()
	}
	return s
}

// FetchPodLogs は追従せずに一括で取得する。さかのぼり読み込み専用。
// tailLines を増やして引き直し、既に持っている行は呼び出し側で捨てる前提。
func (c *Client) FetchPodLogs(ctx context.Context, pods []*Pod, opts LogOptions) []LogLine {
	opts.Follow = false
	if opts.TailLines <= 0 {
		opts.TailLines = 100
	}

	var (
		mu  sync.Mutex
		out []LogLine
		wg  sync.WaitGroup
	)
	for _, p := range pods {
		for _, cn := range containersOf(p) {
			wg.Add(1)
			go func(p *Pod, cn string) {
				defer wg.Done()
				rc, err := c.openLogs(ctx, p, cn, opts)
				if err != nil {
					return // さかのぼりの失敗は黙って諦める（追従側が同じ理由を既に出している）
				}
				defer rc.Close()
				lines := scanLogs(rc, p, cn)
				mu.Lock()
				out = append(out, lines...)
				mu.Unlock()
			}(p, cn)
		}
	}
	wg.Wait()

	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

func containersOf(p *Pod) []string {
	if len(p.Containers) == 0 {
		return []string{""}
	}
	return p.Containers
}

func (s *LogStream) emit(ctx context.Context, l LogLine) {
	select {
	case s.Lines <- l:
	case <-ctx.Done():
	}
}

func (c *Client) openLogs(ctx context.Context, p *Pod, container string, opts LogOptions) (io.ReadCloser, error) {
	tail := opts.TailLines
	req := c.Clientset.CoreV1().Pods(p.Namespace).GetLogs(p.Name, &corev1.PodLogOptions{
		Container:  container,
		Follow:     opts.Follow,
		Previous:   opts.Previous,
		TailLines:  &tail,
		Timestamps: true,
	})
	return req.Stream(ctx)
}

func newScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return sc
}

func scanLogs(r io.Reader, p *Pod, container string) []LogLine {
	var out []LogLine
	sc := newScanner(r)
	for sc.Scan() {
		ts, text := splitTimestamp(sc.Text())
		out = append(out, LogLine{Pod: p.Name, Container: container, Time: ts, Text: text})
	}
	return out
}

func (c *Client) streamOne(ctx context.Context, s *LogStream, p *Pod, container string, opts LogOptions) {
	defer s.wg.Done()

	rc, err := c.openLogs(ctx, p, container, opts)
	if err != nil {
		s.emit(ctx, LogLine{Pod: p.Name, Container: container, Time: time.Now(), Notice: true,
			Text: "ログを取得できません: " + trimAPIError(err.Error())})
		return
	}
	defer rc.Close()

	sc := newScanner(rc)
	for sc.Scan() {
		ts, text := splitTimestamp(sc.Text())
		s.emit(ctx, LogLine{Pod: p.Name, Container: container, Time: ts, Text: text})
		if ctx.Err() != nil {
			return
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		s.emit(ctx, LogLine{Pod: p.Name, Container: container, Time: time.Now(), Notice: true,
			Text: "ログが途切れました: " + trimAPIError(err.Error())})
	}
}

// splitTimestamp は kubelet が付ける RFC3339Nano の先頭タイムスタンプを剥がす。
func splitTimestamp(line string) (time.Time, string) {
	i := strings.IndexByte(line, ' ')
	if i <= 0 {
		return time.Time{}, line
	}
	t, err := time.Parse(time.RFC3339Nano, line[:i])
	if err != nil {
		return time.Time{}, line
	}
	return t, line[i+1:]
}

func trimAPIError(s string) string {
	if i := strings.Index(s, ": "); i > 0 && len(s) > 120 {
		return s[i+2:]
	}
	return s
}
