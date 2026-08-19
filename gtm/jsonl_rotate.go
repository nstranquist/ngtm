package gtm

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nstranquist/ngtm/internal/lockfile"
)

// DefaultJSONLMaxBytes is the hot-file cap. Past this, the live JSONL is
// renamed to path.<UTC stamp> so agents cannot rg an unbounded ledger into
// context. Rotations are kept. SQLite remains the full history.
const DefaultJSONLMaxBytes = 64 << 10 // 64 KiB hot window — small enough to survive an accidental rg

// JSONLMaxBytes returns the hot-file cap. 0 disables rotation.
func JSONLMaxBytes() int64 {
	raw := strings.TrimSpace(os.Getenv("NGTM_JSONL_MAX_BYTES"))
	if raw == "" {
		return DefaultJSONLMaxBytes
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return DefaultJSONLMaxBytes
	}
	return n
}

// appendAndRotateJSONL appends one line to the live shadow, then rotates if
// the hot file exceeded the cap. History lives in path.<stamp> next to it.
func appendAndRotateJSONL(path string, raw []byte) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return lockfile.WithFileLock(path, func() error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, werr := f.Write(append(append([]byte(nil), raw...), '\n'))
		cerr := f.Close()
		if werr != nil {
			return werr
		}
		if cerr != nil {
			return cerr
		}
		return rotateJSONLUnlocked(path)
	})
}

func rotateJSONLUnlocked(path string) error {
	max := JSONLMaxBytes()
	if max == 0 {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Size() <= max {
		return nil
	}
	dest := path + "." + time.Now().UTC().Format("20060102T150405Z")
	if err := os.Rename(path, dest); err != nil {
		return err
	}
	if err := gzipCompletedRotation(dest); err != nil {
		return err
	}
	// Keep the well-known path as an empty hot file so `rg runs.jsonl` does
	// not fail-open into a missing-file hunt across rotations.
	return os.WriteFile(path, nil, 0o644)
}

// gzipCompletedRotation writes src.gz and removes the uncompressed rotation.
// The .gz file is the kept history; the live hot file is never gzipped.
func gzipCompletedRotation(src string) error {
	if strings.HasSuffix(src, ".gz") {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(src + ".gz")
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(out)
	_, copyErr := io.Copy(zw, in)
	closeErr := zw.Close()
	outErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if outErr != nil {
		return outErr
	}
	return os.Remove(src)
}

// jsonlFamily is the live file plus any rotated siblings (path.*).
func jsonlFamily(live string) []string {
	if strings.TrimSpace(live) == "" {
		return nil
	}
	matches, _ := filepath.Glob(live + ".*")
	out := make([]string, 0, len(matches)+1)
	out = append(out, matches...)
	if _, err := os.Stat(live); err == nil {
		out = append(out, live)
	}
	return out
}

func jsonlFamilyFingerprint(live string) string {
	var b strings.Builder
	for _, p := range jsonlFamily(live) {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		b.WriteString(filepath.Base(p))
		b.WriteByte('=')
		b.WriteString(strconv.FormatInt(fi.Size(), 10))
		b.WriteByte('@')
		b.WriteString(strconv.FormatInt(fi.ModTime().UnixNano(), 10))
		b.WriteByte(';')
	}
	return b.String()
}

func jsonlHotBytes(live string) int64 {
	fi, err := os.Stat(live)
	if err != nil {
		return 0
	}
	return fi.Size()
}
