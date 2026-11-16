package v2

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ReCasaOS/CasaOS-AppManagement/codegen"
	"github.com/ReCasaOS/CasaOS-AppManagement/pkg/config"
	"github.com/ReCasaOS/CasaOS-AppManagement/service"
	"github.com/ReCasaOS/CasaOS-Common/utils/logger"
	"github.com/labstack/echo/v4"
)

type iconServices struct {
	service.Services
	store *service.AppStoreManagement
}

func (s iconServices) AppStoreManagement() *service.AppStoreManagement { return s.store }

func TestAppStoreIconServesTheWorkdirPNG(t *testing.T) {
	logger.LogInitConsoleOnly()

	workdir := t.TempDir()
	iconPath := filepath.Join(workdir, "Apps", "immich", "icon.png")
	if err := os.MkdirAll(filepath.Dir(iconPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(iconPath, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v2/app_management/appstore/0/apps/immich/icon", nil)
	ctx := echo.New().NewContext(req, rec)

	if err := serveAppStoreIcon(ctx, workdir, "immich"); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("want image/png, got %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "public, max-age=86400" {
		t.Fatalf("want cache header, got %q", rec.Header().Get("Cache-Control"))
	}
}

func TestAppStoreIconIs404WhenMissing(t *testing.T) {
	logger.LogInitConsoleOnly()

	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, "Apps", "immich"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v2/app_management/appstore/0/apps/immich/icon", nil)
	ctx := echo.New().NewContext(req, rec)

	if err := serveAppStoreIcon(ctx, workdir, "immich"); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAppStoreAppIconServesTheRegisteredStoreFile(t *testing.T) {
	logger.LogInitConsoleOnly()

	prevList, prevPath, prevService := config.ServerInfo.AppStoreList, config.AppInfo.AppStorePath, service.MyService
	config.AppInfo.AppStorePath = t.TempDir()
	config.ServerInfo.AppStoreList = []string{"https://icons.test/handler.zip"}
	service.MyService = iconServices{store: service.NewAppStoreManagement()}
	t.Cleanup(func() {
		config.ServerInfo.AppStoreList = prevList
		config.AppInfo.AppStorePath = prevPath
		service.MyService = prevService
	})

	store, err := service.AppStoreByURL(config.ServerInfo.AppStoreList[0])
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	workdir, err := store.WorkDir()
	if err != nil {
		t.Fatalf("workdir: %v", err)
	}
	iconPath := filepath.Join(workdir, "Apps", "immich", "icon.png")
	if err := os.MkdirAll(filepath.Dir(iconPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(iconPath, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v2/app_management/appstore/0/apps/immich/icon", nil)
	ctx := echo.New().NewContext(req, rec)
	if err := (&AppManagement{}).AppStoreAppIcon(ctx, 0, "immich", codegen.AppStoreAppIconParams{}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("want image/png, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestAppStoreScreenshotServesTheWorkdirPNG(t *testing.T) {
	logger.LogInitConsoleOnly()

	workdir := t.TempDir()
	shotPath := filepath.Join(workdir, "Apps", "immich", "screenshot-1.png")
	if err := os.MkdirAll(filepath.Dir(shotPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shotPath, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v2/app_management/appstore/0/apps/immich/screenshot/screenshot-1.png", nil)
	ctx := echo.New().NewContext(req, rec)

	path, contentType, err := service.FindStoreAppScreenshot(workdir, "immich", "screenshot-1.png")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if err := sendAppStoreAsset(ctx, path, contentType, "screenshot not found"); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("want image/png, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestAppStoreAppScreenshotServesTheRegisteredStoreFile(t *testing.T) {
	logger.LogInitConsoleOnly()

	prevList, prevPath, prevService := config.ServerInfo.AppStoreList, config.AppInfo.AppStorePath, service.MyService
	config.AppInfo.AppStorePath = t.TempDir()
	config.ServerInfo.AppStoreList = []string{"https://icons.test/shot-handler.zip"}
	service.MyService = iconServices{store: service.NewAppStoreManagement()}
	t.Cleanup(func() {
		config.ServerInfo.AppStoreList = prevList
		config.AppInfo.AppStorePath = prevPath
		service.MyService = prevService
	})

	store, err := service.AppStoreByURL(config.ServerInfo.AppStoreList[0])
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	workdir, err := store.WorkDir()
	if err != nil {
		t.Fatalf("workdir: %v", err)
	}
	shotPath := filepath.Join(workdir, "Apps", "immich", "screenshot-1.png")
	if err := os.MkdirAll(filepath.Dir(shotPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shotPath, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v2/app_management/appstore/0/apps/immich/screenshot/screenshot-1.png", nil)
	ctx := echo.New().NewContext(req, rec)
	if err := (&AppManagement{}).AppStoreAppScreenshot(ctx, 0, "immich", "screenshot-1.png", codegen.AppStoreAppScreenshotParams{}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("want image/png, got %q", rec.Header().Get("Content-Type"))
	}
}
