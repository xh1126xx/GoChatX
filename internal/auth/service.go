package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	pb "github.com/xh1126xx/gochatx/api"
)

type AuthService struct {
	pb.UnimplementedAuthServiceServer
	DB        *sql.DB
	Redis     *redis.Client
	JWTSecret []byte
}

type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

func hashPassword(password string) string {
	h := sha256.Sum256([]byte(password))
	return fmt.Sprintf("%x", h[:])
}

func (s *AuthService) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	_, err := s.DB.Exec("INSERT INTO users (username, password) VALUES (?, ?)", req.Username, hashPassword(req.Password))
	if err != nil {
		return &pb.RegisterResponse{Success: false, Message: err.Error()}, nil
	}
	return &pb.RegisterResponse{Success: true, Message: "Registration successful"}, nil
}

func (s *AuthService) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	row := s.DB.QueryRow("SELECT id, password FROM users WHERE username=?", req.Username)
	var id, pw string
	if err := row.Scan(&id, &pw); err != nil {
		return &pb.LoginResponse{Success: false, Message: "user not found"}, nil
	}
	if pw != hashPassword(req.Password) {
		return &pb.LoginResponse{Success: false, Message: "incorrect password"}, nil
	}

	claims := Claims{
		UserID: id,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, _ := t.SignedString(s.JWTSecret)

	s.Redis.Set(ctx, "user:"+id+":online", "true", 24*time.Hour)
	return &pb.LoginResponse{Success: true, Message: "Login successful", Token: token}, nil
}

func (s *AuthService) Validate(ctx context.Context, req *pb.TokenRequest) (*pb.TokenResponse, error) {
	t, err := jwt.ParseWithClaims(req.Token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return s.JWTSecret, nil
	})
	if err != nil {
		return &pb.TokenResponse{Valid: false}, nil
	}
	if claims, ok := t.Claims.(*Claims); ok && t.Valid {
		return &pb.TokenResponse{Valid: true, UserId: claims.UserID}, nil
	}
	return &pb.TokenResponse{Valid: false}, nil
}
