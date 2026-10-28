package main

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// flowConn 内核 /connections 里的一条连接，只取用得到的字段。chains 从出口往外排：
// 走代理是「节点、节点选择、其他/国内」，直连是「直连出口、直连、国内/其他」
type flowConn struct {
	ID       string
	Metadata struct {
		Host            string
		DestinationIP   string `json:"destinationIP"`
		DestinationPort string
	}
	Chains          []string
	RulePayload     string
	Upload          int64
	Download        int64
	ShoesGeneration int64
}

// hostFlow 连接页的一行：一个域名，以一种走法（直连，或经某个节点）出去的汇总。
// 同一个域名换了走法（比如切了模式）就是另一行
type hostFlow struct {
	Host   string
	Port   string
	Direct bool
	Via    string // 走代理时经过的节点，内核里的名字
	Rule   string // 命中的规则：局域网、国内域名、国内 IP、其余
	Active int    // 现在还开着几条
	Total  int    // 一共见过几条
	Up     int64
	Down   int64
	First  time.Time
	Last   time.Time // 最后一次看到它有连接开着
	ids    []string  // 还开着的连接，断开用
}

type flowKey struct {
	host, via string
	direct    bool
}

type openFlow struct {
	key      flowKey
	up, down int64
	fresh    bool // 清空记录以后还没再见过，再见到时算一条
}

// maxHostFlows 最多记多少行，多了丢掉最久没动静的。只在内存里，退出就没了
const maxHostFlows = 1000

// flowBook 攒下来的连接记录。内核只报现在开着的连接，每秒问一次，没见过的 id 算一条新连接，
// 流量按每次比上次多出来的加。开了不到一秒就关掉的连接会漏掉
type flowBook struct {
	mu      sync.Mutex
	open    map[string]openFlow
	hosts   map[flowKey]*hostFlow
	version int // 记录有变化就加一，界面据此决定要不要重画
}

// flowRoute 这条连接怎么出去的：chains 里有「直连」组就是直连，不然第一项是节点
func flowRoute(chains []string) (direct bool, via string) {
	if len(chains) == 0 {
		return false, ""
	}
	if chains[0] == directMember || slices.Contains(chains, groupDirect) {
		return true, ""
	}
	return false, chains[0]
}

// ruleLabel 把内核报的规则（rulePayload）翻成人话，对应 generateConfig 里的几条
func ruleLabel(payload string) string {
	switch {
	case strings.Contains(payload, "geosite-cn"):
		return "国内域名"
	case strings.Contains(payload, "geoip-cn"):
		return "国内 IP"
	case strings.Contains(payload, privateRanges[0]):
		return "局域网"
	case strings.Contains(payload, `"0.0.0.0/0"`):
		return "其余"
	}
	// direct-domains.txt 里的一行，内核原样报成 "example.com"
	if i := strings.LastIndex(payload, `: "`); i >= 0 && strings.HasSuffix(payload, `"`) && !strings.Contains(payload[i:], "/") {
		return "自定义直连"
	}
	return ""
}

// record 记下这一次看到的连接，返回记录有没有变化
func (b *flowBook) record(conns []flowConn, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hosts == nil {
		b.open, b.hosts = map[string]openFlow{}, map[flowKey]*hostFlow{}
	}
	changed := false
	wasActive := map[flowKey]int{}
	for key, h := range b.hosts {
		if h.Active > 0 {
			wasActive[key] = h.Active
		}
		h.Active, h.ids = 0, h.ids[:0]
	}
	seen := make(map[string]bool, len(conns))
	for _, conn := range conns {
		id := conn.ID
		if conn.ShoesGeneration != 0 {
			id = strconv.FormatInt(conn.ShoesGeneration, 10) + "/" + conn.ID
		}
		seen[id] = true
		host := conn.Metadata.Host
		if host == "" {
			host = conn.Metadata.DestinationIP
		}
		direct, via := flowRoute(conn.Chains)
		key := flowKey{host: host, via: via, direct: direct}
		h := b.hosts[key]
		if h == nil {
			h = &hostFlow{Host: host, Direct: direct, Via: via, First: now}
			b.hosts[key] = h
		}
		prev, known := b.open[id]
		if !known || prev.fresh {
			h.Total++
			changed = true
		}
		if up, down := conn.Upload-prev.up, conn.Download-prev.down; up > 0 || down > 0 {
			h.Up += max(up, 0)
			h.Down += max(down, 0)
			changed = true
		}
		b.open[id] = openFlow{key: key, up: conn.Upload, down: conn.Download}
		h.Port, h.Rule, h.Last = conn.Metadata.DestinationPort, ruleLabel(conn.RulePayload), now
		h.Active++
		h.ids = append(h.ids, conn.ID)
	}
	for id := range b.open {
		if !seen[id] {
			delete(b.open, id)
			changed = true
		}
	}
	for key, h := range b.hosts {
		if wasActive[key] != h.Active {
			changed = true
		}
	}
	if len(b.hosts) > maxHostFlows {
		b.trim()
	}
	if changed {
		b.version++
	}
	return changed
}

// trim 丢掉最久没动静、现在也没开着的行，直到不超过 maxHostFlows
func (b *flowBook) trim() {
	var idle []flowKey
	for key, h := range b.hosts {
		if h.Active == 0 {
			idle = append(idle, key)
		}
	}
	slices.SortFunc(idle, func(x, y flowKey) int { return b.hosts[x].Last.Compare(b.hosts[y].Last) })
	for _, key := range idle {
		if len(b.hosts) <= maxHostFlows {
			return
		}
		delete(b.hosts, key)
	}
}

// rows 给界面画的一份拷贝：最近还有连接的在上面；同一秒都开着的，后出现的域名在上面，
// 这样开着的那几行每秒刷新时不会来回换位置
func (b *flowBook) rows() (rows []hostFlow, version int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	rows = make([]hostFlow, 0, len(b.hosts))
	for _, h := range b.hosts {
		row := *h
		row.ids = slices.Clone(h.ids)
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(x, y hostFlow) int {
		return cmp.Or(y.Last.Compare(x.Last), y.First.Compare(x.First), strings.Compare(x.Host, y.Host), strings.Compare(x.Via, y.Via))
	})
	return rows, b.version
}

// clear 清掉记录。还开着的连接下一秒照常记一条，流量从这时候算起
func (b *flowBook) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hosts = map[flowKey]*hostFlow{}
	for id, o := range b.open {
		o.fresh = true
		b.open[id] = o
	}
	b.version++
}
