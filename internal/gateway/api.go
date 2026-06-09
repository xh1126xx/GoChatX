package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

// ── Admin Endpoints ──

// AdminStats handles GET /api/admin/stats — returns system statistics.
func (s *GatewayServer) AdminStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]any{
		"ok":            true,
		"online_users":  len(GetOnlineUsers()),
		"online_list":   GetOnlineUsers(),
		"active_rooms":  GetRoomCount(),
		"room_list":     GetRoomIDs(),
	}
	jsonResponse(w, http.StatusOK, stats)
}

// AdminUsers handles GET /api/admin/users — lists all users from MySQL.
func (s *GatewayServer) AdminUsers(w http.ResponseWriter, r *http.Request) {
	if s.DB == nil {
		jsonResponse(w, 503, map[string]any{"ok": false, "msg": "database unavailable"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := s.DB.QueryContext(ctx, "SELECT id, username, role, created_at FROM users ORDER BY id")
	if err != nil {
		slog.Error("admin list users", "error", err)
		jsonResponse(w, 500, map[string]any{"ok": false, "msg": "database error"})
		return
	}
	defer rows.Close()

	var users []map[string]any
	for rows.Next() {
		var id int
		var username, role string
		var createdAt time.Time
		if err := rows.Scan(&id, &username, &role, &createdAt); err != nil {
			continue
		}
		users = append(users, map[string]any{
			"id":         id,
			"username":   username,
			"role":       role,
			"created_at": createdAt.Format(time.RFC3339),
			"is_online":  getOnlineClient(username) != nil,
		})
	}
	if users == nil {
		users = []map[string]any{}
	}
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "users": users})
}

// AdminBanUser handles POST /api/admin/users/ban — bans a user by setting role to "banned".
func (s *GatewayServer) AdminBanUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, 405, map[string]any{"ok": false, "msg": "method not allowed"})
		return
	}
	if s.DB == nil {
		jsonResponse(w, 503, map[string]any{"ok": false, "msg": "database unavailable"})
		return
	}

	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		jsonResponse(w, 400, map[string]any{"ok": false, "msg": "user_id required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	result, err := s.DB.ExecContext(ctx, "UPDATE users SET role='banned' WHERE id=? AND role != 'admin'", req.UserID)
	if err != nil {
		slog.Error("admin ban user", "error", err)
		jsonResponse(w, 500, map[string]any{"ok": false, "msg": "database error"})
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		jsonResponse(w, 400, map[string]any{"ok": false, "msg": "user not found or is admin"})
		return
	}

	// Disconnect banned user if online
	s.disconnectUser(req.UserID)
	jsonResponse(w, 200, map[string]any{"ok": true, "msg": "user banned"})
}

// AdminUnbanUser handles POST /api/admin/users/unban — restores a user's role to "user".
func (s *GatewayServer) AdminUnbanUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, 405, map[string]any{"ok": false, "msg": "method not allowed"})
		return
	}
	if s.DB == nil {
		jsonResponse(w, 503, map[string]any{"ok": false, "msg": "database unavailable"})
		return
	}

	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		jsonResponse(w, 400, map[string]any{"ok": false, "msg": "user_id required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	result, err := s.DB.ExecContext(ctx, "UPDATE users SET role='user' WHERE id=? AND role='banned'", req.UserID)
	if err != nil {
		slog.Error("admin unban user", "error", err)
		jsonResponse(w, 500, map[string]any{"ok": false, "msg": "database error"})
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		jsonResponse(w, 400, map[string]any{"ok": false, "msg": "user not found or not banned"})
		return
	}
	jsonResponse(w, 200, map[string]any{"ok": true, "msg": "user unbanned"})
}

// disconnectUser forcibly disconnects an online user.
func (s *GatewayServer) disconnectUser(userID string) {
	c := getOnlineClient(userID)
	if c != nil {
		// Close the WebSocket connection
		c.Conn.Close()
	}
}

// ── File Upload ──

const (
	maxUploadSize = 10 << 20 // 10MB
	uploadDir     = "./uploads"
)

// Upload handles POST /api/upload — uploads a file and returns its URL.
func (s *GatewayServer) Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, 405, map[string]any{"ok": false, "msg": "method not allowed"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		jsonResponse(w, 400, map[string]any{"ok": false, "msg": "file too large (max 10MB)"})
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		jsonResponse(w, 400, map[string]any{"ok": false, "msg": "file field required"})
		return
	}
	defer file.Close()

	// Validate file type
	ext := strings.ToLower(filepath.Ext(handler.Filename))
	allowed := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
		".pdf": true, ".doc": true, ".docx": true, ".txt": true, ".zip": true,
		".mp3": true, ".mp4": true, ".wav": true,
	}
	if !allowed[ext] {
		jsonResponse(w, 400, map[string]any{"ok": false, "msg": "file type not allowed"})
		return
	}

	// Create upload directory
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		slog.Error("create upload dir", "error", err)
		jsonResponse(w, 500, map[string]any{"ok": false, "msg": "server error"})
		return
	}

	// Generate unique filename
	filename := fmt.Sprintf("%d_%s%s", time.Now().UnixNano(), randomString(8), ext)
	dstPath := filepath.Join(uploadDir, filename)

	dst, err := os.Create(dstPath)
	if err != nil {
		slog.Error("create upload file", "error", err)
		jsonResponse(w, 500, map[string]any{"ok": false, "msg": "server error"})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		slog.Error("save upload file", "error", err)
		jsonResponse(w, 500, map[string]any{"ok": false, "msg": "server error"})
		return
	}

	fileURL := "/uploads/" + filename
	slog.Info("file uploaded", "filename", filename, "size", handler.Size)
	jsonResponse(w, 200, map[string]any{
		"ok":  true,
		"url": fileURL,
		"filename": handler.Filename,
		"size": handler.Size,
	})
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
		time.Sleep(1) // ensure different values
	}
	return string(b)
}
