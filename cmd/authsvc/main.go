package main

import (
	"context"
	"database/sql"
	"log"
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
		log.Fatalf("failed to open database: %v", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}

	// auto-migrate: create users table if not exists
	migrate(ctx, db)

	log.Println("database connected")
	return db
}

func migrate(ctx context.Context, db *sql.DB) {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (
		id INT AUTO_INCREMENT PRIMARY KEY,
		username VARCHAR(64) NOT NULL UNIQUE,
		password VARCHAR(256) NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		log.Fatalf("failed to migrate users table: %v", err)
	}
}

func initRedis(addr string) *redis.Client {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}
	log.Println("redis connected")
	return rdb
}

func main() {
	dsn := getEnvOrDefault("DB_DSN", "root:123456@tcp(127.0.0.1:3306)/gochatx?parseTime=true")
	redisAddr := getEnvOrDefault("REDIS_ADDR", "127.0.0.1:6379")
	listenAddr := getEnvOrDefault("LISTEN_ADDR", ":50051")
	jwtSecret := getEnvOrDefault("JWT_SECRET", "supersecretkey")

	db := initDB(dsn)
	defer db.Close()

	rdb := initRedis(redisAddr)
	defer rdb.Close()

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", listenAddr, err)
	}

	s := grpc.NewServer()
	pb.RegisterAuthServiceServer(s, &auth.AuthService{
		DB:        db,
		Redis:     rdb,
		JWTSecret: []byte(jwtSecret),
	})

	// graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("shutting down auth service...")
		s.GracefulStop()
	}()

	log.Println("Auth service listening on", listenAddr)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
