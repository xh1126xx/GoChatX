package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	Role   string `json:"role"`
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
	if len(req.Username) < 2 || len(req.Username) > 32 {
		return &pb.RegisterResponse{Success: false, Message: "username must be 2-32 characters"}, nil
	}
	if len(req.Password) < 6 || len(req.Password) > 72 {
		return &pb.RegisterResponse{Success: false, Message: "password must be 6-72 characters"}, nil
	}

	hashed, err := hashPassword(req.Password)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to hash password")
	}

	_, err = s.DB.ExecContext(ctx, "INSERT INTO users (username, password, role) VALUES (?, ?, 'user')", req.Username, hashed)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return &pb.RegisterResponse{Success: false, Message: "username already exists"}, nil
		}
		return nil, status.Errorf(codes.Internal, "database error: %v", err)
	}
	return &pb.RegisterResponse{Success: true, Message: "Registration successful"}, nil
}

func (s *AuthService) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	if req.Username == "" || req.Password == "" {
		return &pb.LoginResponse{Success: false, Message: "username and password required"}, nil
	}

	row := s.DB.QueryRowContext(ctx, "SELECT id, password, role FROM users WHERE username=?", req.Username)
	var id, pw, role string
	if err := row.Scan(&id, &pw, &role); err != nil {
		return &pb.LoginResponse{Success: false, Message: "user not found"}, nil
	}
	if !checkPassword(pw, req.Password) {
		return &pb.LoginResponse{Success: false, Message: "incorrect password"}, nil
	}

	claims := Claims{
		UserID: id,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "gochatx-auth",
			Subject:   id,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err := t.SignedString(s.JWTSecret)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to sign token")
	}

	if err := s.Redis.Set(ctx, "user:"+id+":online", "true", 24*time.Hour).Err(); err != nil {
		slog.Warn("failed to set online status", "user_id", id, "error", err)
	}
	return &pb.LoginResponse{Success: true, Message: "Login successful", Token: token}, nil
}

func (s *AuthService) Validate(ctx context.Context, req *pb.TokenRequest) (*pb.TokenResponse, error) {
	t, err := jwt.ParseWithClaims(req.Token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
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

// ParseToken parses a JWT token and returns claims. Used by gateway for role checks.
func (s *AuthService) ParseToken(tokenStr string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.JWTSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := t.Claims.(*Claims); ok && t.Valid {
		return claims, nil
	}
	return nil, fmt.Errorf("invalid token")
}
