package db

import (
	"database/sql"
	"errors"
	"time"
)

type SessionInfo struct {
	Login     string
	PeerID    string
	ExpiresAt int64
}

type MailboxRow struct {
	ID   int64
	Data []byte
}

type ErrorReport struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Error     string `json:"error"`
	CreatedAt int64  `json:"created_at"`
}

func initSyncSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS vaults (
			login      TEXT PRIMARY KEY,
			version    INTEGER NOT NULL,
			data       BLOB NOT NULL,
			updated_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS mailbox (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			to_login   TEXT NOT NULL,
			from_login TEXT NOT NULL,
			data       BLOB NOT NULL,
			created_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_mailbox_to ON mailbox(to_login, id);

		CREATE TABLE IF NOT EXISTS error_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			login TEXT NOT NULL,
			name TEXT NOT NULL,
			ip TEXT NOT NULL,
			error TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS user_settings (
			user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			data       TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_error_reports_created ON error_reports(id DESC);
	`)
	return err
}

func GetUserSettings(db *sql.DB, login string) ([]byte, bool, error) {
	var data []byte
	err := db.QueryRow(`SELECT s.data FROM user_settings s JOIN users u ON u.id = s.user_id WHERE u.login = ?`, login).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return data, err == nil, err
}

func PutUserSettings(db *sql.DB, login string, data []byte) error {
	_, err := db.Exec(`
		INSERT INTO user_settings (user_id, data, updated_at)
		SELECT id, ?, ? FROM users WHERE login = ?
		ON CONFLICT(user_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at
	`, string(data), time.Now().Unix(), login)
	return err
}

func AddErrorReport(db *sql.DB, login, name, ip, message string) error {
	_, err := db.Exec(`INSERT INTO error_reports (login, name, ip, error, created_at) VALUES (?, ?, ?, ?, ?)`, login, name, ip, message, time.Now().UnixMilli())
	return err
}

func ListErrorReports(db *sql.DB, limit int) ([]ErrorReport, error) {
	rows, err := db.Query(`SELECT id, name, ip, error, created_at FROM error_reports ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ErrorReport, 0)
	for rows.Next() {
		var row ErrorReport
		if err := rows.Scan(&row.ID, &row.Name, &row.IP, &row.Error, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func CountErrorReports(db *sql.DB) (int64, error) {
	var count int64
	err := db.QueryRow(`SELECT COUNT(*) FROM error_reports`).Scan(&count)
	return count, err
}

// GetSessionInfo возвращает логин и peer_id, к которым привязана сессия.
func GetSessionInfo(db *sql.DB, sessionID string) (*SessionInfo, error) {
	var info SessionInfo
	err := db.QueryRow(`
		SELECT u.login, s.peer_id, s.expires_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.id = ?
	`, sessionID).Scan(&info.Login, &info.PeerID, &info.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func GetVault(db *sql.DB, login string) ([]byte, bool, error) {
	var data []byte
	err := db.QueryRow(`SELECT data FROM vaults WHERE login = ?`, login).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func PutVault(db *sql.DB, login string, data []byte) error {
	_, err := db.Exec(`
		INSERT INTO vaults (login, version, data, updated_at)
		VALUES (?, 1, ?, ?)
		ON CONFLICT(login) DO UPDATE SET
			version = vaults.version + 1,
			data = excluded.data,
			updated_at = excluded.updated_at
	`, login, data, time.Now().Unix())
	return err
}

// GetUserKeys возвращает имя и публичный identity-ключ пользователя.
// sql.ErrNoRows — если такого логина нет.
func GetUserKeys(db *sql.DB, login string) (fname, sname string, pub []byte, err error) {
	err = db.QueryRow(`
		SELECT fname, sname, identity_public_key FROM users WHERE login = ?
	`, login).Scan(&fname, &sname, &pub)
	return
}

func PushMailbox(db *sql.DB, toLogin, fromLogin string, data []byte) error {
	_, err := db.Exec(`
		INSERT INTO mailbox (to_login, from_login, data, created_at)
		VALUES (?, ?, ?, ?)
	`, toLogin, fromLogin, data, time.Now().Unix())
	return err
}

func FetchMailbox(db *sql.DB, login string, limit int) ([]MailboxRow, error) {
	rows, err := db.Query(`
		SELECT id, data FROM mailbox WHERE to_login = ? ORDER BY id ASC LIMIT ?
	`, login, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MailboxRow
	for rows.Next() {
		var r MailboxRow
		if err := rows.Scan(&r.ID, &r.Data); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AckMailbox удаляет только сообщения, адресованные именно этому логину.
func AckMailbox(db *sql.DB, login string, ids []int64) error {
	for _, id := range ids {
		if _, err := db.Exec(`DELETE FROM mailbox WHERE id = ? AND to_login = ?`, id, login); err != nil {
			return err
		}
	}
	return nil
}
