package main

import (
	"database/sql"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"server/internal/auth"
	control "server/internal/control"
	db "server/internal/db"
	identity "server/internal/identity"

	protocol "github.com/Ryo-del/Syne-protocol"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Server struct {
	DB *sql.DB
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
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/9000"),
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
	for _, a := range addrs {
		slog.Info("listening", "addr", a)
	}
	host.SetStreamHandler(protocol.StreamProtocol, controlhandler.HandleStream)
	authHandler := auth.NewAuthHandler(database)
	host.SetStreamHandler(protocol.AuthStreamProtocol, authHandler.HandleStream)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("shutting down")
	_ = host.Close()
	_ = database.Close()

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
