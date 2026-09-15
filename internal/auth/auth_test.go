package auth

import (
	"context"
	"database/sql"
	"testing"

	"server/internal/db"

	protocol "github.com/Ryo-del/Syne-protocol"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	coreprotocol "github.com/libp2p/go-libp2p/core/protocol"

	_ "modernc.org/sqlite"
)

const testAuthProtocol = coreprotocol.ID("/syne/auth/test/1.0.0")

func setupAuthTestDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		database.Close()
	})

	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}

	return database
}

func setupHosts(t *testing.T, handler *AuthHandler) (network.Stream, func()) {
	t.Helper()

	ctx := context.Background()

	serverHost, err := libp2p.New()
	if err != nil {
		t.Fatal(err)
	}

	clientHost, err := libp2p.New()
	if err != nil {
		serverHost.Close()
		t.Fatal(err)
	}

	serverHost.SetStreamHandler(
		testAuthProtocol,
		handler.HandleStream,
	)

	serverInfo := peer.AddrInfo{
		ID:    serverHost.ID(),
		Addrs: serverHost.Addrs(),
	}

	if err := clientHost.Connect(ctx, serverInfo); err != nil {
		serverHost.Close()
		clientHost.Close()
		t.Fatal(err)
	}

	stream, err := clientHost.NewStream(
		ctx,
		serverHost.ID(),
		testAuthProtocol,
	)
	if err != nil {
		serverHost.Close()
		clientHost.Close()
		t.Fatal(err)
	}

	cleanup := func() {
		stream.Close()
		clientHost.Close()
		serverHost.Close()
	}

	return stream, cleanup
}

func writeMessage(t *testing.T, stream network.Stream, message any) {
	t.Helper()

	data, err := protocol.MarshalJSON(message)
	if err != nil {
		t.Fatal(err)
	}

	if err := protocol.WriteFramedMessage(stream, data); err != nil {
		t.Fatal(err)
	}
}

func readMessage[T any](t *testing.T, stream network.Stream) T {
	t.Helper()

	data, err := protocol.ReadFramedMessage(stream)
	if err != nil {
		t.Fatal(err)
	}

	message, err := protocol.UnmarshalJSON[T](data)
	if err != nil {
		t.Fatal(err)
	}

	return message
}
func TestHandleRegisterSuccess(t *testing.T) {
	database := setupAuthTestDB(t)
	handler := NewAuthHandler(database)

	stream, cleanup := setupHosts(t, handler)
	defer cleanup()

	request := protocol.RegisterRequest{
		Type:                 protocol.AuthTypeRegisterRequest,
		Login:                "testuser",
		FName:                "Test",
		SName:                "User",
		PasswordHash:         []byte("password-hash"),
		PasswordSalt:         []byte("password-salt"),
		LoginKeySalt:         []byte("login-key-salt"),
		EncryptedMasterKey:   []byte("encrypted-master-key"),
		IdentityPublicKey:    []byte("identity-public-key"),
		EncryptedIdentityKey: []byte("encrypted-identity-key"),
	}

	writeMessage(t, stream, request)

	response := readMessage[protocol.RegisterSuccess](t, stream)

	if response.Type != protocol.AuthTypeRegisterSuccess {
		t.Fatalf(
			"expected type %v, got %v",
			protocol.AuthTypeRegisterSuccess,
			response.Type,
		)
	}

	user, err := db.GetUserByLogin(database, "testuser")
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
}
func TestHandleRegisterDuplicateLogin(t *testing.T) {
	database := setupAuthTestDB(t)
	handler := NewAuthHandler(database)

	if err := db.CreateUser(
		database,
		"testuser",
		"Old",
		"User",
		[]byte("hash"),
		[]byte("salt"),
		[]byte("login-salt"),
		[]byte("master-key"),
		[]byte("identityPublicKey"),
		[]byte("encryptedIdentityKey"),
	); err != nil {
		t.Fatal(err)
	}

	stream, cleanup := setupHosts(t, handler)
	defer cleanup()

	request := protocol.RegisterRequest{
		Type:                 protocol.AuthTypeRegisterRequest,
		Login:                "testuser",
		FName:                "New",
		SName:                "User",
		PasswordHash:         []byte("new-hash"),
		PasswordSalt:         []byte("new-salt"),
		LoginKeySalt:         []byte("new-login-salt"),
		EncryptedMasterKey:   []byte("new-master-key"),
		IdentityPublicKey:    []byte("identity-public-key"),
		EncryptedIdentityKey: []byte("encrypted-identity-key"),
	}

	writeMessage(t, stream, request)

	response := readMessage[protocol.RegisterFailure](t, stream)

	if response.Type != protocol.AuthTypeRegisterFailure {
		t.Fatalf(
			"expected type %v, got %v",
			protocol.AuthTypeRegisterFailure,
			response.Type,
		)
	}

	if response.Reason != "login already exists" {
		t.Fatalf(
			"expected reason %q, got %q",
			"login already exists",
			response.Reason,
		)
	}
}
func TestHandleLoginUserNotFound(t *testing.T) {
	database := setupAuthTestDB(t)
	handler := NewAuthHandler(database)

	stream, cleanup := setupHosts(t, handler)
	defer cleanup()

	request := protocol.LoginRequest{
		Type:  protocol.AuthTypeLoginRequest,
		Login: "does-not-exist",
	}

	writeMessage(t, stream, request)

	response := readMessage[protocol.LoginFailure](t, stream)

	if response.Type != protocol.AuthTypeLoginFailure {
		t.Fatalf(
			"expected type %v, got %v",
			protocol.AuthTypeLoginFailure,
			response.Type,
		)
	}

	if response.Reason != "invalid login or password" {
		t.Fatalf(
			"expected reason %q, got %q",
			"invalid login or password",
			response.Reason,
		)
	}
}
func TestHandleLoginWrongPassword(t *testing.T) {
	database := setupAuthTestDB(t)
	handler := NewAuthHandler(database)

	passwordHash := []byte("correct-password-hash")

	if err := db.CreateUser(
		database,
		"testuser",
		"Test",
		"User",
		passwordHash,
		[]byte("password-salt"),
		[]byte("login-key-salt"),
		[]byte("encrypted-master-key"),
		[]byte("identityPublicKey"),
		[]byte("encryptedIdentityKey"),
	); err != nil {
		t.Fatal(err)
	}

	stream, cleanup := setupHosts(t, handler)
	defer cleanup()

	loginRequest := protocol.LoginRequest{
		Type:  protocol.AuthTypeLoginRequest,
		Login: "testuser",
	}

	writeMessage(t, stream, loginRequest)

	challenge := readMessage[protocol.LoginChallenge](t, stream)

	if challenge.Type != protocol.AuthTypeLoginChallenge {
		t.Fatalf(
			"expected type %v, got %v",
			protocol.AuthTypeLoginChallenge,
			challenge.Type,
		)
	}

	verify := protocol.LoginVerify{
		Type:         protocol.AuthTypeLoginVerify,
		PasswordHash: []byte("WRONG-PASSWORD-HASH"),
	}

	writeMessage(t, stream, verify)

	response := readMessage[protocol.LoginFailure](t, stream)

	if response.Type != protocol.AuthTypeLoginFailure {
		t.Fatalf(
			"expected type %v, got %v",
			protocol.AuthTypeLoginFailure,
			response.Type,
		)
	}

	if response.Reason != "invalid login or password" {
		t.Fatalf(
			"expected reason %q, got %q",
			"invalid login or password",
			response.Reason,
		)
	}
}
func TestHandleLoginSuccess(t *testing.T) {
	database := setupAuthTestDB(t)
	handler := NewAuthHandler(database)

	passwordHash := []byte("correct-password-hash")
	passwordSalt := []byte("password-salt")
	loginKeySalt := []byte("login-key-salt")
	masterKey := []byte("encrypted-master-key")
	identityPublicKey := []byte("identityPublicKey")
	encryptedIdentityKey := []byte("encryptedIdentityKey")

	if err := db.CreateUser(
		database,
		"testuser",
		"Test",
		"User",
		passwordHash,
		passwordSalt,
		loginKeySalt,
		masterKey,
		identityPublicKey,
		encryptedIdentityKey,
	); err != nil {
		t.Fatal(err)
	}

	stream, cleanup := setupHosts(t, handler)
	defer cleanup()

	loginRequest := protocol.LoginRequest{
		Type:  protocol.AuthTypeLoginRequest,
		Login: "testuser",
	}

	writeMessage(t, stream, loginRequest)

	challenge := readMessage[protocol.LoginChallenge](t, stream)

	if challenge.Type != protocol.AuthTypeLoginChallenge {
		t.Fatalf(
			"expected type %v, got %v",
			protocol.AuthTypeLoginChallenge,
			challenge.Type,
		)
	}

	if string(challenge.PasswordSalt) != string(passwordSalt) {
		t.Fatal("password salt does not match")
	}

	if string(challenge.LoginKeySalt) != string(loginKeySalt) {
		t.Fatal("login key salt does not match")
	}

	verify := protocol.LoginVerify{
		Type:         protocol.AuthTypeLoginVerify,
		PasswordHash: passwordHash,
	}

	writeMessage(t, stream, verify)

	response := readMessage[protocol.LoginSuccess](t, stream)

	if response.Type != protocol.AuthTypeLoginSuccess {
		t.Fatalf(
			"expected type %v, got %v",
			protocol.AuthTypeLoginSuccess,
			response.Type,
		)
	}

	if response.SessionID == "" {
		t.Fatal("session ID is empty")
	}

	if response.FName != "Test" {
		t.Fatalf("expected fname Test, got %s", response.FName)
	}

	if response.SName != "User" {
		t.Fatalf("expected sname User, got %s", response.SName)
	}

	if string(response.EncryptedMasterKey) != string(masterKey) {
		t.Fatal("encrypted master key does not match")
	}
}
func TestHandleLoginCreatesSession(t *testing.T) {
	database := setupAuthTestDB(t)
	handler := NewAuthHandler(database)

	passwordHash := []byte("correct-hash")
	identityPublicKey := []byte("identityPublicKey")
	encryptedIdentityKey := []byte("encryptedIdentityKey")
	if err := db.CreateUser(
		database,
		"testuser",
		"Test",
		"User",
		passwordHash,
		[]byte("salt"),
		[]byte("login-salt"),
		[]byte("master-key"),
		identityPublicKey,
		encryptedIdentityKey,
	); err != nil {
		t.Fatal(err)
	}

	stream, cleanup := setupHosts(t, handler)
	defer cleanup()

	writeMessage(t, stream, protocol.LoginRequest{
		Type:  protocol.AuthTypeLoginRequest,
		Login: "testuser",
	})

	_ = readMessage[protocol.LoginChallenge](t, stream)

	writeMessage(t, stream, protocol.LoginVerify{
		Type:         protocol.AuthTypeLoginVerify,
		PasswordHash: passwordHash,
	})

	response := readMessage[protocol.LoginSuccess](t, stream)

	var count int

	err := database.QueryRow(
		`SELECT COUNT(*) FROM sessions WHERE id = ?`,
		response.SessionID,
	).Scan(&count)

	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("expected 1 session, got %d", count)
	}
}
