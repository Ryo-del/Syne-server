package files

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	protocol "github.com/Ryo-del/Syne-protocol"
)

func codeOf(err error) string { return respErr(err).Code }

func (e *svcEnv) usage(owner string) int64 {
	e.t.Helper()
	a := Actor{Login: owner, Role: "student"}
	return e.ok(a, rq(protocol.FilesOpQuota, "", "")).Quota.UsedBytes
}

func (e *svcEnv) upload(a Actor, owner, parent, name string, size int64, policy, body string) (protocol.FilesResponse, error) {
	req := rq(protocol.FilesOpUpload, owner, parent)
	req.Name, req.Size, req.OnConflict = name, size, policy
	up, err := e.svc.BeginUpload(a, req)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if up == nil {
		return protocol.FilesResponse{Skipped: 1}, nil
	}
	return up.Commit(strings.NewReader(body))
}

func (e *svcEnv) save(a Actor, owner, path string, size, base int64, body string) (protocol.FilesResponse, error) {
	req := rq(protocol.FilesOpWrite, owner, path)
	req.Size, req.BaseModTime = size, base
	up, err := e.svc.BeginWrite(a, req)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	return up.Commit(strings.NewReader(body))
}

func (e *svcEnv) noTemps(owner, rel string) {
	e.t.Helper()
	des, err := os.ReadDir(e.abs(owner, rel))
	if err != nil {
		e.t.Fatal(err)
	}
	for _, d := range des {
		if strings.HasPrefix(d.Name(), TempPrefix) {
			e.t.Errorf("temp file left behind: %s", d.Name())
		}
	}
}

func wantCode(t *testing.T, what string, err error, code string) {
	t.Helper()
	if err == nil || codeOf(err) != code {
		t.Errorf("%s: err = %v, want code %q", what, err, code)
	}
}

func TestServiceReadAndDownload(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "docs/a.txt", "hello")
	e.write("alice", "docs/pic.png", "png")
	e.write("alice", "docs/secret.txt", "s")
	e.rule(uAlice, "", "docs", protocol.ActionView, protocol.ModeAll)
	e.rule(uAlice, "", "docs/secret.txt", protocol.ActionView, protocol.ModeNone)

	read := func(a Actor, path string, download bool) (string, error) {
		f, entry, err := e.svc.OpenRead(a, rq(protocol.FilesOpRead, "alice", path), download)
		if err != nil {
			return "", err
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(b)) != entry.Size {
			t.Errorf("entry.Size = %d, read %d bytes", entry.Size, len(b))
		}
		return string(b), nil
	}

	if got, err := read(uBob, "docs/a.txt", false); err != nil || got != "hello" {
		t.Errorf("read a.txt = %q, %v", got, err)
	}
	_, err := read(uBob, "docs/pic.png", false)
	wantCode(t, "open non-txt in the editor", err, protocol.FilesErrInvalidRequest)
	_, err = read(uBob, "docs/a.txt", true)
	wantCode(t, "download without the right", err, protocol.FilesErrForbidden)
	_, err = read(uBob, "docs", false)
	wantCode(t, "read a folder", err, protocol.FilesErrInvalidRequest)
	_, err = read(uBob, "docs/secret.txt", false)
	wantCode(t, "hidden file", err, protocol.FilesErrNotFound)
	_, err = read(uBob, "docs/nope.txt", false)
	wantCode(t, "missing file", err, protocol.FilesErrNotFound)

	e.rule(uAlice, "", "docs", protocol.ActionDownload, protocol.ModeAll)
	if got, err := read(uBob, "docs/pic.png", true); err != nil || got != "png" {
		t.Errorf("download pic.png = %q, %v", got, err)
	}
	if got, err := read(uAlice, "docs/secret.txt", true); err != nil || got != "s" {
		t.Errorf("owner reads hidden file = %q, %v", got, err)
	}
}

func TestServiceUpload(t *testing.T) {
	e := newSvcEnv(t)

	r, err := e.upload(uAlice, "", "", "n.txt", 5, "", "hello")
	if err != nil || r.Entry == nil || r.Entry.Name != "n.txt" {
		t.Fatalf("upload = %+v, %v", r, err)
	}
	if e.read("alice", "n.txt") != "hello" || e.usage("alice") != 5 {
		t.Fatalf("content %q, usage %d", e.read("alice", "n.txt"), e.usage("alice"))
	}

	// короткая загрузка: ничего не остаётся, место возвращается
	_, err = e.upload(uAlice, "", "", "s.txt", 5, "", "hi")
	wantCode(t, "short upload", err, protocol.FilesErrInvalidRequest)
	if e.exists("alice", "s.txt") || e.usage("alice") != 5 {
		t.Errorf("short upload left state behind: exists=%v usage=%d", e.exists("alice", "s.txt"), e.usage("alice"))
	}
	e.noTemps("alice", "")

	// конфликты имён
	_, err = e.upload(uAlice, "", "", "N.TXT", 1, "", "x")
	wantCode(t, "name conflict", err, protocol.FilesErrExists)
	r, err = e.upload(uAlice, "", "", "n.txt", 5, protocol.ConflictRename, "world")
	if err != nil || r.Entry == nil || r.Entry.Name != "n (1).txt" || e.read("alice", "n (1).txt") != "world" {
		t.Fatalf("rename policy = %+v, %v", r, err)
	}
	if e.usage("alice") != 10 {
		t.Errorf("usage = %d, want 10", e.usage("alice"))
	}
	if r, err = e.upload(uAlice, "", "", "n.txt", 1, protocol.ConflictSkip, "x"); err != nil || r.Skipped != 1 {
		t.Errorf("skip policy = %+v, %v", r, err)
	}
	// замена с меньшим файлом: 10 - 5 + 3 = 8
	if _, err = e.upload(uAlice, "", "", "n.txt", 3, protocol.ConflictReplace, "new"); err != nil {
		t.Fatal(err)
	}
	if e.read("alice", "n.txt") != "new" || e.usage("alice") != 8 {
		t.Errorf("replace: content %q, usage %d", e.read("alice", "n.txt"), e.usage("alice"))
	}

	// слишком большой файл отклоняется до начала передачи
	_, err = e.upload(uAlice, "", "", "big.bin", MaxFileBytes+1, "", "")
	wantCode(t, "file over 2 GiB", err, protocol.FilesErrInvalidRequest)
	e.noTemps("alice", "")
}

func TestServiceUploadPermissions(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "up/old.txt", "12")
	e.rule(uAlice, "", "up", protocol.ActionPaste, protocol.ModeAll)

	// корень Алисы виден Борису (в нём есть папка up), но вставка там не разрешена
	_, err := e.upload(uBob, "alice", "", "x.txt", 1, "", "x")
	wantCode(t, "upload into a folder without paste", err, protocol.FilesErrForbidden)

	if _, err := e.upload(uBob, "alice", "up", "b.txt", 2, "", "ok"); err != nil {
		t.Fatalf("upload into a folder with paste: %v", err)
	}
	if e.read("alice", "up/b.txt") != "ok" {
		t.Error("uploaded content mismatch")
	}
	// место занимает владелец папки
	if e.usage("alice") != 4 {
		t.Errorf("owner usage = %d, want 4", e.usage("alice"))
	}

	// заменить существующий файл без права удаления нельзя
	_, err = e.upload(uBob, "alice", "up", "old.txt", 1, protocol.ConflictReplace, "z")
	wantCode(t, "replace without delete", err, protocol.FilesErrForbidden)
	if e.read("alice", "up/old.txt") != "12" {
		t.Error("failed replace must keep the old file")
	}
	e.noTemps("alice", "up")
}

func TestServiceUploadQuota(t *testing.T) {
	e := newSvcEnv(t)
	e.quota = 6
	if _, err := e.upload(uBob, "", "", "a.bin", 4, "", "abcd"); err != nil {
		t.Fatal(err)
	}
	_, err := e.upload(uBob, "", "", "b.bin", 4, "", "abcd")
	wantCode(t, "upload over the quota", err, protocol.FilesErrQuota)
	if e.exists("bob", "b.bin") || e.usage("bob") != 4 {
		t.Errorf("exists=%v usage=%d", e.exists("bob", "b.bin"), e.usage("bob"))
	}
	e.noTemps("bob", "")
}

func TestServiceWrite(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "docs/a.txt", "hello")
	e.write("alice", "docs/p.png", "x")
	// чтобы время изменения гарантированно отличалось после сохранения
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(e.abs("alice", "docs/a.txt"), old, old); err != nil {
		t.Fatal(err)
	}
	base := e.ok(uAlice, rq(protocol.FilesOpStat, "", "docs/a.txt")).Entry.ModTime

	r, err := e.save(uAlice, "", "docs/a.txt", 3, base, "new")
	if err != nil || r.Entry == nil || r.Entry.ModTime == base {
		t.Fatalf("save = %+v, %v", r, err)
	}
	if e.read("alice", "docs/a.txt") != "new" {
		t.Fatal("content was not saved")
	}

	// устаревшая версия: конфликт, файл не затронут
	_, err = e.save(uAlice, "", "docs/a.txt", 3, base, "zzz")
	wantCode(t, "stale save", err, protocol.FilesErrConflict)
	if e.read("alice", "docs/a.txt") != "new" {
		t.Error("conflicting save must not change the file")
	}

	// чужой файл: нужно право edit
	e.rule(uAlice, "", "docs", protocol.ActionView, protocol.ModeAll)
	_, err = e.save(uBob, "alice", "docs/a.txt", 3, 0, "abc")
	wantCode(t, "save without edit", err, protocol.FilesErrForbidden)
	e.rule(uAlice, "", "docs", protocol.ActionEdit, protocol.ModeAll)
	if _, err = e.save(uBob, "alice", "docs/a.txt", 3, 0, "abc"); err != nil || e.read("alice", "docs/a.txt") != "abc" {
		t.Errorf("save with edit: %v, content %q", err, e.read("alice", "docs/a.txt"))
	}

	_, err = e.save(uAlice, "", "docs/p.png", 1, 0, "y")
	wantCode(t, "save a non-txt file", err, protocol.FilesErrInvalidRequest)
	_, err = e.save(uAlice, "", "docs/a.txt", MaxEditBytes+1, 0, "")
	wantCode(t, "save over 10 MiB", err, protocol.FilesErrInvalidRequest)
	e.noTemps("alice", "docs")
}

func TestServiceWriteQuotaCountsOnlyTheGrowth(t *testing.T) {
	e := newSvcEnv(t)
	e.write("alice", "a.txt", "hello") // 5
	e.write("alice", "o.txt", "abcd")  // 4, всего 9
	e.quota = 10

	if _, err := e.save(uAlice, "", "a.txt", 6, 0, "abcdef"); err != nil {
		t.Fatalf("growth by 1 byte must fit: %v", err)
	}
	if e.usage("alice") != 10 {
		t.Errorf("usage = %d, want 10", e.usage("alice"))
	}
	_, err := e.save(uAlice, "", "a.txt", 7, 0, "abcdefg")
	wantCode(t, "growth over the quota", err, protocol.FilesErrQuota)
	if e.read("alice", "a.txt") != "abcdef" || e.usage("alice") != 10 {
		t.Error("failed save must change neither the file nor the usage")
	}
	if _, err = e.save(uAlice, "", "a.txt", 2, 0, "ab"); err != nil {
		t.Fatal(err)
	}
	if e.usage("alice") != 6 {
		t.Errorf("usage after shrinking = %d, want 6", e.usage("alice"))
	}
}
