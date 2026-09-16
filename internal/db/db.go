package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type User struct {
	ID                   int64
	FName                string
	SName                string
	Role                 string
	Login                string
	PasswordHash         []byte
	PasswordSalt         []byte
	LoginKeySalt         []byte
	EncryptedMasterKey   []byte
	IdentityPublicKey    []byte
	EncryptedIdentityKey []byte

	Claimed       bool
	ClaimCodeHash []byte

	CreatedAt int64
}

var ErrLoginAlreadyExists = errors.New("login already exists")

func HexEncodeRandomBytes(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func CreateUser(
	db *sql.DB,
	login, fname, sname, role string,
	passwordHash, passwordSalt, loginKeySalt, encryptedMasterKey []byte,
	identityPublicKey, encryptedIdentityKey []byte,
) error {
	_, err := db.Exec(`
		INSERT INTO users (
			login, fname, sname,role , password_hash, password_salt,
			login_key_salt, encrypted_master_key,
			identity_public_key, encrypted_identity_key, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		login, fname, sname, role, passwordHash, passwordSalt,
		loginKeySalt, encryptedMasterKey,
		identityPublicKey, encryptedIdentityKey,
		time.Now().Unix(),
	)

	if err != nil {
		var sqliteErr *sqlite.Error

		if errors.As(err, &sqliteErr) &&
			sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return ErrLoginAlreadyExists
		}

		return err
	}

	return nil
}
func CreateClaimableUser(
	db *sql.DB,
	login string,
	fname string,
	sname string,
	role string,
	claimed bool,
	claimCodeHash []byte,
) error {
	_, err := db.Exec(`
		INSERT INTO users (
			login,
			fname,
			sname,
			role,
			claimed,
			claim_code_hash,
			created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		login,
		fname,
		sname,
		role,
		claimed,
		claimCodeHash,
		time.Now().Unix(),
	)

	if err != nil {
		var sqliteErr *sqlite.Error

		if errors.As(err, &sqliteErr) &&
			sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return ErrLoginAlreadyExists
		}

		return err
	}

	return nil
}
func GetUserByLogin(db *sql.DB, login string) (*User, error) {
	row := db.QueryRow(`
		SELECT
			id,
			login,
			fname,
			sname,
			role,
			password_hash,
			password_salt,
			login_key_salt,
			encrypted_master_key,
			identity_public_key,
			encrypted_identity_key,
			claimed,
			claim_code_hash,
			created_at
		FROM users
		WHERE login = ?
	`, login)

	var user User

	err := row.Scan(
		&user.ID,
		&user.Login,
		&user.FName,
		&user.SName,
		&user.Role,
		&user.PasswordHash,
		&user.PasswordSalt,
		&user.LoginKeySalt,
		&user.EncryptedMasterKey,
		&user.IdentityPublicKey,
		&user.EncryptedIdentityKey,
		&user.Claimed,
		&user.ClaimCodeHash,

		&user.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &user, nil
}
func CreateSession(db *sql.DB, userID int64, peerID string, ttl time.Duration) (sessionID string, err error) {
	sessionID, err = HexEncodeRandomBytes(32)
	if err != nil {
		return "", err
	}
	createdAt := time.Now().Unix()
	expiresAt := createdAt + int64(ttl.Seconds())

	_, err = db.Exec(`
		INSERT INTO sessions (id, user_id, peer_id, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)
	`, sessionID, userID, peerID, createdAt, expiresAt)
	if err != nil {
		return "", err
	}
	return sessionID, nil
}
func DeleteSession(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE id = ?`, sessionID)
	if err != nil {
		return err
	}
	return nil
}
func GetAllUser(db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(context.Background(), `
	SELECT
			id,
			login,
			fname,
			sname,
			role,
			password_hash,
			password_salt,
			login_key_salt,
			encrypted_master_key,
			identity_public_key,
			encrypted_identity_key,
			claimed,
			claim_code_hash,
			created_at
		FROM users
	`)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Login, &u.FName, &u.SName, &u.Role, &u.PasswordHash, &u.PasswordSalt, &u.LoginKeySalt, &u.EncryptedMasterKey, &u.IdentityPublicKey, &u.EncryptedIdentityKey, &u.Claimed, &u.ClaimCodeHash, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)

	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}
func InitSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id                     INTEGER PRIMARY KEY AUTOINCREMENT,
			login                  TEXT NOT NULL UNIQUE,
			fname                  TEXT NOT NULL,
			sname                  TEXT NOT NULL,
			role                   TEXT NOT NULL DEFAULT 'student',

			password_hash          BLOB,
			password_salt          BLOB,
			login_key_salt         BLOB,
			encrypted_master_key   BLOB,
			identity_public_key    BLOB,
			encrypted_identity_key BLOB,

			claimed                INTEGER NOT NULL DEFAULT 0,
			claim_code_hash        BLOB,

			created_at             INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS sessions (
			id         TEXT PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id),
			peer_id    TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		);
	`)

	return err
}
