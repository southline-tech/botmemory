package mcp

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics tracks tool call counts (no content, keys optional in logs only as slugs).
type Metrics struct {
	Calls   atomic.Uint64
	Errors  atomic.Uint64
	byTool  sync.Map // string -> *atomic.Uint64
	byBot   sync.Map // string -> *atomic.Uint64
	errCode sync.Map // string -> *atomic.Uint64
}

var globalMetrics Metrics

func counter(m *sync.Map, key string) *atomic.Uint64 {
	if v, ok := m.Load(key); ok {
		return v.(*atomic.Uint64)
	}
	c := &atomic.Uint64{}
	actual, _ := m.LoadOrStore(key, c)
	return actual.(*atomic.Uint64)
}

// RecordCall logs one line and updates counters.
func RecordCall(bot, tool, project, key string, ms int64, ok bool, code string) {
	globalMetrics.Calls.Add(1)
	counter(&globalMetrics.byTool, tool).Add(1)
	counter(&globalMetrics.byBot, bot).Add(1)
	if !ok {
		globalMetrics.Errors.Add(1)
		if code != "" {
			counter(&globalMetrics.errCode, code).Add(1)
		}
	}
	okFlag := 0
	if ok {
		okFlag = 1
	}
	line := fmt.Sprintf("ts=%s bot=%s tool=%s project=%s key=%s ms=%d ok=%d",
		time.Now().UTC().Format(time.RFC3339Nano), bot, tool, project, key, ms, okFlag)
	if !ok && code != "" {
		line += " code=" + code
	}
	log.Print(line)
}

// Snapshot returns aggregate counters (no paths or secrets).
func Snapshot() map[string]any {
	out := map[string]any{
		"calls":  globalMetrics.Calls.Load(),
		"errors": globalMetrics.Errors.Load(),
	}
	out["by_tool"] = mapSnap(&globalMetrics.byTool)
	out["by_bot"] = mapSnap(&globalMetrics.byBot)
	out["errors"] = mapSnap(&globalMetrics.errCode)
	return out
}

func mapSnap(m *sync.Map) map[string]uint64 {
	out := map[string]uint64{}
	m.Range(func(k, v any) bool {
		out[k.(string)] = v.(*atomic.Uint64).Load()
		return true
	})
	return out
}
