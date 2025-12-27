package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

import (
	appinfo "boltcache/appinfo"
	logger "boltcache/logger"
	swaggerui "boltcache/swaggerui"

	config "boltcache/config"
	cache "boltcache/internal/cache"
)

type RestServer struct {
	cache    *cache.BoltCache
	config   *config.Config
	upgrader websocket.Upgrader
}

type CacheRequest struct {
	Value string `json:"value"`
	TTL   string `json:"ttl,omitempty"`
}

type CacheResponse struct {
	Success bool        `json:"success"`
	Value   interface{} `json:"value,omitempty"`
	Error   string      `json:"error,omitempty"`
	Count   int         `json:"count,omitempty"`
}

func NewRestServer(cache *cache.BoltCache) *RestServer {
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

	var ttl time.Duration
	if req.TTL != "" {
		var err error
		ttl, err = time.ParseDuration(req.TTL)
		if err != nil {
			s.sendError(w, "Invalid TTL format", http.StatusBadRequest)
			return
		}
	}

	s.cache.Set(key, req.Value, ttl)
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
		logger.Log("WebSocket upgrade failed: %v", err)
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

	result := s.cache.LuaEngine.Execute(req.Script, req.Keys, req.Args)
	s.sendResponse(w, CacheResponse{Success: true, Value: result})
}

// Info endpoint
func (s *RestServer) info(w http.ResponseWriter, r *http.Request) {
	var count int
	s.cache.GetData().Range(func(k, v interface{}) bool {
		count++
		return true
	})

	info := map[string]interface{}{
		"keys":     count,
		"replicas": len(s.cache.GetReplicas()),
		"version":  appinfo.Version,
		"uptime":   time.Now().Format(time.RFC3339),
	}

	s.sendResponse(w, CacheResponse{Success: true, Value: info})
}

// Token management
func (s *RestServer) listTokens(w http.ResponseWriter, r *http.Request) {
	// Demo token list since auth is disabled
	tokens := map[string]interface{}{
		"dev-token-123": map[string]interface{}{
			"created_at":  "2024-01-01T00:00:00Z",
			"last_used":   "2024-01-01T12:00:00Z",
			"usage_count": 42,
		},
	}
	s.sendResponse(w, CacheResponse{Success: true, Value: tokens})
}

func (s *RestServer) createToken(w http.ResponseWriter, r *http.Request) {
	// Generate a demo token
	newToken := fmt.Sprintf("token-%d", time.Now().Unix())
	s.sendResponse(w, CacheResponse{Success: true, Value: map[string]string{"token": newToken}})
}

func (s *RestServer) deleteToken(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	token := vars["token"]
	s.sendResponse(w, CacheResponse{Success: true, Value: map[string]string{"message": fmt.Sprintf("Token %s deleted", token)}})
}

// Health check
func (s *RestServer) ping(w http.ResponseWriter, r *http.Request) {
	s.sendResponse(w, CacheResponse{Success: true, Value: "PONG"})
}

// Helper methods
func (s *RestServer) sendResponse(w http.ResponseWriter, resp CacheResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(resp)
}

func (s *RestServer) sendError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
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

	// CORS middleware 
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Token")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// Auth middleware (disabled)
	// r.Use(s.authManager.HTTPMiddleware)

	// Swagger UI
	if s.config.Server.SwaggerUI {
		r.HandleFunc("/openapi.json", swaggerui.OpenAPIHandler).Methods("GET")
		r.HandleFunc("/docs", swaggerui.SwaggerUIHandler).Methods("GET")
	}

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

	// Auth management
	r.HandleFunc("/auth/tokens", s.listTokens).Methods("GET")
	r.HandleFunc("/auth/tokens", s.createToken).Methods("POST")
	r.HandleFunc("/auth/tokens/{token}", s.deleteToken).Methods("DELETE")

	// Info and health check - BEFORE static files
	r.HandleFunc("/info", s.info).Methods("GET")
	r.HandleFunc("/ping", s.ping).Methods("GET")

	// Static HTML file
	r.HandleFunc("/rest-client.html", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./rest-client.html")
	}).Methods("GET")

	// Root endpoint - serve rest-client.html as index
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./rest-client.html")
	}).Methods("GET")

	addr := ":" + strconv.Itoa(port)
	logger.LogServerStart(addr)

	fmt.Println("\nAPI Documentation:")
	logger.LogRoute("PUT", "/cache/{key}", "Set value")
	logger.LogRoute("GET", "/cache/{key}", "Get value")
	logger.LogRoute("DELETE", "/cache/{key}", "Delete value")
	logger.LogRoute("POST", "/list/{key}", "Push to list")
	logger.LogRoute("DELETE", "/list/{key}", "Pop from list")
	logger.LogRoute("POST", "/set/{key}", "Add to set")
	logger.LogRoute("GET", "/set/{key}", "Get set members")
	logger.LogRoute("PUT", "/hash/{key}/{field}", "Set hash field")
	logger.LogRoute("GET", "/hash/{key}/{field}", "Get hash field")
	logger.LogRoute("GET", "/subscribe/{channel}", "Subscribe (WebSocket)")
	logger.LogRoute("POST", "/publish/{channel}", "Publish message")
	logger.LogRoute("POST", "/eval", "Execute script")
	logger.LogRoute("GET", "/auth/tokens", "List tokens")
	logger.LogRoute("POST", "/auth/tokens", "Create token")
	logger.LogRoute("DELETE", "/auth/tokens/{token}", "Delete token")
	logger.LogRoute("GET", "/info", "Server info")
	logger.LogRoute("GET", "/ping", "Health check")

	return http.ListenAndServe(addr, r)
}
