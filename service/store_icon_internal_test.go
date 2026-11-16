package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ReCasaOS/CasaOS-AppManagement/common"
	"github.com/ReCasaOS/CasaOS-AppManagement/pkg/config"
	"github.com/ReCasaOS/CasaOS-Common/utils/logger"
)

const relativeIconYAML = `name: immich
services:
  immich:
    image: test/immich:1
x-casaos:
  icon: icon.png
  title:
    en_us: Immich
  main: immich
`

const httpsIconYAML = `name: syncthing
services:
  syncthing:
    image: test/syncthing:1
x-casaos:
  icon: https://cdn.example/icon.png
  title:
    en_us: Syncthing
  main: syncthing
`

func TestRelativeIconBecomesTheGatewayPathWhenTheListIsBuilt(t *testing.T) {
	logger.LogInitConsoleOnly()

	app, err := NewComposeAppFromYAML([]byte(relativeIconYAML), true, false)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	raw, err := app.StoreInfo(false)
	if err != nil {
		t.Fatalf("StoreInfo: %v", err)
	}
	if raw.Icon != "icon.png" {
		t.Fatalf("ComposeApp.StoreInfo must stay store-id-agnostic, got %q", raw.Icon)
	}

	list := storeInfoList(0, map[string]*ComposeApp{app.Name: app})
	got := list[app.Name].Icon
	want := "/v2/app_management/appstore/0/apps/immich/icon"
	if got != want {
		t.Fatalf("relative icon.png must become the gateway path, got %q", got)
	}
}

func TestComposeStoreInfoRewritesWhenTheStoreIsIdZero(t *testing.T) {
	logger.LogInitConsoleOnly()

	prevList, prevPath := config.ServerInfo.AppStoreList, config.AppInfo.AppStorePath
	config.AppInfo.AppStorePath = t.TempDir()
	config.ServerInfo.AppStoreList = []string{"https://icons.test/store.zip"}
	t.Cleanup(func() {
		config.ServerInfo.AppStoreList = prevList
		config.AppInfo.AppStorePath = prevPath
	})

	app, err := NewComposeAppFromYAML([]byte(relativeIconYAML), true, false)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	store, err := AppStoreByURL(config.ServerInfo.AppStoreList[0])
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	store.(*appStore).catalog = map[string]*ComposeApp{app.Name: app}

	info, err := NewAppStoreManagement().ComposeStoreInfo(app, false)
	if err != nil {
		t.Fatalf("ComposeStoreInfo: %v", err)
	}
	want := "/v2/app_management/appstore/0/apps/immich/icon"
	if info.Icon != want {
		t.Fatalf("got %q want %q", info.Icon, want)
	}
}

func TestIconFileFindsPNGInTheStoreWorkdir(t *testing.T) {
	logger.LogInitConsoleOnly()

	prevList, prevPath := config.ServerInfo.AppStoreList, config.AppInfo.AppStorePath
	config.AppInfo.AppStorePath = t.TempDir()
	config.ServerInfo.AppStoreList = []string{"https://icons.test/icon-file.zip"}
	t.Cleanup(func() {
		config.ServerInfo.AppStoreList = prevList
		config.AppInfo.AppStorePath = prevPath
	})

	store, err := AppStoreByURL(config.ServerInfo.AppStoreList[0])
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	workdir, err := store.WorkDir()
	if err != nil {
		t.Fatalf("workdir: %v", err)
	}
	iconPath := filepath.Join(workdir, common.AppsDirectoryName, "immich", "icon.png")
	if err := os.MkdirAll(filepath.Dir(iconPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(iconPath, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, contentType, err := NewAppStoreManagement().IconFile(0, "immich")
	if err != nil {
		t.Fatalf("IconFile: %v", err)
	}
	if path != iconPath {
		t.Fatalf("got path %q want %q", path, iconPath)
	}
	if contentType != "image/png" {
		t.Fatalf("got %q", contentType)
	}

	if _, _, err := NewAppStoreManagement().IconFile(0, "missing"); err == nil {
		t.Fatal("missing app must 404")
	}
}

func TestAbsoluteIconStaysOnTheList(t *testing.T) {
	logger.LogInitConsoleOnly()

	app, err := NewComposeAppFromYAML([]byte(httpsIconYAML), true, false)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	list := storeInfoList(0, map[string]*ComposeApp{app.Name: app})
	if list[app.Name].Icon != "https://cdn.example/icon.png" {
		t.Fatalf("https icon must stay, got %q", list[app.Name].Icon)
	}
}
