package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsAudioMuseNoPathError(t *testing.T) {
	if !isAudioMuseNoPathError(assertError("No path found between the selected songs within 25 steps.")) {
		t.Fatal("expected no-path error to be recognized")
	}
	if isAudioMuseNoPathError(assertError("AudioMuse returned 500")) {
		t.Fatal("unexpected generic error match")
	}
}

func TestHandleListenSongPathReturnsEmptyPayloadForNoPath(t *testing.T) {
	audioMuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/find_path" {
			t.Fatalf("path = %s, want /api/find_path", r.URL.Path)
		}
		http.Error(w, `{"error":"No path found between the selected songs within 25 steps."}`, http.StatusBadGateway)
	}))
	defer audioMuse.Close()

	t.Setenv("AUDIOMUSE_URL", audioMuse.URL)
	server := &Server{}
	body := bytes.NewBufferString(`{"startTrackId":"start-1","endTrackId":"end-1","maxSteps":25,"keepExactSize":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/listen/path", body)
	rr := httptest.NewRecorder()

	server.handleListenSongPath(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result := payload["listenSongPath"]
	if result["pathLength"] != float64(0) {
		t.Fatalf("pathLength = %#v", result["pathLength"])
	}
	if result["message"] != "No path found between the selected songs within 25 steps." {
		t.Fatalf("message = %#v", result["message"])
	}
}

func assertError(message string) error {
	return &staticError{text: message}
}

type staticError struct {
	text string
}

func (e *staticError) Error() string {
	return e.text
}
