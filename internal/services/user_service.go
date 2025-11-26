package services

import (
	"context"
	"errors"
	"time"

	"chat-backend/internal/db"
	"chat-backend/internal/models"
	"chat-backend/internal/utils"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type UserService struct{}

func NewUserService() *UserService {
	return &UserService{}
}

func (s *UserService) Register(ctx context.Context, req models.RegisterRequest) (*models.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	var user models.User
	query := `INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id, username, created_at`
	err = db.Pool.QueryRow(ctx, query, req.Username, string(hash)).Scan(&user.ID, &user.Username, &user.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (s *UserService) Login(ctx context.Context, req models.LoginRequest) (*models.AuthResponse, error) {
	var user models.User
	query := `SELECT id, username, password_hash FROM users WHERE username = $1`
	err := db.Pool.QueryRow(ctx, query, req.Username).Scan(&user.ID, &user.Username, &user.PasswordHash)
	if err != nil {
		return nil, errors.New("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, errors.New("invalid credentials")
	}

	token, err := GenerateJWT(user.ID, user.Username)
	if err != nil {
		return nil, err
	}

	return &models.AuthResponse{
		Token:    token,
		Username: user.Username,
		UserID:   user.ID,
	}, nil
}

func GenerateJWT(userID int, username string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"exp":      time.Now().Add(time.Hour * 72).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(utils.GetEnv("JWT_SECRET", "secret")))
}

func ValidateToken(tokenString string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(utils.GetEnv("JWT_SECRET", "secret")), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}
