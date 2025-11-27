package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"chat-backend/internal/db"
	"chat-backend/internal/models"
	"chat-backend/internal/utils"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgconn"
	"golang.org/x/crypto/bcrypt"
)

type UserService struct{}

func NewUserService() *UserService {
	return &UserService{}
}

// ErrUserExists is returned when attempting to register with an existing username
var ErrUserExists = errors.New("username already exists")

func (s *UserService) Register(ctx context.Context, req models.RegisterRequest) (*models.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		// Try to detect Postgres unique-violation errors in several ways:
		// 1) If the error is a pgconn.PgError with code 23505
		// 2) Fallback: check the error string for SQLSTATE 23505 or unique constraint name
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return nil, ErrUserExists
			}
		}

		// Fallback string checks for environments where the PgError isn't exposed
		msg := err.Error()
		if strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value") || strings.Contains(msg, "users_username_key") {
			return nil, ErrUserExists
		}

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

	refresh, err := GenerateRefreshToken(user.ID, user.Username)
	if err != nil {
		return nil, err
	}

	return &models.AuthResponse{
		AccessToken:  token,
		RefreshToken: refresh,
		Username:     user.Username,
		UserID:       user.ID,
	}, nil
}

func GenerateJWT(userID int, username string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"exp":      time.Now().Add(time.Hour * 1).Unix(),
		"typ":      "access",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(utils.GetEnv("JWT_SECRET", "secret")))
}

// GenerateRefreshToken creates a refresh JWT with longer expiry and typ claim
func GenerateRefreshToken(userID int, username string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"exp":      time.Now().Add(time.Hour * 24 * 30).Unix(), // 30 days
		"typ":      "refresh",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(utils.GetEnv("JWT_SECRET", "secret")))
}

// ValidateRefreshToken parses and validates a refresh token and returns claims
func ValidateRefreshToken(tokenString string) (jwt.MapClaims, error) {
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
		// Ensure token type is refresh
		if typ, ok := claims["typ"].(string); !ok || typ != "refresh" {
			return nil, errors.New("invalid token type")
		}
		return claims, nil
	}

	return nil, errors.New("invalid token")
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
