package k8s

import (
	"bufio"
	"context"
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
		opts.TailLines = 200
	}

	for _, p := range pods {
		containers := p.Containers
		if len(containers) == 0 {
			containers = []string{""}
		}
		for _, cn := range containers {
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

func (s *LogStream) emit(ctx context.Context, l LogLine) {
	select {
	case s.Lines <- l:
	case <-ctx.Done():
	}
}

func (c *Client) streamOne(ctx context.Context, s *LogStream, p *Pod, container string, opts LogOptions) {
	defer s.wg.Done()

	tail := opts.TailLines
	req := c.Clientset.CoreV1().Pods(p.Namespace).GetLogs(p.Name, &corev1.PodLogOptions{
		Container:  container,
		Follow:     opts.Follow,
		Previous:   opts.Previous,
		TailLines:  &tail,
		Timestamps: true,
	})
	rc, err := req.Stream(ctx)
	if err != nil {
		s.emit(ctx, LogLine{Pod: p.Name, Container: container, Time: time.Now(), Notice: true,
			Text: "ログを取得できません: " + trimAPIError(err.Error())})
		return
	}
	defer rc.Close()

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
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
