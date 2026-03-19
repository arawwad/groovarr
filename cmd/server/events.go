package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type serverEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	At      string `json:"at"`
}

type eventBroker struct {
	mu   sync.RWMutex
	subs map[chan serverEvent]struct{}
}

func newEventBroker() *eventBroker {
	return &eventBroker{
		subs: make(map[chan serverEvent]struct{}),
	}
}

func (b *eventBroker) Subscribe() chan serverEvent {
	ch := make(chan serverEvent, 32)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *eventBroker) Unsubscribe(ch chan serverEvent) {
	b.mu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

func (b *eventBroker) Publish(eventType, message string) {
	if strings.TrimSpace(message) == "" {
		return
	}

	ev := serverEvent{
		Type:    strings.TrimSpace(eventType),
		Message: strings.TrimSpace(message),
		At:      time.Now().UTC().Format(time.RFC3339),
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (s *Server) publishEvent(eventType, message string) {
	if s.events == nil {
		return
	}
	s.events.Publish(eventType, message)
}

func writeSSEEvent(w io.Writer, eventType string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if strings.TrimSpace(eventType) != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", strings.TrimSpace(eventType)); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", string(body))
	return err
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if s.events == nil {
		http.Error(w, "Events unavailable", http.StatusServiceUnavailable)
		return
	}

	ch := s.events.Subscribe()
	defer s.events.Unsubscribe(ch)

	_ = writeSSEEvent(w, "message", serverEvent{
		Type:    "system",
		Message: "Event stream connected.",
		At:      time.Now().UTC().Format(time.RFC3339),
	})
	flusher.Flush()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, "message", ev); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
