package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"server/config"
	db "server/internal/db"
	identity "server/internal/identity"
)

// createClaimable создаёт ученика; если login пустой — генерирует числовой
// с длиной из настроек. Возвращает итоговый login и claim code.
func createClaimable(database *sql.DB, store *config.Store, login, fname, sname, role string) (string, string, error) {
	cfg := store.Get()
	login = strings.TrimSpace(login)
	role = strings.TrimSpace(role)
	if role == "" {
		role = "student"
	}
	autoID := login == ""

	for attempt := 0; attempt < 10; attempt++ {
		if autoID {
			var err error
			if login, err = identity.GenerateNumericID(cfg.IDDigits); err != nil {
				return "", "", err
			}
		}
		claimCode, err := identity.GenerateClaimCodeN(cfg.ClaimCodeLength)
		if err != nil {
			return "", "", err
		}
		hash := sha256.Sum256([]byte(claimCode))

		err = db.CreateClaimableUser(database, login, fname, sname, role, false, claimCode, hash[:])
		if errors.Is(err, db.ErrLoginAlreadyExists) && autoID {
			continue // коллизия случайного id — пробуем ещё раз
		}
		if err != nil {
			return "", "", err
		}
		return login, claimCode, nil
	}
	return "", "", errors.New("could not generate a unique id")
}

func createUserHandler(w http.ResponseWriter, r *http.Request, database *sql.DB, store *config.Store) {
	login, code, err := createClaimable(database, store,
		r.FormValue("login"), r.FormValue("fname"), r.FormValue("sname"), r.FormValue("role"))
	if err != nil {
		reason, status := "error create CreateClaimableUser", http.StatusInternalServerError
		if errors.Is(err, db.ErrLoginAlreadyExists) {
			reason, status = "login already exists", http.StatusBadRequest
		}
		writeError(w, status, reason)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "login": login, "claim_code": code})
}

// Экспорт: разделитель ';' — так русский Excel открывает CSV по колонкам.
func exportUsersHandler(w http.ResponseWriter, _ *http.Request, database *sql.DB) {
	users, err := db.GetAllUser(database)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get users")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	_ = cw.Write([]string{"login", "fname", "sname", "role", "claimed", "claim_code"})
	for _, u := range users {
		_ = cw.Write([]string{u.Login, u.FName, u.SName, u.Role, strconv.FormatBool(u.Claimed), u.ClaimCode})
	}
	cw.Flush()
}

// Импорт: колонки login;fname;sname;role. login можно оставить пустым —
// сгенерируется. Заголовок (первая ячейка "login") необязателен.
// Разделитель ',' или ';' определяется автоматически.
func importUsersHandler(w http.ResponseWriter, r *http.Request, database *sql.DB, store *config.Store) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 5<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	text := strings.TrimPrefix(string(raw), "\uFEFF")

	rd := csv.NewReader(strings.NewReader(text))
	rd.FieldsPerRecord = -1
	rd.TrimLeadingSpace = true
	firstLine, _, _ := strings.Cut(text, "\n")
	if strings.Contains(firstLine, ";") && !strings.Contains(firstLine, ",") {
		rd.Comma = ';'
	}
	records, err := rd.ReadAll()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid CSV: "+err.Error())
		return
	}

	type rowErr struct {
		Line   int    `json:"line"`
		Reason string `json:"reason"`
	}
	failed := []rowErr{}
	created := 0

	for i, rec := range records {
		if i == 0 && len(rec) > 0 && strings.EqualFold(strings.TrimSpace(rec[0]), "login") {
			continue
		}
		get := func(n int) string {
			if n < len(rec) {
				return strings.TrimSpace(rec[n])
			}
			return ""
		}
		if get(1) == "" || get(2) == "" {
			failed = append(failed, rowErr{i + 1, "fname и sname обязательны"})
			continue
		}
		if _, _, err := createClaimable(database, store, get(0), get(1), get(2), get(3)); err != nil {
			reason := "ошибка создания"
			if errors.Is(err, db.ErrLoginAlreadyExists) {
				reason = "логин уже существует"
			}
			failed = append(failed, rowErr{i + 1, reason})
			continue
		}
		created++
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": created, "failed": failed})
}
