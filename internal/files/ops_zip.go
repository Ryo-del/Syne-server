package files

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"strings"

	protocol "github.com/Ryo-del/Syne-protocol"
)

// openRegular открывает обычный файл и проверяет, что по пути лежит именно он
// (путь не успели подменить ссылкой между проверкой и открытием).
func openRegular(abs string) (*os.File, os.FileInfo, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	li, lerr := os.Lstat(abs)
	if err != nil || lerr != nil || !fi.Mode().IsRegular() || !os.SameFile(fi, li) {
		_ = f.Close()
		return nil, nil, os.ErrNotExist
	}
	return f, fi, nil
}

// DirZip — подготовленный архив папки: список файлов уже проверен по правам.
type DirZip struct {
	Skipped  int // сколько элементов не вошло из-за прав
	rootName string
	srcRel   string
	nodes    []node
}

// PrepareDirZip проверяет права и собирает список файлов. Для чужой папки
// нужно право download на ней; в архив попадают только файлы, которые
// пользователь видит и может скачать. Entry.Name — предлагаемое имя файла.
func (s *Service) PrepareDirZip(actor Actor, req protocol.FilesRequest) (*DirZip, protocol.FileEntry, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return nil, protocol.FileEntry{}, err
	}
	if !t.info.IsDir() {
		return nil, protocol.FileEntry{}, invalid("not a folder")
	}
	priv := canManage(actor, t.owner.Login)
	if !priv {
		if err := s.require(t, actor, protocol.ActionDownload); err != nil {
			return nil, protocol.FileEntry{}, err
		}
	}
	need := []protocol.Action{protocol.ActionView, protocol.ActionDownload}
	nodes, skipped, err := s.gather(actor, t.eng, t.owner.Login, priv, t.rel, t.abs, t.info, need, false)
	if err != nil {
		return nil, protocol.FileEntry{}, err
	}

	root := Base(t.rel)
	if t.rel == "" {
		root = zipSafeName(t.owner.DisplayName())
	}
	entry := protocol.FileEntry{
		Name:      root + ".zip",
		Path:      t.rel,
		Owner:     t.owner.Login,
		OwnerName: t.owner.DisplayName(),
	}
	return &DirZip{Skipped: skipped, rootName: root, srcRel: t.rel, nodes: nodes}, entry, nil
}

// zipSafeName делает из «Фамилия Имя» безопасное имя папки внутри архива.
func zipSafeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(strings.TrimSpace(name), ".")
	if name == "" {
		return "folder"
	}
	return name
}

// WriteZip пишет архив потоком (без временных файлов). Файл, исчезнувший
// во время архивации, пропускается; любая другая ошибка прерывает архив, и
// вызывающий обязан сбросить соединение: оборванный архив нельзя выдавать за целый.
func (d *DirZip) WriteZip(w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, n := range d.nodes {
		sub := strings.TrimPrefix(strings.TrimPrefix(n.rel, d.srcRel), "/")
		name := d.rootName
		if sub != "" {
			name += "/" + sub
		}
		info, err := os.Lstat(n.abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if !okKind(info) {
			continue
		}
		hdr := &zip.FileHeader{Name: name, Modified: info.ModTime()}

		if n.isDir {
			hdr.Name += "/"
			if _, err := zw.CreateHeader(hdr); err != nil {
				return err
			}
			continue
		}

		f, _, err := openRegular(n.abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		hdr.Method = zip.Deflate
		fw, err := zw.CreateHeader(hdr)
		if err != nil {
			_ = f.Close()
			return err
		}
		_, err = io.Copy(fw, io.LimitReader(f, MaxFileBytes))
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return zw.Close()
}
