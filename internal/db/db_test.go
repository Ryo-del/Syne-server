package db

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	if err := InitSchema(db); err != nil {
		db.Close()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func TestHexEncodeRandomBytes(t *testing.T) {
	result, err := HexEncodeRandomBytes(32)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 64 {
		t.Fatalf("expected length 64, got %d", len(result))
	}

	result2, err := HexEncodeRandomBytes(32)
	if err != nil {
		t.Fatal(err)
	}

	if result == result2 {
		t.Fatal("expected different random values")
	}
}

func TestCreateUser(t *testing.T) {
	db := setupTestDB(t)

	err := CreateUser(
		db,
		"testuser",
		"Test",
		"User",
		[]byte("hash"),
		[]byte("salt"),
		[]byte("loginsalt"),
		[]byte("encryptedkey"),
	)
	if err != nil {
		t.Fatal(err)
	}

	user, err := GetUserByLogin(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}

	if user.Login != "testuser" {
		t.Fatalf("expected login testuser, got %s", user.Login)
	}

	if user.FName != "Test" {
		t.Fatalf("expected fname Test, got %s", user.FName)
	}

	if user.SName != "User" {
		t.Fatalf("expected sname User, got %s", user.SName)
	}

	if string(user.PasswordHash) != "hash" {
		t.Fatal("password hash does not match")
	}

	if string(user.PasswordSalt) != "salt" {
		t.Fatal("password salt does not match")
	}

	if string(user.LoginKeySalt) != "loginsalt" {
		t.Fatal("login key salt does not match")
	}

	if string(user.EncryptedMasterKey) != "encryptedkey" {
		t.Fatal("encrypted master key does not match")
	}

	if user.CreatedAt == 0 {
		t.Fatal("created_at was not set")
	}
}

func TestCreateUserDuplicateLogin(t *testing.T) {
	db := setupTestDB(t)

	err := CreateUser(
		db,
		"testuser",
		"Test",
		"User",
		[]byte("hash"),
		[]byte("salt"),
		[]byte("loginsalt"),
		[]byte("encryptedkey"),
	)
	if err != nil {
		t.Fatal(err)
	}

	err = CreateUser(
		db,
		"testuser",
		"Another",
		"User",
		[]byte("hash"),
		[]byte("salt"),
		[]byte("loginsalt"),
		[]byte("encryptedkey"),
	)

	if !errors.Is(err, ErrLoginAlreadyExists) {
		t.Fatalf("expected ErrLoginAlreadyExists, got %v", err)
	}
}

func TestGetUserByLogin(t *testing.T) {
	db := setupTestDB(t)

	err := CreateUser(
		db,
		"mr_testik",
		"Test",
		"User",
		[]byte("hash"),
		[]byte("salt"),
		[]byte("loginsalt"),
		[]byte("encryptedkey"),
	)
	if err != nil {
		t.Fatal(err)
	}

	user, err := GetUserByLogin(db, "mr_testik")
	if err != nil {
		t.Fatal(err)
	}

	if user.Login != "mr_testik" {
		t.Fatalf("expected login mr_testik, got %s", user.Login)
	}
}

func TestGetUserByLoginNotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := GetUserByLogin(db, "does_not_exist")

	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestCreateSession(t *testing.T) {
	db := setupTestDB(t)

	err := CreateUser(
		db,
		"testuser",
		"Test",
		"User",
		[]byte("hash"),
		[]byte("salt"),
		[]byte("loginsalt"),
		[]byte("encryptedkey"),
	)
	if err != nil {
		t.Fatal(err)
	}

	user, err := GetUserByLogin(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}

	sessionID, err := CreateSession(
		db,
		user.ID,
		"test-peer-id",
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}

	if sessionID == "" {
		t.Fatal("expected session ID")
	}

	if len(sessionID) != 64 {
		t.Fatalf("expected session ID length 64, got %d", len(sessionID))
	}

	var peerID string
	var userID int64
	var expiresAt int64

	err = db.QueryRow(`
		SELECT user_id, peer_id, expires_at
		FROM sessions
		WHERE id = ?
	`, sessionID).Scan(&userID, &peerID, &expiresAt)

	if err != nil {
		t.Fatal(err)
	}

	if userID != user.ID {
		t.Fatalf("expected user ID %d, got %d", user.ID, userID)
	}

	if peerID != "test-peer-id" {
		t.Fatalf("expected peer ID test-peer-id, got %s", peerID)
	}

	if expiresAt <= time.Now().Unix() {
		t.Fatal("session expiration time is invalid")
	}
}

func TestDeleteSession(t *testing.T) {
	db := setupTestDB(t)

	err := CreateUser(
		db,
		"testuser",
		"Test",
		"User",
		[]byte("hash"),
		[]byte("salt"),
		[]byte("loginsalt"),
		[]byte("encryptedkey"),
	)
	if err != nil {
		t.Fatal(err)
	}

	user, err := GetUserByLogin(db, "testuser")
	if err != nil {
		t.Fatal(err)
	}

	sessionID, err := CreateSession(
		db,
		user.ID,
		"test-peer-id",
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = DeleteSession(db, sessionID)
	if err != nil {
		t.Fatal(err)
	}

	var count int

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM sessions
		WHERE id = ?
	`, sessionID).Scan(&count)

	if err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("expected session to be deleted, got %d", count)
	}
}

func TestInitSchema(t *testing.T) {
	db := setupTestDB(t)

	var tableCount int

	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
		AND name IN ('users', 'sessions')
	`).Scan(&tableCount)

	if err != nil {
		t.Fatal(err)
	}

	if tableCount != 2 {
		t.Fatalf("expected 2 tables, got %d", tableCount)
	}
}
