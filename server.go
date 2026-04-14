package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed web/dist
var distFS embed.FS

// ── API response types ────────────────────────────────────────────────────────

type TraceSummary struct {
	TraceID     string  `json:"traceId"`
	RootService string  `json:"rootService"`
	RootName    string  `json:"rootName"`
	StartMs     float64 `json:"startMs"`
	DurationMs  float64 `json:"durationMs"`
	SpanCount   int     `json:"spanCount"`
	ErrorCount  int     `json:"errorCount"`
}

// SpanView adds a computed depth field to DisplaySpan for the waterfall UI.
type SpanView struct {
	DisplaySpan
	Depth int `json:"depth"`
}

type TraceDetailResponse struct {
	TraceSummary
	Spans []SpanView `json:"spans"`
}

type LogPage struct {
	Total  int          `json:"total"`
	Offset int          `json:"offset"`
	Limit  int          `json:"limit"`
	Items  []DisplayLog `json:"items"`
}

type MetaResponse struct {
	File        string `json:"file"`
	TraceCount  int    `json:"traceCount"`
	LogCount    int    `json:"logCount"`
	MetricCount int    `json:"metricCount"`
}

// ── server ────────────────────────────────────────────────────────────────────

type apiServer struct {
	data        *DisplayData
	traceIdx    map[string]int
	logsBySpan  map[string][]int
	logsByTrace map[string][]int
}

func newAPIServer(data *DisplayData) *apiServer {
	s := &apiServer{
		data:        data,
		traceIdx:    make(map[string]int),
		logsBySpan:  make(map[string][]int),
		logsByTrace: make(map[string][]int),
	}
	for i, t := range data.Traces {
		s.traceIdx[t.TraceID] = i
	}

	for i, l := range data.Logs {
		if l.SpanID != "" {
			s.logsBySpan[l.SpanID] = append(s.logsBySpan[l.SpanID], i)
		}

		if l.TraceID != "" {
			s.logsByTrace[l.TraceID] = append(s.logsByTrace[l.TraceID], i)
		}
	}

	return s
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode: %v", err)
	}
}

func (s *apiServer) handleMeta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, MetaResponse{
		File:        s.data.Meta.File,
		TraceCount:  len(s.data.Traces),
		LogCount:    len(s.data.Logs),
		MetricCount: len(s.data.Metrics),
	})
}

func traceSummary(t DisplayTrace) TraceSummary {
	sum := TraceSummary{
		TraceID:    t.TraceID,
		RootName:   t.RootName,
		StartMs:    t.StartMs,
		DurationMs: t.DurationMs,
		SpanCount:  len(t.Spans),
	}

	spanIDs := make(map[string]bool, len(t.Spans))

	for _, sp := range t.Spans {
		spanIDs[sp.SpanID] = true
	}

	for _, sp := range t.Spans {
		if sp.HasError {
			sum.ErrorCount++
		}

		if sum.RootService == "" && (sp.ParentSpanID == "" || !spanIDs[sp.ParentSpanID]) {
			sum.RootService = sp.Service
		}
	}

	if sum.RootService == "" && len(t.Spans) > 0 {
		sum.RootService = t.Spans[0].Service
	}

	return sum
}

func (s *apiServer) handleTraces(w http.ResponseWriter, _ *http.Request) {
	out := make([]TraceSummary, len(s.data.Traces))

	for i, t := range s.data.Traces {
		out[i] = traceSummary(t)
	}

	writeJSON(w, out)
}

// computeDepths returns the tree depth for each span (0 = root).
func computeDepths(spans []DisplaySpan) []int {
	spanIdx := make(map[string]int, len(spans))

	for i, sp := range spans {
		spanIdx[sp.SpanID] = i
	}

	depths := make([]int, len(spans))

	for i := range depths {
		depths[i] = -1
	}

	var getDepth func(i int) int

	getDepth = func(i int) int {
		if depths[i] >= 0 {
			return depths[i]
		}

		sp := spans[i]
		if sp.ParentSpanID == "" {
			depths[i] = 0
			return 0
		}

		pidx, ok := spanIdx[sp.ParentSpanID]
		if !ok {
			depths[i] = 0
			return 0
		}

		depths[i] = getDepth(pidx) + 1

		return depths[i]
	}

	for i := range spans {
		getDepth(i)
	}

	return depths
}

func (s *apiServer) handleTraceDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	idx, ok := s.traceIdx[id]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)

		return
	}

	t := s.data.Traces[idx]
	depths := computeDepths(t.Spans)
	spanViews := make([]SpanView, len(t.Spans))

	for i, sp := range t.Spans {
		spanViews[i] = SpanView{DisplaySpan: sp, Depth: depths[i]}
	}

	writeJSON(w, TraceDetailResponse{
		TraceSummary: traceSummary(t),
		Spans:        spanViews,
	})
}

func normSev(s string) string {
	switch strings.ToUpper(s) {
	case "TRACE", "TRACE2", "TRACE3", "TRACE4":
		return "TRACE"
	case "DEBUG", "DEBUG2", "DEBUG3", "DEBUG4":
		return "DEBUG"
	case "INFO", "INFO2", "INFO3", "INFO4":
		return "INFO"
	case "WARN", "WARNING", "WARN2", "WARN3", "WARN4":
		return "WARN"
	case "ERROR", "ERROR2", "ERROR3", "ERROR4":
		return "ERROR"
	case "FATAL", "FATAL2", "FATAL3", "FATAL4":
		return "FATAL"
	}

	return s
}

type logFilters struct {
	offset    int
	limit     int
	traceID   string
	spanID    string
	search    string
	sevFilter string
}

func (s *apiServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	filters := parseLogFilters(r.URL.Query())
	candidates := s.logCandidates(filters)
	filtered := s.filterLogCandidates(candidates, filters)

	writeJSON(w, LogPage{
		Total:  len(filtered),
		Offset: filters.offset,
		Limit:  filters.limit,
		Items:  s.paginateLogs(filtered, filters),
	})
}

func parseLogFilters(q url.Values) logFilters {
	filters := logFilters{
		limit:     200,
		traceID:   q.Get("traceId"),
		spanID:    q.Get("spanId"),
		search:    strings.ToLower(q.Get("q")),
		sevFilter: q.Get("sev"),
	}

	if offsetStr := q.Get("offset"); offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil {
			filters.offset = v
		}
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 1000 {
			filters.limit = v
		}
	}

	return filters
}

func (s *apiServer) logCandidates(filters logFilters) []int {
	switch {
	case filters.spanID != "" && filters.search == "" && filters.sevFilter == "" && filters.traceID == "":
		return s.logsBySpan[filters.spanID]
	case filters.traceID != "" && filters.spanID == "" && filters.search == "" && filters.sevFilter == "":
		return s.logsByTrace[filters.traceID]
	default:
		candidates := make([]int, len(s.data.Logs))

		for i := range s.data.Logs {
			candidates[i] = i
		}

		return candidates
	}
}

func (s *apiServer) filterLogCandidates(candidates []int, filters logFilters) []int {
	filtered := make([]int, 0, len(candidates))

	for _, i := range candidates {
		l := s.data.Logs[i]

		if filters.spanID != "" && l.SpanID != filters.spanID {
			continue
		}

		if filters.traceID != "" && l.TraceID != filters.traceID {
			continue
		}

		if filters.sevFilter != "" && normSev(l.SeverityText) != filters.sevFilter {
			continue
		}

		if filters.search != "" && !strings.Contains(strings.ToLower(l.Body), filters.search) {
			continue
		}

		filtered = append(filtered, i)
	}

	return filtered
}

func (s *apiServer) paginateLogs(filtered []int, filters logFilters) []DisplayLog {
	if filters.offset >= len(filtered) {
		return []DisplayLog{}
	}

	end := filters.offset + filters.limit
	if end > len(filtered) {
		end = len(filtered)
	}

	items := make([]DisplayLog, 0, end-filters.offset)

	for _, i := range filtered[filters.offset:end] {
		items = append(items, s.data.Logs[i])
	}

	return items
}

func (s *apiServer) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.data.Metrics)
}

func serve(data *DisplayData) error {
	sub, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		return fmt.Errorf("web/dist embed: %w", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	addr := fmt.Sprintf("http://localhost:%d", ln.Addr().(*net.TCPAddr).Port)

	srv := newAPIServer(data)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/meta", srv.handleMeta)
	mux.HandleFunc("GET /api/traces", srv.handleTraces)
	mux.HandleFunc("GET /api/traces/{id}", srv.handleTraceDetail)
	mux.HandleFunc("GET /api/logs", srv.handleLogs)
	mux.HandleFunc("GET /api/metrics", srv.handleMetrics)

	// Static file server with SPA fallback.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		f, err := sub.Open(path)
		if err != nil {
			// Fall back to index.html for client-side routing.
			f2, err2 := sub.Open("index.html")
			if err2 != nil {
				http.NotFound(w, r)
				return
			}
			defer f2.Close()

			st, _ := f2.Stat()
			http.ServeContent(w, r, "index.html", st.ModTime(), f2.(io.ReadSeeker))

			return
		}
		defer f.Close()

		st, _ := f.Stat()
		if st.IsDir() {
			f2, err2 := sub.Open(path + "/index.html")
			if err2 != nil {
				http.NotFound(w, r)
				return
			}
			defer f2.Close()

			st2, _ := f2.Stat()
			http.ServeContent(w, r, "index.html", st2.ModTime(), f2.(io.ReadSeeker))

			return
		}

		http.ServeContent(w, r, path, st.ModTime(), f.(io.ReadSeeker))
	})

	fmt.Fprintf(os.Stderr, "🌐 Serving at %s\n", addr)
	openBrowser(addr)

	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)

	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(quit)

	<-quit
	fmt.Fprintln(os.Stderr, "\nBye 👋")

	return nil
}

func openBrowser(target string) {
	var err error

	switch runtime.GOOS {
	case "darwin":
		err = startDarwinBrowser(target)
	case "linux":
		err = startLinuxBrowser(target)
	case "windows":
		err = startWindowsBrowser(target)
	default:
		fmt.Fprintf(os.Stderr, "Open your browser at %s\n", target)

		return
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Open your browser at %s\n", target)
	}
}

func startDarwinBrowser(target string) error {
	//nolint:gosec // The target is the locally constructed listener URL.
	cmd := exec.Command("open")
	cmd.Args = append(cmd.Args, target)

	return cmd.Start()
}

func startLinuxBrowser(target string) error {
	//nolint:gosec // The target is the locally constructed listener URL.
	cmd := exec.Command("xdg-open")
	cmd.Args = append(cmd.Args, target)

	return cmd.Start()
}

func startWindowsBrowser(target string) error {
	//nolint:gosec // The target is the locally constructed listener URL.
	cmd := exec.Command("rundll32")
	cmd.Args = append(cmd.Args, "url.dll,FileProtocolHandler", target)

	return cmd.Start()
}
