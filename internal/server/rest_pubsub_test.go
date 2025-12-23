package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

const pubsubBaseURL = "http://localhost:8090"

func pubReq(t *testing.T, method, url string, body any) {
	t.Helper()

	var buf *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json marshal error: %v", err)
		}
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}

	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		t.Fatalf("request create error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		t.Fatalf("%s %s failed with status %d", method, url, resp.StatusCode)
	}
}

func TestRESTPubSub(t *testing.T) {

	t.Run("1. Publish to empty channel", func(t *testing.T) {
		pubReq(
			t,
			http.MethodPost,
			pubsubBaseURL+"/publish/test-channel",
			map[string]any{
				"message": "Hello World!",
			},
		)
	})

	t.Run("2. Multiple publishes", func(t *testing.T) {
		for i := 1; i <= 3; i++ {
			pubReq(
				t,
				http.MethodPost,
				pubsubBaseURL+"/publish/notifications",
				map[string]any{
					"message": "Message " + string(rune('0'+i)) + " from test",
				},
			)
			time.Sleep(300 * time.Millisecond)
		}
	})

	t.Run("3. Different channels", func(t *testing.T) {
		pubReq(
			t,
			http.MethodPost,
			pubsubBaseURL+"/publish/user-events",
			map[string]any{
				"message": "User logged in",
			},
		)

		pubReq(
			t,
			http.MethodPost,
			pubsubBaseURL+"/publish/system-alerts",
			map[string]any{
				"message": "System maintenance in 5 minutes",
			},
		)
	})
}
