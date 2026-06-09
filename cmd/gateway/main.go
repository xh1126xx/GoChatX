package main

import (
	"context"
	"database/sql"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spf13/viper"
	"github.com/xh1126xx/gochatx/internal/gateway"
	"github.com/xh1126xx/gochatx/internal/storage"
)

func loadConfig(path string) {
	viper.SetDefault("authsvc.addr", "localhost:50051")
	viper.SetDefault("mongo.uri", "mongodb://127.0.0.1:27017")
	viper.SetDefault("mongo.db", "gochatx")
	viper.SetDefault("redis.addr", "127.0.0.1:6379")
	viper.SetDefault("gateway.listen", ":8080")
	viper.SetDefault("gateway.cors", "*")

	viper.SetEnvPrefix("GOCHATX")
	viper.AutomaticEnv()

	if path != "" {
		viper.SetConfigFile(path)
		if err := viper.ReadInConfig(); err != nil {
			slog.Warn("config file not found, using defaults", "error", err)
		}
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg := flag.String("config", "config.yaml", "config file path (optional)")
	flag.Parse()

	loadConfig(*cfg)

	// JWT secret for admin middleware
	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(jwtSecret) == 0 {
		jwtSecret = []byte("supersecretkey") // dev fallback
		slog.Warn("JWT_SECRET not set, using dev default")
	}

	// connect to auth service
	authAddr := viper.GetString("authsvc.addr")
	var authConn *grpc.ClientConn
	if authAddr != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, err := grpc.DialContext(ctx, authAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		cancel()
		if err != nil {
			slog.Warn("can't connect to auth service, falling back to token-as-userID mode",
				"addr", authAddr, "error", err)
			authConn = nil
		} else {
			authConn = conn
			defer conn.Close()
			slog.Info("connected to auth service", "addr", authAddr)
		}
	}

	// connect to MySQL (for admin endpoints)
	var db *sql.DB
	dbDSN := os.Getenv("DB_DSN")
	if dbDSN != "" {
		var err error
		db, err = sql.Open("mysql", dbDSN)
		if err != nil {
			slog.Warn("can't open mysql", "error", err)
			db = nil
		} else {
			db.SetMaxOpenConns(10)
			db.SetMaxIdleConns(3)
			db.SetConnMaxLifetime(5 * time.Minute)
			pctx, pcancel := context.WithTimeout(context.Background(), 3*time.Second)
			if err := db.PingContext(pctx); err != nil {
				slog.Warn("mysql ping failed, admin user management disabled", "error", err)
				db = nil
			}
			pcancel()
			if db != nil {
				defer db.Close()
				slog.Info("connected to MySQL")
			}
		}
	}

	// connect to MongoDB
	var mongoStore *storage.MangoStore
	mongoURI := viper.GetString("mongo.uri")
	mongoDB := viper.GetString("mongo.db")
	if mongoURI != "" {
		var err error
		mongoStore, err = storage.NewMangoStore(mongoURI, mongoDB)
		if err != nil {
			slog.Warn("can't connect to mongo, message persistence disabled", "error", err)
			mongoStore = nil
		} else {
			defer mongoStore.Close()
			slog.Info("connected to MongoDB", "uri", mongoURI, "db", mongoDB)
		}
	}

	// connect to Redis
	var rdb *redis.Client
	redisAddr := viper.GetString("redis.addr")
	if redisAddr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
		pctx, pcancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := rdb.Ping(pctx).Err(); err != nil {
			slog.Warn("redis ping failed, continuing without redis", "error", err)
			rdb = nil
		}
		pcancel()
		if rdb != nil {
			defer rdb.Close()
			slog.Info("connected to Redis", "addr", redisAddr)
		}
	}

	// gateway server
	gw := gateway.NewGatewayServer(authConn, mongoStore, rdb)
	gw.DB = db
	gw.JWTSecret = jwtSecret
	rest := gateway.NewRESTHandler(authConn, rdb)

	// Rate limiter
	rl := gateway.NewRateLimiter(rdb)

	// CORS
	corsOrigins := viper.GetString("gateway.cors")
	corsMiddleware := gateway.CORSMiddleware(corsOrigins)

	// Admin middleware
	adminMW := gateway.AdminMiddleware(jwtSecret)

	mux := http.NewServeMux()

	// Public routes
	mux.HandleFunc("/ws", gw.HandleWS)
	mux.HandleFunc("/health", gw.Health)
	mux.Handle("/api/login", rl.Limit(gateway.LoginKey, 10, time.Minute)(http.HandlerFunc(rest.Login)))
	mux.Handle("/api/register", rl.Limit(gateway.LoginKey, 5, time.Minute)(http.HandlerFunc(rest.Register)))
	mux.Handle("/api/users/online", rl.Limit(gateway.IPKey, 30, time.Minute)(http.HandlerFunc(rest.OnlineUsers)))

	// File upload (authenticated)
	mux.Handle("/api/upload", gateway.AuthMiddleware(jwtSecret)(http.HandlerFunc(gw.Upload)))
	// Uploaded files served as static
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads"))))

	// Admin routes (require admin role)
	mux.Handle("/api/admin/stats", adminMW(http.HandlerFunc(gw.AdminStats)))
	mux.Handle("/api/admin/users", adminMW(http.HandlerFunc(gw.AdminUsers)))
	mux.Handle("/api/admin/users/ban", adminMW(http.HandlerFunc(gw.AdminBanUser)))
	mux.Handle("/api/admin/users/unban", adminMW(http.HandlerFunc(gw.AdminUnbanUser)))

	// Static files
	mux.Handle("/", http.FileServer(http.Dir("./web")))

	handler := corsMiddleware(mux)

	addr := viper.GetString("gateway.listen")
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		slog.Info("shutting down gateway...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("gateway shutdown error", "error", err)
		}
	}()

	slog.Info("gateway started", "addr", addr, "cors", corsOrigins)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("gateway failed", "error", err)
		os.Exit(1)
	}
	slog.Info("gateway stopped")
}
