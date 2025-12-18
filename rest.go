package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type RestServer struct {
	cache    *BoltCache
	config   *Config
	upgrader websocket.Upgrader
}

type CacheRequest struct {
	Value string        `json:"value"`
	TTL   time.Duration `json:"ttl,omitempty"`
}

type CacheResponse struct {
	Success bool        `json:"success"`
	Value   interface{} `json:"value,omitempty"`
	Error   string      `json:"error,omitempty"`
	Count   int         `json:"count,omitempty"`
}

func NewRestServer(cache *BoltCache) *RestServer {
	return &RestServer{
		cache: cache,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// String operations
func (s *RestServer) setValue(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	var req CacheRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	s.cache.Set(key, req.Value, req.TTL)
	s.sendResponse(w, CacheResponse{Success: true})
}

func (s *RestServer) getValue(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	if value, ok := s.cache.Get(key); ok {
		s.sendResponse(w, CacheResponse{Success: true, Value: value})
	} else {
		s.sendError(w, "Key not found", http.StatusNotFound)
	}
}

func (s *RestServer) deleteValue(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	s.cache.Delete(key)
	s.sendResponse(w, CacheResponse{Success: true})
}

// List operations
func (s *RestServer) listPush(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	var values []string
	if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
		s.sendError(w, "Invalid JSON array", http.StatusBadRequest)
		return
	}

	count := s.cache.LPush(key, values...)
	s.sendResponse(w, CacheResponse{Success: true, Count: count})
}

func (s *RestServer) listPop(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	if value, ok := s.cache.LPop(key); ok {
		s.sendResponse(w, CacheResponse{Success: true, Value: value})
	} else {
		s.sendError(w, "List empty or not found", http.StatusNotFound)
	}
}

// Set operations
func (s *RestServer) setAdd(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	var members []string
	if err := json.NewDecoder(r.Body).Decode(&members); err != nil {
		s.sendError(w, "Invalid JSON array", http.StatusBadRequest)
		return
	}

	count := s.cache.SAdd(key, members...)
	s.sendResponse(w, CacheResponse{Success: true, Count: count})
}

func (s *RestServer) setMembers(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	members := s.cache.SMembers(key)
	s.sendResponse(w, CacheResponse{Success: true, Value: members})
}

// Hash operations
func (s *RestServer) hashSet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]
	field := vars["field"]

	var req CacheRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	s.cache.HSet(key, field, req.Value)
	s.sendResponse(w, CacheResponse{Success: true})
}

func (s *RestServer) hashGet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]
	field := vars["field"]

	if value, ok := s.cache.HGet(key, field); ok {
		s.sendResponse(w, CacheResponse{Success: true, Value: value})
	} else {
		s.sendError(w, "Field not found", http.StatusNotFound)
	}
}

// Pub/Sub via WebSocket
func (s *RestServer) subscribe(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	channel := vars["channel"]

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade failed:", err)
		return
	}
	defer conn.Close()

	// Create a mock net.Conn for compatibility
	s.cache.Subscribe(channel, &wsConn{conn: conn})

	// Keep connection alive
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (s *RestServer) publish(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	channel := vars["channel"]

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	count := s.cache.Publish(channel, req.Message)
	s.sendResponse(w, CacheResponse{Success: true, Count: count})
}

// Script execution
func (s *RestServer) evalScript(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Script string   `json:"script"`
		Keys   []string `json:"keys"`
		Args   []string `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	result := s.cache.luaEngine.Execute(req.Script, req.Keys, req.Args)
	s.sendResponse(w, CacheResponse{Success: true, Value: result})
}

// Info endpoint
func (s *RestServer) info(w http.ResponseWriter, r *http.Request) {
	var count int
	s.cache.data.Range(func(k, v interface{}) bool {
		count++
		return true
	})

	info := map[string]interface{}{
		"keys":     count,
		"replicas": len(s.cache.replicas),
		"version":  "1.0.0",
		"uptime":   time.Now().Format(time.RFC3339),
	}

	s.sendResponse(w, CacheResponse{Success: true, Value: info})
}

// Token management (disabled)
func (s *RestServer) listTokens(w http.ResponseWriter, r *http.Request) {
	s.sendResponse(w, CacheResponse{Success: true, Value: map[string]string{}})
}

func (s *RestServer) createToken(w http.ResponseWriter, r *http.Request) {
	s.sendResponse(w, CacheResponse{Success: true, Value: map[string]string{"token": "demo-token"}})
}

func (s *RestServer) deleteToken(w http.ResponseWriter, r *http.Request) {
	s.sendResponse(w, CacheResponse{Success: true})
}

// Health check
func (s *RestServer) ping(w http.ResponseWriter, r *http.Request) {
	s.sendResponse(w, CacheResponse{Success: true, Value: "PONG"})
}

// Helper methods
func (s *RestServer) sendResponse(w http.ResponseWriter, resp CacheResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *RestServer) sendError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(CacheResponse{Success: false, Error: message})
}

// WebSocket wrapper for net.Conn compatibility
type wsConn struct {
	conn *websocket.Conn
}

func (w *wsConn) Write(p []byte) (n int, err error) {
	err = w.conn.WriteMessage(websocket.TextMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *wsConn) Read(p []byte) (n int, err error) {
	_, data, err := w.conn.ReadMessage()
	if err != nil {
		return 0, err
	}
	copy(p, data)
	return len(data), nil
}

func (w *wsConn) Close() error                       { return w.conn.Close() }
func (w *wsConn) LocalAddr() net.Addr                { return w.conn.LocalAddr() }
func (w *wsConn) RemoteAddr() net.Addr               { return w.conn.RemoteAddr() }
func (w *wsConn) SetDeadline(t time.Time) error      { return nil }
func (w *wsConn) SetReadDeadline(t time.Time) error  { return nil }
func (w *wsConn) SetWriteDeadline(t time.Time) error { return nil }

func (s *RestServer) Start() error {
	port := s.config.Server.REST.Port
	r := mux.NewRouter()

	// String operations
	r.HandleFunc("/cache/{key}", s.setValue).Methods("PUT")
	r.HandleFunc("/cache/{key}", s.getValue).Methods("GET")
	r.HandleFunc("/cache/{key}", s.deleteValue).Methods("DELETE")

	// List operations
	r.HandleFunc("/list/{key}", s.listPush).Methods("POST")
	r.HandleFunc("/list/{key}", s.listPop).Methods("DELETE")

	// Set operations
	r.HandleFunc("/set/{key}", s.setAdd).Methods("POST")
	r.HandleFunc("/set/{key}", s.setMembers).Methods("GET")

	// Hash operations
	r.HandleFunc("/hash/{key}/{field}", s.hashSet).Methods("PUT")
	r.HandleFunc("/hash/{key}/{field}", s.hashGet).Methods("GET")

	// Pub/Sub
	r.HandleFunc("/subscribe/{channel}", s.subscribe).Methods("GET")
	r.HandleFunc("/publish/{channel}", s.publish).Methods("POST")

	// Script execution
	r.HandleFunc("/eval", s.evalScript).Methods("POST")

	// Auth management (disabled)
	// r.HandleFunc("/auth/tokens", s.listTokens).Methods("GET")
	// r.HandleFunc("/auth/tokens", s.createToken).Methods("POST")
	// r.HandleFunc("/auth/tokens/{token}", s.deleteToken).Methods("DELETE")

	// Info
	r.HandleFunc("/info", s.info).Methods("GET")
	r.HandleFunc("/ping", s.ping).Methods("GET")

	// Auth middleware (disabled)
	// r.Use(s.authManager.HTTPMiddleware)

	// CORS middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Token")
			if r.Method == "OPTIONS" {
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	addr := ":" + strconv.Itoa(port)
	fmt.Printf("BoltCache REST API started on %s\n", addr)
	fmt.Println("API Documentation:")
	fmt.Println("  PUT    /cache/{key}           - Set value")
	fmt.Println("  GET    /cache/{key}           - Get value")
	fmt.Println("  DELETE /cache/{key}           - Delete key")
	fmt.Println("  POST   /list/{key}            - Push to list")
	fmt.Println("  DELETE /list/{key}            - Pop from list")
	fmt.Println("  POST   /set/{key}             - Add to set")
	fmt.Println("  GET    /set/{key}             - Get set members")
	fmt.Println("  PUT    /hash/{key}/{field}    - Set hash field")
	fmt.Println("  GET    /hash/{key}/{field}    - Get hash field")
	fmt.Println("  GET    /subscribe/{channel}   - Subscribe (WebSocket)")
	fmt.Println("  POST   /publish/{channel}     - Publish message")
	fmt.Println("  POST   /eval                  - Execute script")
	// fmt.Println("  GET    /auth/tokens           - List tokens")
	// fmt.Println("  POST   /auth/tokens           - Create token")
	// fmt.Println("  DELETE /auth/tokens/{token}   - Delete token")
	fmt.Println("  GET    /info                  - Server info")
	fmt.Println("  GET    /ping                  - Health check")

	return http.ListenAndServe(addr, r)
}