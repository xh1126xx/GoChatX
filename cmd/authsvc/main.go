package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	pb "github.com/xh1126xx/gochatx/api"
	"github.com/xh1126xx/gochatx/internal/auth"
)

func initDB(dsn string) *sql.DB {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		slog.Error("failed to ping database", "error", err)
		os.Exit(1)
	}

	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer migrateCancel()
	migrate(migrateCtx, db)

	slog.Info("database connected")
	return db
}

func migrate(ctx context.Context, db *sql.DB) {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (
		id INT AUTO_INCREMENT PRIMARY KEY,
		username VARCHAR(64) NOT NULL UNIQUE,
		password VARCHAR(256) NOT NULL,
		role VARCHAR(16) NOT NULL DEFAULT 'user',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		INDEX idx_username (username)
	)`)
	if err != nil {
		slog.Error("failed to migrate users table", "error", err)
		os.Exit(1)
	}
}

func initRedis(addr string) *redis.Client {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	slog.Info("redis connected")
	return rdb
}

func main() {
	// Structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		slog.Error("DB_DSN environment variable is required")
		os.Exit(1)
	}
	redisAddr := getEnvOrDefault("REDIS_ADDR", "127.0.0.1:6379")
	listenAddr := getEnvOrDefault("LISTEN_ADDR", ":50051")
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		slog.Error("JWT_SECRET environment variable is required")
		os.Exit(1)
	}

	db := initDB(dsn)
	defer db.Close()

	rdb := initRedis(redisAddr)
	defer rdb.Close()

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		slog.Error("failed to listen", "addr", listenAddr, "error", err)
		os.Exit(1)
	}

	s := grpc.NewServer()
	pb.RegisterAuthServiceServer(s, &auth.AuthService{
		DB:        db,
		Redis:     rdb,
		JWTSecret: []byte(jwtSecret),
	})

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		slog.Info("shutting down auth service...")
		s.GracefulStop()
	}()

	slog.Info("auth service started", "addr", listenAddr)
	if err := s.Serve(lis); err != nil {
		slog.Error("failed to serve", "error", err)
		os.Exit(1)
	}
	slog.Info("auth service stopped")
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
