package engine

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// debugEventsEnabled reports whether structured event debugging is on
// (set DEBUG_EVENTS=1). Cached once at first use.
var debugEventsEnabled = sync.OnceValue(func() bool {
	return strings.TrimSpace(os.Getenv("DEBUG_EVENTS")) == "1"
})

// debugLogEvent prints one structured room-event line when debugging is
// enabled. Callers pass short tokens, e.g.:
//
//	debugLogEvent(room, "file_chunk_generated", "path", "public/styles.css", "bytes", 1024)
//
// Key/value pairs are printed in order; odd trailing values are printed
// as "key=<missing>".
func debugLogEvent(room *ProjectRoom, event string, kv ...any) {
	if !debugEventsEnabled() {
		return
	}
	var b strings.Builder
	b.WriteString("[room:")
	b.WriteString(room.ChatID())
	b.WriteString("] ev=")
	b.WriteString(event)
	for i := 0; i < len(kv); i += 2 {
		key, _ := kv[i].(string)
		b.WriteString(" ")
		b.WriteString(key)
		b.WriteString("=")
		if i+1 < len(kv) {
			b.WriteString(debugValue(kv[i+1]))
		} else {
			b.WriteString("<missing>")
		}
	}
	log.Print(b.String())
}

// debugValue renders a log value compactly, truncating long strings.
func debugValue(v any) string {
	const maxLen = 120
	switch t := v.(type) {
	case string:
		if len(t) > maxLen {
			return fmt.Sprintf("%q...(truncated)", t[:maxLen])
		}
		return fmt.Sprintf("%q", t)
	default:
		return fmt.Sprint(v)
	}
}

// atomic counter so concurrent rooms produce unique event sequence ids.
var debugEventSeq atomic.Int64

// debugNextSeq returns the next global event sequence number for logs.
func debugNextSeq() int64 { return debugEventSeq.Add(1) }
