package main

import (
	"embed"
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed assets
var assets embed.FS

var (
	pageIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	titleRe  = regexp.MustCompile(`(?is)<title[^>]*>\s*(.*?)\s*</title>`)
)

const (
	defaultWait = 10 * time.Minute
	maxWait     = 24 * time.Hour
	maxBody     = 10 << 20
)

// isLoopbackHost reports whether a Host authority names this machine.
func isLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	return false
}

// hostCheck rejects requests whose Host header is not a loopback name.
// Combined with binding to 127.0.0.1 this defeats DNS-rebinding attacks.
func hostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireHeader guards endpoints with side effects against cross-site
// requests: a custom header forces a CORS preflight, which no cross-origin
// page passes. curl and the page's own script simply send the header.
func requireHeader(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Fui") == "" {
			http.Error(w, `missing X-Fui header (send -H "X-Fui: 1")`, http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// viewers counts live /events streams per page, i.e. open browser tabs.
// PUT /api/pages uses it to decide whether to open the browser or let the
// already-open tabs reload themselves.
type viewers struct {
	mu sync.Mutex
	n  map[string]int
}

func (v *viewers) add(id string) {
	v.mu.Lock()
	v.n[id]++
	v.mu.Unlock()
}

func (v *viewers) remove(id string) {
	v.mu.Lock()
	if v.n[id]--; v.n[id] <= 0 {
		delete(v.n, id)
	}
	v.mu.Unlock()
}

func (v *viewers) count(id string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.n[id]
}

func NewWebHandler(store *Store, hub *Hub, baseURL string) *http.ServeMux {
	mux := http.NewServeMux()
	open := &viewers{n: map[string]int{}}

	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}

	pageID := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id := r.PathValue("id")
		if !pageIDRe.MatchString(id) {
			http.Error(w, "invalid page id: use letters, digits, dot, underscore, dash", http.StatusBadRequest)
			return "", false
		}
		return id, true
	}

	static := func(path, file, ctype string) {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			b, _ := assets.ReadFile("assets/" + file)
			w.Header().Set("Content-Type", ctype)
			w.Write(b)
		})
	}
	static("/{$}", "overview.html", "text/html; charset=utf-8")
	static("/sharedLocalStorage.js", "sharedLocalStorage.js", "text/javascript; charset=utf-8")
	static("/style.css", "style.css", "text/css; charset=utf-8")
	// The agent-facing contract. An agent that fetches this once knows the
	// whole API; no client registration needed.
	static("/SKILL.md", "SKILL.md", "text/markdown; charset=utf-8")

	mux.HandleFunc("GET /p/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pageID(w, r)
		if !ok {
			return
		}
		html, found, err := store.GetHTML(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "page not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, html)
	})

	mux.HandleFunc("GET /api/pages", func(w http.ResponseWriter, r *http.Request) {
		pages, err := store.ListPages()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, pages)
	})

	// Publish a page: the request body is the HTML document. Tabs already
	// showing this id reload themselves (via the /events stream); the browser
	// is opened only when nobody is viewing, unless ?open=1 or ?open=0 says
	// otherwise. ?title= overrides the <title> tag for the overview.
	mux.HandleFunc("PUT /api/pages/{id}", requireHeader(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pageID(w, r)
		if !ok {
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		html := string(body)
		if strings.TrimSpace(html) == "" {
			http.Error(w, "empty body: send the HTML document as the request body", http.StatusBadRequest)
			return
		}
		title := r.URL.Query().Get("title")
		if title == "" {
			if m := titleRe.FindStringSubmatch(html); m != nil {
				// The captured text is markup: &amp; must not reach the
				// overview, which renders titles with textContent.
				title = htmlpkg.UnescapeString(m[1])
			}
		}
		if err := store.UpsertPage(id, title, html); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		viewing := open.count(id)
		hub.Notify(id) // open tabs see the new updated_at and reload
		url := fmt.Sprintf("%s/p/%s", baseURL, id)
		opened := false
		switch strings.ToLower(r.URL.Query().Get("open")) {
		case "0", "false", "no":
		case "1", "true", "yes":
			opened = openBrowser(url) == nil
		default:
			if viewing == 0 {
				opened = openBrowser(url) == nil
			}
		}
		writeJSON(w, map[string]any{
			"page_id": id,
			"url":     url,
			"opened":  opened,
			"viewers": viewing,
		})
	}))

	mux.HandleFunc("DELETE /api/pages/{id}", requireHeader(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pageID(w, r)
		if !ok {
			return
		}
		if err := store.DeletePage(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"deleted": true})
	}))

	// Full state map, or only ?keys=a,b,c.
	mux.HandleFunc("GET /api/state/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pageID(w, r)
		if !ok {
			return
		}
		state, err := store.GetState(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if keys := r.URL.Query().Get("keys"); keys != "" {
			filtered := map[string]string{}
			for _, k := range strings.Split(keys, ",") {
				k = strings.TrimSpace(k)
				if v, ok := state[k]; ok {
					filtered[k] = v
				}
			}
			state = filtered
		}
		writeJSON(w, state)
	})

	// Upsert one key: {"key": "...", "value": "..."}; value null deletes.
	// The page's localStorage shim and the agent (progress dashboards,
	// subagent blackboards) both write through here.
	mux.HandleFunc("POST /api/state/{id}", requireHeader(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pageID(w, r)
		if !ok {
			return
		}
		var body struct {
			Key   string  `json:"key"`
			Value *string `json:"value"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&body); err != nil || body.Key == "" {
			http.Error(w, `expected JSON body {"key": "...", "value": "..."}`, http.StatusBadRequest)
			return
		}
		if err := store.SetState(id, body.Key, body.Value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		hub.Notify(id)
		writeJSON(w, map[string]bool{"ok": true})
	}))

	mux.HandleFunc("DELETE /api/state/{id}", requireHeader(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pageID(w, r)
		if !ok {
			return
		}
		if err := store.ClearState(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		hub.Notify(id)
		writeJSON(w, map[string]bool{"ok": true})
	}))

	mux.HandleFunc("POST /api/done/{id}", requireHeader(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pageID(w, r)
		if !ok {
			return
		}
		if err := store.MarkDone(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		hub.Notify(id)
		writeJSON(w, map[string]bool{"ok": true})
	}))

	// Long-poll until the page calls done(), or ?timeout= seconds elapse.
	// The done signal is persisted and consumed here, so a click that lands
	// between two waits is picked up by the next one. The caller owns the
	// timeout: pick one under your HTTP client's limit and simply call again.
	mux.HandleFunc("GET /api/wait/{id}", requireHeader(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pageID(w, r)
		if !ok {
			return
		}
		if _, found, err := store.PageUpdatedAt(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if !found {
			http.Error(w, "page not found", http.StatusNotFound)
			return
		}
		timeout := defaultWait
		if s := r.URL.Query().Get("timeout"); s != "" {
			secs, err := strconv.ParseFloat(s, 64)
			if err != nil || secs <= 0 {
				http.Error(w, "timeout must be a positive number of seconds", http.StatusBadRequest)
				return
			}
			timeout = time.Duration(secs * float64(time.Second))
		}
		if timeout > maxWait {
			timeout = maxWait
		}

		deadline := time.NewTimer(timeout)
		defer deadline.Stop()
		for {
			// Subscribe before checking, so a click between check and wait
			// cannot be missed.
			wake, cancel := hub.Subscribe(id)
			done, err := store.ConsumeDone(id)
			if err != nil {
				cancel()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if done {
				cancel()
				state, err := store.GetState(id)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				writeJSON(w, map[string]any{"done": true, "page_id": id, "state": state})
				return
			}
			select {
			case <-wake:
				cancel()
			case <-deadline.C:
				cancel()
				state, _ := store.GetState(id)
				writeJSON(w, map[string]any{
					"done":    false,
					"page_id": id,
					"reason":  "timeout: no done() click yet; state is persisted, call again to keep waiting",
					"state":   state,
				})
				return
			case <-r.Context().Done():
				cancel()
				return
			}
		}
	}))

	// Push-only feed for sharedLocalStorage.js. Every hub wakeup for this
	// page sends the full state map as one SSE data frame, or a "reload"
	// event when the page's HTML itself was republished. Server-Sent Events
	// are a plain HTTP GET, so the browser's own CORS rules cover it: the
	// server sends no Access-Control-Allow-Origin, so a cross-origin page's
	// EventSource can never read the stream.
	mux.HandleFunc("GET /events/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pageID(w, r)
		if !ok {
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		open.add(id)
		defer open.remove(id)
		lastPublished, _, _ := store.PageUpdatedAt(id)

		send := func() error {
			if t, found, err := store.PageUpdatedAt(id); err == nil && found && t != lastPublished {
				lastPublished = t
				if _, err := io.WriteString(w, "event: reload\ndata: {}\n\n"); err != nil {
					return err
				}
				flusher.Flush()
				return nil
			}
			state, err := store.GetState(id)
			if err != nil {
				return err
			}
			b, err := json.Marshal(state)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}

		// One frame up front closes the race between the page's initial
		// synchronous state fetch and this stream being established.
		if err := send(); err != nil {
			return
		}
		for {
			wake, cancel := hub.Subscribe(id)
			select {
			case <-wake:
				cancel()
				if err := send(); err != nil {
					return
				}
			case <-r.Context().Done():
				cancel()
				return
			}
		}
	})

	return mux
}
