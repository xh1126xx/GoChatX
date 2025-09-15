package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var secret = []byte("GoChatX-secret")

type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

func Sign(userID string, ttl time.Duration) (string, error) {
	tokens := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	return tokens.SignedString(secret)

}

func Parse(tokenStr string) (string, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	})
	if err != nil {
		return "", err
	}
	if c, ok := token.Claims.(*Claims); ok && token.Valid {
		return c.UserID, nil
	}
	return "", jwt.ErrTokenInvalidClaims
}
