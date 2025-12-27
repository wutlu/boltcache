package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

const baseURL = "http://localhost:8090"

func doReq(t *testing.T, method, url string, body any) *http.Response {
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

	if resp.StatusCode >= 400 {
		t.Fatalf("%s %s failed with status %d", method, url, resp.StatusCode)
	}

	return resp
}

func TestRESTAPI(t *testing.T) {

	t.Run("1. Health Check", func(t *testing.T) {
		resp := doReq(t, http.MethodGet, baseURL+"/ping", nil)
		resp.Body.Close()
	})

	t.Run("2. Set Value", func(t *testing.T) {
		doReq(t, http.MethodPut, baseURL+"/cache/user:1", map[string]any{
			"value": "John Doe",
		}).Body.Close()
	})

	t.Run("3. Get Value", func(t *testing.T) {
		doReq(t, http.MethodGet, baseURL+"/cache/user:1", nil).Body.Close()
	})

	t.Run("4. Set with TTL", func(t *testing.T) {
		doReq(t, http.MethodPut, baseURL+"/cache/session:abc", map[string]any{
			"value": "active",
			"ttl":   "5m",
		}).Body.Close()
	})

	t.Run("5. List Push", func(t *testing.T) {
		doReq(t, http.MethodPost, baseURL+"/list/mylist",
			[]string{"item1", "item2", "item3"},
		).Body.Close()
	})

	t.Run("6. List Pop", func(t *testing.T) {
		doReq(t, http.MethodDelete, baseURL+"/list/mylist", nil).Body.Close()
	})

	t.Run("7. Set Add", func(t *testing.T) {
		doReq(t, http.MethodPost, baseURL+"/set/myset",
			[]string{"member1", "member2", "member3"},
		).Body.Close()
	})

	t.Run("8. Set Members", func(t *testing.T) {
		doReq(t, http.MethodGet, baseURL+"/set/myset", nil).Body.Close()
	})

	t.Run("9. Hash Set", func(t *testing.T) {
		doReq(t, http.MethodPut, baseURL+"/hash/user:1/name", map[string]any{
			"value": "John",
		}).Body.Close()

		doReq(t, http.MethodPut, baseURL+"/hash/user:1/age", map[string]any{
			"value": "30",
		}).Body.Close()
	})

	t.Run("10. Hash Get", func(t *testing.T) {
		doReq(t, http.MethodGet, baseURL+"/hash/user:1/name", nil).Body.Close()
	})

	t.Run("11. Publish Message", func(t *testing.T) {
		doReq(t, http.MethodPost, baseURL+"/publish/notifications", map[string]any{
			"message": "Hello World!",
		}).Body.Close()
	})

	t.Run("12. Lua Script", func(t *testing.T) {
		doReq(t, http.MethodPost, baseURL+"/eval", map[string]any{
			"script": "redis.call(\"SET\", KEYS[1], ARGV[1])",
			"keys":   []string{"scriptkey"},
			"args":   []string{"scriptvalue"},
		}).Body.Close()
	})

	t.Run("13. Server Info", func(t *testing.T) {
		doReq(t, http.MethodGet, baseURL+"/info", nil).Body.Close()
	})

	t.Run("14. Delete Key", func(t *testing.T) {
		doReq(t, http.MethodDelete, baseURL+"/cache/user:1", nil).Body.Close()
	})
}
