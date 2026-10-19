package main

import "sync"

// Hub wakes tool calls that are blocked in wait_for_user when a page
// reports completion. Persistence lives in the Store; this is only the
// in-memory wakeup signal.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: map[string]map[chan struct{}]struct{}{}}
}

// Subscribe returns a channel that is closed when Notify(pageID) is called,
// plus a cancel func the caller must invoke when done waiting.
func (h *Hub) Subscribe(pageID string) (<-chan struct{}, func()) {
	ch := make(chan struct{})
	h.mu.Lock()
	if h.subs[pageID] == nil {
		h.subs[pageID] = map[chan struct{}]struct{}{}
	}
	h.subs[pageID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[pageID], ch)
		h.mu.Unlock()
	}
}

func (h *Hub) Notify(pageID string) {
	h.mu.Lock()
	for ch := range h.subs[pageID] {
		close(ch)
	}
	delete(h.subs, pageID)
	h.mu.Unlock()
}
