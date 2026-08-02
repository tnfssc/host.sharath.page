package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math/big"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	idLength    = 5
	idAlphabet  = "abcdefghijklmnopqrstuvwxyz0123456789"
	blobName    = "blob"
	metaName    = "meta.json"
	maxBaseLen  = 60
	maxFileExt  = 11 // leading dot + up to 10 chars
	orphanGrace = time.Hour
)

// Meta describes one stored file. Stored as <data>/<id>/meta.json next to
// the content in <data>/<id>/blob.
type Meta struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Uploader    string    `json:"uploader"`
	UploadedAt  time.Time `json:"uploaded_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (m Meta) Expired(now time.Time) bool { return !now.Before(m.ExpiresAt) }

type Store struct{ dir string }

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func validID(id string) bool {
	if len(id) != idLength {
		return false
	}
	for _, c := range id {
		if !strings.ContainsRune(idAlphabet, c) {
			return false
		}
	}
	return true
}

func randomID() (string, error) {
	var b strings.Builder
	b.Grow(idLength)
	for b.Len() < idLength {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(idAlphabet))))
		if err != nil {
			return "", err
		}
		b.WriteByte(idAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// createDir reserves a fresh collision-free ID directory.
func (s *Store) createDir() (id, dir string, err error) {
	for range 16 {
		id, err = randomID()
		if err != nil {
			return "", "", err
		}
		dir = filepath.Join(s.dir, id)
		if err = os.Mkdir(dir, 0o755); err == nil {
			return id, dir, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", "", err
		}
	}
	return "", "", errors.New("could not allocate a free id")
}

// writeBlob streams r into a temp file inside dir, atomically renames it to
// blob, and returns the size and the first bytes for content sniffing.
// Aborts with errTooLarge once max+1 bytes have been read.
var errTooLarge = errors.New("file exceeds maximum upload size")

func writeBlob(dir string, r io.Reader, max int64) (size int64, head []byte, err error) {
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		tmp.Close()
		if err != nil {
			os.Remove(tmp.Name())
		}
	}()

	limited := &io.LimitedReader{R: r, N: max + 1}
	head = make([]byte, 512)
	n, _ := io.ReadFull(limited, head) // io.EOF / ErrUnexpectedEOF are fine
	head = head[:n]

	written, err := tmp.Write(head)
	if err != nil {
		return 0, nil, err
	}
	copied, err := io.Copy(tmp, limited)
	size = int64(written) + copied
	if err != nil {
		return 0, nil, err
	}
	if size > max {
		return 0, nil, errTooLarge
	}
	if err := tmp.Close(); err != nil {
		return 0, nil, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, blobName)); err != nil {
		return 0, nil, err
	}
	return size, head, nil
}

func (s *Store) saveMeta(dir string, m *Meta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, metaName), data, 0o644)
}

func (s *Store) Load(id string) (*Meta, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, id, metaName))
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) BlobPath(id string) string { return filepath.Join(s.dir, id, blobName) }

func (s *Store) Delete(id string) { _ = os.RemoveAll(filepath.Join(s.dir, id)) }

// Sweep removes expired files and orphaned directories (failed uploads whose
// newest entry is older than orphanGrace).
func (s *Store) Sweep(now time.Time) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !validID(e.Name()) {
			continue
		}
		dir := filepath.Join(s.dir, e.Name())
		meta, err := s.Load(e.Name())
		if err == nil {
			if meta.Expired(now) {
				_ = os.RemoveAll(dir)
			}
			continue
		}
		// No readable meta.json: orphan. Delete only once the newest entry
		// (e.g. an in-progress .upload-* temp file) has been quiet for a while.
		newest := now.Add(-2 * orphanGrace)
		if info, err := e.Info(); err == nil {
			newest = info.ModTime()
		}
		if sub, err := os.ReadDir(dir); err == nil {
			for _, f := range sub {
				if info, err := f.Info(); err == nil && info.ModTime().After(newest) {
					newest = info.ModTime()
				}
			}
		}
		if now.Sub(newest) > orphanGrace {
			_ = os.RemoveAll(dir)
		}
	}
}

// sanitizeFilename turns an arbitrary client-supplied name into a clean
// single path segment: [A-Za-z0-9._-], base truncated, extension kept.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimSpace(path.Base(name))

	ext := strings.ToLower(path.Ext(name))
	base := strings.TrimSuffix(name, path.Ext(name))

	var b strings.Builder
	lastDash := false
	for _, r := range base {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == '-'
		switch {
		case ok:
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	clean := strings.Trim(b.String(), "-.")
	if len(clean) > maxBaseLen {
		clean = strings.TrimRight(clean[:maxBaseLen], "-.")
	}
	if clean == "" {
		clean = "file"
	}

	var eb strings.Builder
	for _, r := range strings.TrimPrefix(ext, ".") {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			eb.WriteRune(r)
		}
	}
	ext = eb.String()
	if len(ext) > maxFileExt-1 {
		ext = ext[:maxFileExt-1]
	}
	if ext != "" {
		ext = "." + ext
	}
	return clean + ext
}

// knownTypes covers common types without relying on /etc/mime.types, which is
// absent in the distroless container image.
var knownTypes = map[string]string{
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	".mkv":  "video/x-matroska",
	".avi":  "video/x-msvideo",
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".flac": "audio/flac",
	".txt":  "text/plain; charset=utf-8",
	".log":  "text/plain; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".htm":  "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript",
	".mjs":  "text/javascript",
	".json": "application/json",
	".csv":  "text/csv; charset=utf-8",
	".xml":  "text/xml; charset=utf-8",
	".yaml": "text/yaml; charset=utf-8",
	".yml":  "text/yaml; charset=utf-8",
	".pdf":  "application/pdf",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",
	".zip":  "application/zip",
	".gz":   "application/gzip",
	".tar":  "application/x-tar",
	".7z":   "application/x-7z-compressed",
	".wasm": "application/wasm",
}

// contentTypeFor resolves the served Content-Type from the extension, Go's
// mime table, then content sniffing, defaulting to a binary download type.
func contentTypeFor(filename string, head []byte) string {
	ext := strings.ToLower(path.Ext(filename))
	if ct, ok := knownTypes[ext]; ok {
		return ct
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	if len(head) > 0 {
		return http.DetectContentType(head)
	}
	return "application/octet-stream"
}

// parseDuration accepts Go durations ("72h", "30m") plus a day suffix ("3d").
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// resolveTTL picks the effective TTL: request override, default, capped at max.
func resolveTTL(req string, def, max time.Duration) time.Duration {
	d := def
	if req != "" {
		if v, err := parseDuration(req); err == nil && v > 0 {
			d = v
		}
	}
	if d > max {
		d = max
	}
	return d
}
