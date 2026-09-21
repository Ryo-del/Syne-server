package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"server/internal/auth"
	control "server/internal/control"
	db "server/internal/db"
	identity "server/internal/identity"
	metrics "server/internal/metrics"
	presence "server/internal/presence"

	protocol "github.com/Ryo-del/Syne-protocol"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Server struct {
	DB *sql.DB
}

// publicUser — то, что реально можно отдавать в панель лаборанта.
// В отличие от db.User здесь нет хэшей пароля/ключей.
type publicUser struct {
	ID        int64  `json:"ID"`
	Login     string `json:"Login"`
	FName     string `json:"FName"`
	SName     string `json:"SName"`
	Role      string `json:"Role"`
	Claimed   bool   `json:"Claimed"`
	ClaimCode string `json:"ClaimCode"`
	CreatedAt int64  `json:"CreatedAt"`
}

func toPublicUser(u db.User) publicUser {
	return publicUser{
		ID:        u.ID,
		Login:     u.Login,
		FName:     u.FName,
		SName:     u.SName,
		Role:      u.Role,
		Claimed:   u.Claimed,
		ClaimCode: u.ClaimCode,
		CreatedAt: u.CreatedAt,
	}
}

func ConnectToDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	return db, nil
}
func main() {
	var listenAddr string
	var port string
	flag.StringVar(&listenAddr, "listen", "/ip4/0.0.0.0/tcp/9000", "libp2p listen multiaddr")
	flag.StringVar(&port, "port", "8080", "for host frontend api")
	flag.Parse()
	startedAt := time.Now()
	path, err := os.Getwd()
	if err != nil {
		slog.Error("can't to get path", "error", err)
		return
	}
	path = filepath.Join(path, ".identity")

	privKey, err := identity.LoadOrCreateIdentity(path)
	if err != nil {
		slog.Error("Error with get privKey", "error", err)
		return
	}

	host, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(listenAddr),
	)
	if err != nil {
		slog.Error("error create host", "error", err)
		return
	}
	database, err := ConnectToDB("storage/syne.db")
	if err != nil {
		slog.Error("error to create database")
		return
	}
	err = db.InitSchema(database)
	if err != nil {
		slog.Error("failed to init schema", "error", err)
		return
	}
	controlhandler := control.NewControlHandler(host)
	addrs, err := fullAddrs(host)
	if err != nil {
		slog.Error("failed to resolve addrs", "error", err)
		return
	}
	collector := metrics.NewCollector()

	go HostApi("8080", collector, startedAt, database)
	for _, a := range addrs {
		slog.Info("listening", "addr", a)
	}
	host.SetStreamHandler(protocol.StreamProtocol, controlhandler.HandleStream)
	authHandler := auth.NewAuthHandler(database)
	host.SetStreamHandler(protocol.AuthStreamProtocol, authHandler.HandleStream)
	presenceHandler := presence.NewPresenceHandler()
	host.SetStreamHandler(protocol.PresenceStreamProtocol, presenceHandler.HandleStream)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("shutting down")
	_ = host.Close()
	_ = database.Close()

}
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
func HostApi(port string, collector *metrics.Collector, startedAt time.Time, database *sql.DB) {
	mux := http.NewServeMux()

	metrics.RegisterRoutes(mux, collector)

	mux.HandleFunc("/api/uptime", func(w http.ResponseWriter, r *http.Request) {
		uptime := time.Since(startedAt)

		w.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(w).Encode(map[string]float64{
			"seconds": uptime.Seconds(),
		})
	})
	mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getUsersHandler(w, database)
		case http.MethodPost:
			createUserHandler(w, r, database)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	mux.HandleFunc("/api/users/", func(w http.ResponseWriter, r *http.Request) {
		login := strings.TrimPrefix(r.URL.Path, "/api/users/")
		login = strings.Trim(login, "/")
		if login == "" {
			writeError(w, http.StatusBadRequest, "login is required")
			return
		}

		switch r.Method {
		case http.MethodPatch:
			updateUserHandler(w, r, database, login)
		case http.MethodDelete:
			deleteUserHandler(w, database, login)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	handler := cors(mux)

	slog.Info("HTTP API listening", "port", port)

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		slog.Error("HTTP API stopped", "error", err)
	}
}
func createUserHandler(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	login := r.FormValue("login")
	fname := r.FormValue("fname")
	sname := r.FormValue("sname")
	role := r.FormValue("role")
	claimed := false //false → аккаунт создан админом, но ещё не забран | true  → пользователь уже активировал аккаунт
	claimCode, err := identity.GenerateClaimCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "error create claimCodeHash")
		return
	}
	claimCodeHash := sha256.Sum256([]byte(claimCode))

	err = db.CreateClaimableUser(database, login, fname, sname, role, claimed, claimCode, claimCodeHash[:])
	if err != nil {
		reason := "error create CreateClaimableUser"
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrLoginAlreadyExists) {
			reason = "login already exists"
			status = http.StatusBadRequest
		}
		writeError(w, status, reason)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"claim_code": claimCode,
	})
}

func updateUserHandler(w http.ResponseWriter, r *http.Request, database *sql.DB, login string) {
	var req struct {
		FName string `json:"fname"`
		SName string `json:"sname"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(req.FName) == "" || strings.TrimSpace(req.SName) == "" {
		writeError(w, http.StatusBadRequest, "fname and sname are required")
		return
	}

	if err := db.UpdateUser(database, login, req.FName, req.SName, req.Role); err != nil {
		status := http.StatusInternalServerError
		reason := "failed to update user"
		if errors.Is(err, db.ErrUserNotFound) {
			status = http.StatusNotFound
			reason = "user not found"
		}
		writeError(w, status, reason)
		return
	}

	user, err := db.GetUserByLogin(database, login)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated user")
		return
	}

	writeJSON(w, http.StatusOK, toPublicUser(*user))
}

func deleteUserHandler(w http.ResponseWriter, database *sql.DB, login string) {
	if err := db.DeleteUser(database, login); err != nil {
		status := http.StatusInternalServerError
		reason := "failed to delete user"
		if errors.Is(err, db.ErrUserNotFound) {
			status = http.StatusNotFound
			reason = "user not found"
		}
		writeError(w, status, reason)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func getUsersHandler(w http.ResponseWriter, database *sql.DB) {
	users, err := db.GetAllUser(database)
	if err != nil {
		http.Error(w, "failed to get users", http.StatusInternalServerError)
		return
	}

	out := make([]publicUser, 0, len(users))
	for _, u := range users {
		out = append(out, toPublicUser(u))
	}

	writeJSON(w, http.StatusOK, out)
}
func fullAddrs(h host.Host) ([]string, error) {
	info := peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()}
	addrs, err := peer.AddrInfoToP2pAddrs(&info)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out, nil

}
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}
