package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	authpb "github.com/xh1126xx/gochatx/api"
)

// RESTHandler holds dependencies for REST API endpoints.
type RESTHandler struct {
	AuthClient authpb.AuthServiceClient
	Redis      *redis.Client
}

// NewRESTHandler creates a REST handler.
func NewRESTHandler(authConn *grpc.ClientConn, rdb *redis.Client) *RESTHandler {
	var authClient authpb.AuthServiceClient
	if authConn != nil {
		authClient = authpb.NewAuthServiceClient(authConn)
	}
	return &RESTHandler{AuthClient: authClient, Redis: rdb}
}

// jsonResponse writes a JSON response with the given status code.
func jsonResponse(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("jsonResponse encode failed", "error", err)
	}
}

// Register handles POST /api/register
func (h *RESTHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, 405, map[string]interface{}{"ok": false, "msg": "method not allowed"})
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		jsonResponse(w, 400, map[string]interface{}{"ok": false, "msg": "username and password required"})
		return
	}

	if h.AuthClient == nil {
		jsonResponse(w, 503, map[string]interface{}{"ok": false, "msg": "auth service unavailable"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	resp, err := h.AuthClient.Register(ctx, &authpb.RegisterRequest{Username: req.Username, Password: req.Password})
	if err != nil {
		jsonResponse(w, 500, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"ok": resp.Success, "msg": resp.Message})
}

// Login handles POST /api/login
func (h *RESTHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, 405, map[string]interface{}{"ok": false, "msg": "method not allowed"})
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		jsonResponse(w, 400, map[string]interface{}{"ok": false, "msg": "username and password required"})
		return
	}

	if h.AuthClient == nil {
		jsonResponse(w, 503, map[string]interface{}{"ok": false, "msg": "auth service unavailable"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	resp, err := h.AuthClient.Login(ctx, &authpb.LoginRequest{Username: req.Username, Password: req.Password})
	if err != nil {
		jsonResponse(w, 500, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"ok": resp.Success, "msg": resp.Message, "token": resp.Token})
}

// OnlineUsers handles GET /api/users/online
func (h *RESTHandler) OnlineUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, 405, map[string]interface{}{"ok": false, "msg": "method not allowed"})
		return
	}
	users := GetOnlineUsers()
	if users == nil {
		users = []string{}
	}
	jsonResponse(w, 200, map[string]interface{}{"ok": true, "users": users})
}

// Health handles GET /health — returns service health status.
func (s *GatewayServer) Health(w http.ResponseWriter, r *http.Request) {
	status := map[string]string{"status": "ok"}

	if s.Mongo != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.Mongo.Client.Ping(ctx, nil); err != nil {
			status["mongo"] = "disconnected"
			status["status"] = "degraded"
		} else {
			status["mongo"] = "connected"
		}
	} else {
		status["mongo"] = "disabled"
	}

	if s.Redis != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
		defer cancel()
		if err := s.Redis.Ping(ctx).Err(); err != nil {
			status["redis"] = "disconnected"
			status["status"] = "degraded"
		} else {
			status["redis"] = "connected"
		}
	} else {
		status["redis"] = "disabled"
	}

	if s.AuthClient != nil {
		status["authsvc"] = "configured"
	} else {
		status["authsvc"] = "disabled"
	}

	code := http.StatusOK
	if status["status"] != "ok" {
		code = http.StatusServiceUnavailable
	}
	jsonResponse(w, code, status)
}
