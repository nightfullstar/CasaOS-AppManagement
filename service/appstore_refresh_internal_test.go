package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ReCasaOS/CasaOS-AppManagement/common"
	"github.com/ReCasaOS/CasaOS-AppManagement/pkg/config"
	"github.com/ReCasaOS/CasaOS-Common/utils/logger"
)

const dummyInZipSHA = "0000000000000000000000000000000000000000000000000000000000000000"
const badIndexSHA = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

func TestRefreshDownloadsWhenContentLengthIsUnchangedButETagDiffers(t *testing.T) {
	logger.LogInitConsoleOnly()
	zipped := catalogZip(t, "", "alpha", "beta")
	var downloads int
	srv := serveCatalogue(t, zipped, `"etag-new"`, "", &downloads)
	defer srv.Close()

	s := newRefreshStore(t, srv.URL+"/store/recasaos.zip")
	s.lastETag = `"etag-old"`
	s.lastAPPStoreSize = int64(len(zipped))

	if err := s.UpdateCatalog(); err != nil {
		t.Fatalf("update: %v", err)
	}
	if downloads == 0 {
		t.Fatal("same Content-Length with a different ETag must still download")
	}
}

func TestRefreshSkipsWhenETagMatches(t *testing.T) {
	logger.LogInitConsoleOnly()
	zipped := catalogZip(t, "", "alpha", "beta")
	var downloads int
	srv := serveCatalogue(t, zipped, `"etag-same"`, "", &downloads)
	defer srv.Close()

	s := newRefreshStore(t, srv.URL+"/store/recasaos.zip")
	s.lastETag = `"etag-same"`
	// Content-Length differs on purpose: skip must be ETag, not size.
	s.lastAPPStoreSize = int64(len(zipped)) + 99

	if err := s.UpdateCatalog(); err != nil {
		t.Fatalf("update: %v", err)
	}
	if downloads != 0 {
		t.Fatalf("same ETag must not download, got %d", downloads)
	}
}

func TestRefreshSkipsWhenLastSHA256MatchesSiblingIndex(t *testing.T) {
	logger.LogInitConsoleOnly()
	zipped := catalogZip(t, dummyInZipSHA, "alpha", "beta")
	sum := sha256Hex(zipped)
	var downloads int
	srv := serveCatalogue(t, zipped, `"etag-new"`, sum, &downloads)
	defer srv.Close()

	s := newRefreshStore(t, srv.URL+"/store/recasaos.zip")
	s.lastETag = `"etag-old"`
	s.lastSHA256 = sum

	if err := s.UpdateCatalog(); err != nil {
		t.Fatalf("update: %v", err)
	}
	if downloads != 0 {
		t.Fatalf("matching sibling index digest must skip the zip GET, got %d", downloads)
	}
}

func TestRefreshAcceptsWhenExternalIndexHashMatches(t *testing.T) {
	logger.LogInitConsoleOnly()
	zipped := catalogZip(t, dummyInZipSHA, "alpha", "beta")
	sum := sha256Hex(zipped)
	var downloads int
	srv := serveCatalogue(t, zipped, `"etag-ok"`, sum, &downloads)
	defer srv.Close()

	s := newRefreshStore(t, srv.URL+"/store/recasaos.zip")
	if err := s.UpdateCatalog(); err != nil {
		t.Fatalf("matching sibling index must accept the catalog: %v", err)
	}
	if downloads == 0 {
		t.Fatal("want a zip GET when lastSHA256 is empty")
	}
	if got := len(s.catalog); got != 2 {
		t.Fatalf("catalog len=%d, want 2", got)
	}
	if !strings.EqualFold(s.lastSHA256, sum) {
		t.Fatalf("lastSHA256=%s, want zip digest %s", s.lastSHA256, sum)
	}
}

func TestRefreshKeepsCatalogWhenStoreIndexHashMismatches(t *testing.T) {
	logger.LogInitConsoleOnly()
	zipped := catalogZip(t, dummyInZipSHA, "alpha", "beta")
	var downloads int
	srv := serveCatalogue(t, zipped, `"etag-bad-hash"`, badIndexSHA, &downloads)
	defer srv.Close()

	s := newRefreshStore(t, srv.URL+"/store/recasaos.zip")
	workdir := seedWorkdir(t, s)

	err := s.UpdateCatalog()
	if err == nil {
		t.Fatal("want hash mismatch to refuse the catalog")
	}
	mustExist(t, filepath.Join(workdir, "KEEP"))
	mustExist(t, filepath.Join(workdir, common.AppsDirectoryName, "legacy", common.ComposeYAMLFileName))
}

func TestRefreshRefusesWhenAppCountShrinks(t *testing.T) {
	logger.LogInitConsoleOnly()
	zipped := catalogZip(t, "", "only")
	var downloads int
	srv := serveCatalogue(t, zipped, `"etag-shrink"`, "", &downloads)
	defer srv.Close()

	s := newRefreshStore(t, srv.URL+"/store/recasaos.zip")
	s.lastAppCount = 10
	s.minAppCount = 2
	workdir := seedWorkdir(t, s)

	err := s.UpdateCatalog()
	if err == nil {
		t.Fatal("want a 1-app catalog to be refused when lastAppCount is 10")
	}
	mustExist(t, filepath.Join(workdir, "KEEP"))
	mustExist(t, filepath.Join(workdir, common.AppsDirectoryName, "legacy", common.ComposeYAMLFileName))
}

func TestRefreshDoesNotKeepATinyFirstCatalog(t *testing.T) {
	logger.LogInitConsoleOnly()
	zipped := catalogZip(t, "", "only")
	var downloads int
	srv := serveCatalogue(t, zipped, `"etag-first-tiny"`, "", &downloads)
	defer srv.Close()

	s := newRefreshStore(t, srv.URL+"/store/recasaos.zip")
	workdir, err := s.WorkDir()
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateCatalog(); err == nil {
		t.Fatal("want a 1-app first fetch to be refused")
	}
	if _, err := os.Stat(workdir); !os.IsNotExist(err) {
		t.Fatalf("tiny first catalog must not stick at %s: %v", workdir, err)
	}
}

func TestRefreshDoesNotResumeAStaleZip(t *testing.T) {
	logger.LogInitConsoleOnly()
	zipped := catalogZip(t, "", "alpha", "beta")
	var downloads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, storeIndexFile) || strings.HasSuffix(r.URL.Path, ".sha256") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `"etag-stale"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(zipped)))
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Header.Get("Range") != "" {
			t.Errorf("stale workdir.zip must not be range-resumed, Range=%s", r.Header.Get("Range"))
		}
		downloads++
		_, _ = w.Write(zipped)
	}))
	defer srv.Close()

	s := newRefreshStore(t, srv.URL+"/store/recasaos.zip")
	workdir, err := s.WorkDir()
	if err != nil {
		t.Fatal(err)
	}
	zipPath := workdir + ".zip"
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := bytes.Repeat([]byte("X"), len(zipped)+64)
	if err := os.WriteFile(zipPath, stale, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateCatalog(); err != nil {
		t.Fatalf("leftover workdir.zip must not be resumed: %v", err)
	}
	if downloads == 0 {
		t.Fatal("want a full zip GET, not a skip of an existing workdir.zip")
	}
	if got := len(s.catalog); got != 2 {
		t.Fatalf("catalog len=%d, want 2", got)
	}
}

func newRefreshStore(t *testing.T, storeURL string) *appStore {
	t.Helper()
	prev := config.AppInfo.AppStorePath
	config.AppInfo.AppStorePath = t.TempDir()
	t.Cleanup(func() { config.AppInfo.AppStorePath = prev })
	return &appStore{
		url:         storeURL,
		catalog:     map[string]*ComposeApp{},
		minAppCount: 2,
	}
}

func serveCatalogue(t *testing.T, zipped []byte, etag, indexSHA string, downloads *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, storeIndexFile) {
			if indexSHA == "" {
				http.NotFound(w, r)
				return
			}
			body := fmt.Sprintf(`{"schema":"recasaos.store-index.v1","sha256":"%s"}`, indexSHA)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
			_, _ = fmt.Fprint(w, body)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Length", strconv.Itoa(len(zipped)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		*downloads++
		_, _ = w.Write(zipped)
	}))
}

func catalogZip(t *testing.T, indexSHA string, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := zw.Create(filepath.ToSlash(filepath.Join(common.AppsDirectoryName, name, common.ComposeYAMLFileName)))
		if err != nil {
			t.Fatal(err)
		}
		yaml := strings.Replace(common.SampleComposeAppYAML, "name: syncthing", "name: "+name, 1)
		if _, err := w.Write([]byte(yaml)); err != nil {
			t.Fatal(err)
		}
	}
	if indexSHA != "" {
		w, err := zw.Create(storeIndexFile)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(w, `{"schema":"recasaos.store-index.v1","sha256":"%s"}`, indexSHA); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func seedWorkdir(t *testing.T, s *appStore) string {
	t.Helper()
	workdir, err := s.WorkDir()
	if err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(workdir, common.AppsDirectoryName, "legacy")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := strings.Replace(common.SampleComposeAppYAML, "name: syncthing", "name: legacy", 1)
	if err := os.WriteFile(filepath.Join(appDir, common.ComposeYAMLFileName), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "KEEP"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	return workdir
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("want %s still present: %v", path, err)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
