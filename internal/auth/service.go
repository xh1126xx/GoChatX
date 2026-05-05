package auth

import (
	"context"
	"database/sql"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

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

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func checkPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func (s *AuthService) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	hashed, err := hashPassword(req.Password)
	if err != nil {
		return &pb.RegisterResponse{Success: false, Message: "internal error"}, nil
	}

	_, err = s.DB.ExecContext(ctx, "INSERT INTO users (username, password) VALUES (?, ?)", req.Username, hashed)
	if err != nil {
		return &pb.RegisterResponse{Success: false, Message: "username already exists"}, nil
	}
	return &pb.RegisterResponse{Success: true, Message: "Registration successful"}, nil
}

func (s *AuthService) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	row := s.DB.QueryRowContext(ctx, "SELECT id, password FROM users WHERE username=?", req.Username)
	var id, pw string
	if err := row.Scan(&id, &pw); err != nil {
		return &pb.LoginResponse{Success: false, Message: "user not found"}, nil
	}
	if !checkPassword(pw, req.Password) {
		return &pb.LoginResponse{Success: false, Message: "incorrect password"}, nil
	}

	claims := Claims{
		UserID: id,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err := t.SignedString(s.JWTSecret)
	if err != nil {
		return &pb.LoginResponse{Success: false, Message: "internal error"}, nil
	}

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
