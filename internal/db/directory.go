package db

import (
	"database/sql"
	"sort"
	"strings"
	"time"
)

// DirectoryUser — то, что можно показывать другим пользователям.
// Никаких хэшей, ключей и claim-кодов.
type DirectoryUser struct {
	Login string
	FName string
	SName string
	Role  string
}

// LoginBySession — login владельца действующей сессии.
func LoginBySession(dbConn *sql.DB, sessionID string) (string, error) {
	var login string
	err := dbConn.QueryRow(`
		SELECT u.login
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.id = ? AND s.expires_at > ?
	`, sessionID, time.Now().Unix()).Scan(&login)
	return login, err
}

// SearchDirectory ищет по всем пользователям, кроме excludeLogin.
// Фильтрация в Go, а не через LIKE: SQLite не учитывает регистр кириллицы.
// Запрос из нескольких слов: каждое должно встретиться в "фамилия имя логин".
func SearchDirectory(dbConn *sql.DB, excludeLogin, query string, limit int) ([]DirectoryUser, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	rows, err := dbConn.Query(`
		SELECT login, fname, sname, role FROM users WHERE login <> ?
	`, excludeLogin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := strings.Fields(strings.ToLower(query))
	var out []DirectoryUser
	for rows.Next() {
		var u DirectoryUser
		if err := rows.Scan(&u.Login, &u.FName, &u.SName, &u.Role); err != nil {
			return nil, err
		}
		hay := strings.ToLower(u.SName + " " + u.FName + " " + u.Login)
		match := true
		for _, t := range tokens {
			if !strings.Contains(hay, t) {
				match = false
				break
			}
		}
		if match {
			out = append(out, u)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(out, func(i, j int) bool {
		a := strings.ToLower(out[i].SName + " " + out[i].FName)
		b := strings.ToLower(out[j].SName + " " + out[j].FName)
		if a != b {
			return a < b
		}
		return out[i].Login < out[j].Login
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
