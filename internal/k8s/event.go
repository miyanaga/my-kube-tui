package k8s

import (
	"context"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
)

// EventsForUIDs はワークロードに関係するイベントだけを時系列で返す。
// ワークロード本体・ReplicaSet/Job・Pod の UID をまとめて渡す想定。
// 「起動する前に落ちている」理由はログではなくここに出るので、ログと同格に扱う。
func (c *Client) EventsForUIDs(ctx context.Context, ns string, uids []types.UID) ([]Event, error) {
	want := make(map[types.UID]bool, len(uids))
	for _, u := range uids {
		want[u] = true
	}
	list, err := c.Clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{Limit: 1000})
	if err != nil {
		return nil, err
	}
	var out []Event
	for i := range list.Items {
		e := &list.Items[i]
		if !want[e.InvolvedObject.UID] {
			continue
		}
		out = append(out, convertEvent(e))
	}
	sortEvents(out)
	return out, nil
}

// EventsForNode はノード自身に出ているイベント。
func (c *Client) EventsForNode(ctx context.Context, nodeName string) ([]Event, error) {
	sel := fields.AndSelectors(
		fields.OneTermEqualSelector("involvedObject.kind", "Node"),
		fields.OneTermEqualSelector("involvedObject.name", nodeName),
	).String()
	list, err := c.Clientset.CoreV1().Events("").List(ctx, metav1.ListOptions{FieldSelector: sel, Limit: 500})
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, convertEvent(&list.Items[i]))
	}
	sortEvents(out)
	return out, nil
}

func convertEvent(e *corev1.Event) Event {
	t := e.LastTimestamp.Time
	if t.IsZero() {
		t = e.EventTime.Time
	}
	if t.IsZero() {
		t = e.CreationTimestamp.Time
	}
	count := e.Count
	if count == 0 {
		count = 1
	}
	return Event{
		Time:    t,
		Type:    e.Type,
		Reason:  e.Reason,
		Object:  e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
		Message: e.Message,
		Count:   count,
	}
}

func sortEvents(evs []Event) {
	sort.Slice(evs, func(i, j int) bool { return evs[i].Time.After(evs[j].Time) })
}

// ShortAge は 12d / 3h / 5m のような短い経過表記。
func ShortAge(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return ShortDuration(time.Since(t))
}

func ShortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return itoa(int(d.Hours())) + "h"
	default:
		return itoa(int(d.Hours()/24)) + "d"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
