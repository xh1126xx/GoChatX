package main

import (
	"database/sql"
	"log"
	"net"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	pb "github.com/xh1126xx/gochatx/api"
	"github.com/xh1126xx/gochatx/internal/auth"
)

func main() {
	db, _ := sql.Open("mysql", "root:123456@tcp(127.0.0.1:3306)/gochatx?parseTime=true")
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	s := grpc.NewServer()
	pb.RegisterAuthServiceServer(s, &auth.AuthService{DB: db, Redis: rdb, JWTSecret: []byte("supersecretkey")})
	lis, _ := net.Listen("tcp", ":50051")
	log.Println("Auth service listening on :50051")
	s.Serve(lis)
}
