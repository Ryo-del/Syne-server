package db

import (
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openAccountTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "account.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestUserSettingsAreStoredPerAccount(t *testing.T) {
	database := openAccountTestDB(t)
	for _, login := range []string{"alice", "bob"} {
		if _, err := database.Exec(`INSERT INTO users (login, fname, sname, role, created_at) VALUES (?, 'A', 'B', 'student', ?)`, login, time.Now().Unix()); err != nil {
			t.Fatal(err)
		}
	}
	want := []byte(`{"language":"fr","theme":"dark"}`)
	if err := PutUserSettings(database, "alice", want); err != nil {
		t.Fatal(err)
	}
	got, found, err := GetUserSettings(database, "alice")
	if err != nil || !found || !bytes.Equal(got, want) {
		t.Fatalf("alice settings = %s, found=%v, err=%v", got, found, err)
	}
	if got, found, err := GetUserSettings(database, "bob"); err != nil || found || got != nil {
		t.Fatalf("bob unexpectedly has settings: %s, found=%v, err=%v", got, found, err)
	}
}

func TestChangePasswordForSessionPreservesCurrentAndRevokesOthers(t *testing.T) {
	database := openAccountTestDB(t)
	if _, err := database.Exec(`INSERT INTO users (login, fname, sname, role, password_hash, created_at) VALUES ('alice', 'A', 'B', 'student', ?, ?)`, bytes.Repeat([]byte{1}, 32), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := database.QueryRow(`SELECT id FROM users WHERE login='alice'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"current", "other"} {
		if _, err := database.Exec(`INSERT INTO sessions (id, user_id, peer_id, created_at, expires_at) VALUES (?, ?, 'peer', ?, ?)`, session, userID, time.Now().Unix(), time.Now().Add(time.Hour).Unix()); err != nil {
			t.Fatal(err)
		}
	}
	oldHash := bytes.Repeat([]byte{1}, 32)
	newHash := bytes.Repeat([]byte{2}, 32)
	if err := ChangePasswordForSession(database, "current", "peer", oldHash, newHash,
		bytes.Repeat([]byte{3}, 16), bytes.Repeat([]byte{4}, 16), []byte("wrapped master"), []byte("public identity"), []byte("wrapped identity")); err != nil {
		t.Fatal(err)
	}
	var storedHash []byte
	if err := database.QueryRow(`SELECT password_hash FROM users WHERE id=?`, userID).Scan(&storedHash); err != nil || !bytes.Equal(storedHash, newHash) {
		t.Fatalf("password hash = %x, err=%v", storedHash, err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id=?`, userID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("session count = %d, err=%v", count, err)
	}
	if err := database.QueryRow(`SELECT id FROM sessions WHERE user_id=?`, userID).Scan(new(string)); err != nil {
		t.Fatalf("current session was not preserved: %v", err)
	}
	if err := ChangePasswordForSession(database, "current", "peer", oldHash, newHash,
		bytes.Repeat([]byte{3}, 16), bytes.Repeat([]byte{4}, 16), []byte("wrapped master"), []byte("public identity"), []byte("wrapped identity")); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("wrong old password error = %v", err)
	}
}
