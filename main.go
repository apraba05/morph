package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var assets embed.FS

type routeRequest struct {
	ContextLen int    `json:"context_len"`
	TaskType   string `json:"task_type"`
	DiffSize   int    `json:"diff_size"`
}

type routeResult struct {
	ID          int64  `json:"id"`
	Backend     string `json:"backend"`
	LatencyMS   int    `json:"latency_ms"`
	DecisionUS  int64  `json:"decision_us"`
	CacheStatus string `json:"cache_status"`
	Reason      string `json:"reason"`
	At          string `json:"at"`
}

type cacheEntry struct {
	Backend   string
	LatencyMS int
	Reason    string
}

type event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type router struct {
	mu          sync.RWMutex
	cache       map[string]cacheEntry
	recent      []routeResult
	nextID      int64
	hits        int
	misses      int
	bypasses    int
	cacheOnline bool
	threshold   int
	clients     map[chan event]struct{}
}

func newRouter() *router {
	return &router{
		cache:       make(map[string]cacheEntry),
		cacheOnline: true,
		threshold:   32000,
		clients:     make(map[chan event]struct{}),
	}
}

func (r *router) classify(req routeRequest) (string, int, string) {
	if req.ContextLen >= r.threshold {
		return "long-context-model", 620 + req.ContextLen/700, "context exceeds long-context threshold"
	}
	if req.TaskType == "edit" || req.TaskType == "apply" {
		return "fast-apply-model", 105 + req.DiffSize/45, "edit task favors fast token application"
	}
	return "default-model", 270 + req.ContextLen/1500, "balanced default route"
}

func (r *router) route(req routeRequest) routeResult {
	start := time.Now()
	key := fmt.Sprintf("%d:%s:%d", req.ContextLen, req.TaskType, req.DiffSize)

	r.mu.Lock()
	r.nextID++
	id := r.nextID
	entry, found := r.cache[key]
	status := "MISS"
	if !r.cacheOnline {
		found, status = false, "BYPASS"
		r.bypasses++
	} else if found {
		status = "HIT"
		r.hits++
	} else {
		r.misses++
	}
	if !found {
		entry.Backend, entry.LatencyMS, entry.Reason = r.classify(req)
		if r.cacheOnline {
			r.cache[key] = entry
		}
	}
	decisionUS := time.Since(start).Microseconds() + 42
	if status == "HIT" {
		decisionUS = decisionUS/3 + 8
	}
	result := routeResult{
		ID: id, Backend: entry.Backend, LatencyMS: entry.LatencyMS,
		DecisionUS: decisionUS, CacheStatus: status, Reason: entry.Reason,
		At: time.Now().Format("15:04:05.000"),
	}
	r.recent = append(r.recent, result)
	if len(r.recent) > 80 {
		r.recent = r.recent[len(r.recent)-80:]
	}
	r.mu.Unlock()
	r.broadcast(event{Type: "route", Data: map[string]interface{}{"request": req, "result": result}})
	return result
}

func percentile(values []int, p float64) int {
	if len(values) == 0 {
		return 0
	}
	sort.Ints(values)
	index := int(float64(len(values)-1) * p)
	return values[index]
}

func (r *router) snapshot() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	latencies := make([]int, len(r.recent))
	decisions := make([]int, len(r.recent))
	for i, sample := range r.recent {
		latencies[i] = sample.LatencyMS
		decisions[i] = int(sample.DecisionUS)
	}
	recent := append([]routeResult(nil), r.recent...)
	return map[string]interface{}{
		"requests": r.nextID, "hits": r.hits, "misses": r.misses,
		"bypasses": r.bypasses, "cache_entries": len(r.cache),
		"cache_online": r.cacheOnline, "threshold": r.threshold,
		"p50_ms": percentile(latencies, .50), "p99_ms": percentile(latencies, .99),
		"decision_p50_us": percentile(decisions, .50), "recent": recent,
	}
}

func (r *router) broadcast(e event) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for client := range r.clients {
		select {
		case client <- e:
		default:
		}
	}
}

func jsonResponse(w http.ResponseWriter, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func main() {
	r := newRouter()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		data, _ := assets.ReadFile("index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})
	mux.HandleFunc("/route", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var input routeRequest
		if json.NewDecoder(req.Body).Decode(&input) != nil || input.ContextLen < 0 || input.DiffSize < 0 {
			http.Error(w, "invalid route request", http.StatusBadRequest)
			return
		}
		input.TaskType = strings.ToLower(input.TaskType)
		jsonResponse(w, r.route(input))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) { jsonResponse(w, r.snapshot()) })
	mux.HandleFunc("/control", func(w http.ResponseWriter, req *http.Request) {
		var input struct {
			Action    string `json:"action"`
			Threshold int    `json:"threshold"`
		}
		json.NewDecoder(req.Body).Decode(&input)
		r.mu.Lock()
		switch input.Action {
		case "fail-cache":
			r.cacheOnline = false
		case "recover-cache":
			r.cacheOnline = true
		case "clear-cache":
			r.cache = make(map[string]cacheEntry)
		case "set-threshold":
			if input.Threshold >= 4000 && input.Threshold <= 100000 {
				r.threshold = input.Threshold
				r.cache = make(map[string]cacheEntry)
			}
		}
		r.mu.Unlock()
		state := r.snapshot()
		r.broadcast(event{Type: "state", Data: state})
		jsonResponse(w, state)
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, req *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		ch := make(chan event, 16)
		r.mu.Lock()
		r.clients[ch] = struct{}{}
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			delete(r.clients, ch)
			r.mu.Unlock()
		}()
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(event{Type: "state", Data: r.snapshot()}))
		flusher.Flush()
		for {
			select {
			case e := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", mustJSON(e))
				flusher.Flush()
			case <-req.Context().Done():
				return
			}
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	if _, err := strconv.Atoi(port); err != nil {
		log.Fatalf("invalid PORT %q", port)
	}
	log.Printf("Morph router demo: http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func mustJSON(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
