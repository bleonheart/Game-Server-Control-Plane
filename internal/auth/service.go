package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/example/game-server-control-plane/internal/database"
	"github.com/example/game-server-control-plane/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrInvalidSession = errors.New("invalid session")

type Service struct {
	db  *pgxpool.Pool
	ttl time.Duration
}

func NewService(db *pgxpool.Pool, ttl time.Duration) *Service {
	return &Service{db: db, ttl: ttl}
}

func (s *Service) CreateUser(ctx context.Context, email, password string, roles []model.Role) (model.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || strings.ToLower(address.Address) != email {
		return model.User{}, fmt.Errorf("invalid email")
	}
	if len(password) < 12 {
		return model.User{}, fmt.Errorf("password must be at least 12 characters")
	}
	if len(roles) == 0 {
		roles = []model.Role{model.RoleViewer}
	}
	for _, role := range roles {
		if role != model.RoleAdmin && role != model.RoleOperator && role != model.RoleViewer {
			return model.User{}, fmt.Errorf("invalid role %q", role)
		}
	}
	hash, err := HashPassword(password)
	if err != nil {
		return model.User{}, err
	}
	id, err := database.NewID("usr")
	if err != nil {
		return model.User{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback(ctx)
	var user model.User
	if err := tx.QueryRow(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,$3) RETURNING id,email,password_hash,created_at,updated_at`, id, email, hash).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return model.User{}, err
	}
	for _, role := range roles {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id,role_name) VALUES($1,$2)`, id, role); err != nil {
			return model.User{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, err
	}
	user.Roles = roles
	return user, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (string, model.Principal, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var id, storedEmail, hash string
	var disabled bool
	if err := s.db.QueryRow(ctx, `SELECT id,email,password_hash,disabled FROM users WHERE email=$1`, email).Scan(&id, &storedEmail, &hash, &disabled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", model.Principal{}, ErrInvalidCredentials
		}
		return "", model.Principal{}, err
	}
	if disabled || !CheckPassword(hash, password) {
		return "", model.Principal{}, ErrInvalidCredentials
	}
	roles, err := s.roles(ctx, id)
	if err != nil {
		return "", model.Principal{}, err
	}
	token, tokenHash, err := newSessionToken()
	if err != nil {
		return "", model.Principal{}, err
	}
	sessionID, err := database.NewID("ses")
	if err != nil {
		return "", model.Principal{}, err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,expires_at) VALUES($1,$2,$3,$4)`, sessionID, id, tokenHash[:], time.Now().UTC().Add(s.ttl)); err != nil {
		return "", model.Principal{}, err
	}
	return token, model.Principal{UserID: id, Email: storedEmail, Roles: roles}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (model.Principal, error) {
	hash := sha256.Sum256([]byte(token))
	var principal model.Principal
	if err := s.db.QueryRow(ctx, `SELECT u.id,u.email FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at > now() AND u.disabled=false`, hash[:]).Scan(&principal.UserID, &principal.Email); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Principal{}, ErrInvalidSession
		}
		return model.Principal{}, err
	}
	roles, err := s.roles(ctx, principal.UserID)
	if err != nil {
		return model.Principal{}, err
	}
	principal.Roles = roles
	_, _ = s.db.Exec(ctx, `UPDATE sessions SET last_seen_at=now() WHERE token_hash=$1`, hash[:])
	return principal, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, hash[:])
	return err
}

func (s *Service) CleanupExpired(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	return err
}

func (s *Service) roles(ctx context.Context, userID string) ([]model.Role, error) {
	rows, err := s.db.Query(ctx, `SELECT role_name FROM user_roles WHERE user_id=$1 ORDER BY role_name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := make([]model.Role, 0, 3)
	for rows.Next() {
		var role model.Role
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (s *Service) SeedAdmin(ctx context.Context, email, password string) error {
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, strings.ToLower(strings.TrimSpace(email))).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err := s.CreateUser(ctx, email, password, []model.Role{model.RoleAdmin})
	return err
}

func newSessionToken() (string, [32]byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", [32]byte{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	return token, sha256.Sum256([]byte(token)), nil
}

func (s *Service) ListUsers(ctx context.Context, limit, offset int) ([]model.User, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(ctx, `SELECT id,email,password_hash,created_at,updated_at FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []model.User
	for rows.Next() {
		var user model.User
		if err := rows.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, err
		}
		roles, err := s.roles(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		user.Roles = roles
		user.PasswordHash = ""
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Service) SetRoles(ctx context.Context, userID string, roles []model.Role) error {
	if len(roles) == 0 {
		return fmt.Errorf("at least one role is required")
	}
	for _, role := range roles {
		if role != model.RoleAdmin && role != model.RoleOperator && role != model.RoleViewer {
			return fmt.Errorf("invalid role %q", role)
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1`, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
	}
	for _, role := range roles {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id,role_name) VALUES($1,$2)`, userID, role); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
