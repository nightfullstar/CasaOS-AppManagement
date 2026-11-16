package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ReCasaOS/CasaOS-AppManagement/codegen"
	"github.com/ReCasaOS/CasaOS-AppManagement/common"
	"github.com/ReCasaOS/CasaOS-AppManagement/pkg/config"
	"github.com/ReCasaOS/CasaOS-Common/utils/file"
)

var storeIconFiles = []struct {
	ext         string
	contentType string
}{
	{".png", "image/png"},
	{".jpg", "image/jpeg"},
	{".webp", "image/webp"},
	{".svg", "image/svg+xml"},
}

// storeInfoList is the catalogue list for one store. Relative icons become the
// gateway path; ComposeApp.StoreInfo itself stays store-id-agnostic.
func storeInfoList(storeID int, catalog map[string]*ComposeApp) map[string]codegen.ComposeAppStoreInfo {
	list := make(map[string]codegen.ComposeAppStoreInfo, len(catalog))
	for appStoreID, composeApp := range catalog {
		info, err := composeApp.StoreInfo(true)
		if err != nil {
			continue
		}
		applyStoreAssets(info, storeID, composeApp.Name)
		list[appStoreID] = *info
	}
	return list
}

func applyStoreIcon(info *codegen.ComposeAppStoreInfo, storeID int, appID string) {
	if info == nil || info.Icon == "" || isAbsoluteHTTP(info.Icon) {
		return
	}
	info.Icon = fmt.Sprintf("/v2/app_management/appstore/%d/apps/%s/icon", storeID, appID)
}

func applyStoreScreenshots(info *codegen.ComposeAppStoreInfo, storeID int, appID string) {
	if info == nil {
		return
	}
	out := make([]string, len(info.ScreenshotLink))
	for i, link := range info.ScreenshotLink {
		if link == "" || isAbsoluteHTTP(link) {
			out[i] = link
			continue
		}
		name := filepath.Base(link)
		out[i] = fmt.Sprintf("/v2/app_management/appstore/%d/apps/%s/screenshot/%s", storeID, appID, name)
	}
	info.ScreenshotLink = out
}

func applyStoreAssets(info *codegen.ComposeAppStoreInfo, storeID int, appID string) {
	applyStoreIcon(info, storeID, appID)
	applyStoreScreenshots(info, storeID, appID)
}

func isAbsoluteHTTP(icon string) bool {
	lower := strings.ToLower(icon)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// ComposeStoreInfo is StoreInfo plus the gateway rewrite for a relative icon.
func (a *AppStoreManagement) ComposeStoreInfo(composeApp *ComposeApp, includeApps bool) (*codegen.ComposeAppStoreInfo, error) {
	info, err := composeApp.StoreInfo(includeApps)
	if err != nil {
		return nil, err
	}
	if id, ok := a.storeIDForApp(composeApp.Name); ok {
		applyStoreAssets(info, id, composeApp.Name)
	}
	return info, nil
}

func (a *AppStoreManagement) storeIDForApp(appID string) (int, bool) {
	found, id := false, 0
	for i, url := range config.ServerInfo.AppStoreList {
		store, err := AppStoreByURL(url)
		if err != nil {
			continue
		}
		app, err := store.ComposeApp(appID)
		if err != nil || app == nil {
			continue
		}
		found, id = true, i
	}
	return id, found
}

func (a *AppStoreManagement) storeWorkDir(storeID int) (string, error) {
	if storeID < 0 || storeID >= len(config.ServerInfo.AppStoreList) {
		return "", os.ErrNotExist
	}
	store, err := AppStoreByURL(config.ServerInfo.AppStoreList[storeID])
	if err != nil {
		return "", err
	}
	return store.WorkDir()
}

// IconFile is Apps/{app}/icon.{png|jpg|webp|svg} in that store's workdir.
func (a *AppStoreManagement) IconFile(storeID int, app string) (path, contentType string, err error) {
	workdir, err := a.storeWorkDir(storeID)
	if err != nil {
		return "", "", err
	}
	return FindStoreAppIcon(workdir, app)
}

// ScreenshotFile is Apps/{app}/{name} in that store's workdir.
func (a *AppStoreManagement) ScreenshotFile(storeID int, app, name string) (path, contentType string, err error) {
	workdir, err := a.storeWorkDir(storeID)
	if err != nil {
		return "", "", err
	}
	return FindStoreAppScreenshot(workdir, app, name)
}

// FindStoreAppIcon locates the first icon file for app under workdir.
func FindStoreAppIcon(workdir, app string) (path, contentType string, err error) {
	if app == "" || app == "." || app == ".." || strings.ContainsAny(app, `/\`) || filepath.Base(app) != app {
		return "", "", os.ErrNotExist
	}

	storeRoot, err := StoreRoot(workdir)
	if err != nil {
		return "", "", err
	}

	for _, icon := range storeIconFiles {
		p := filepath.Join(storeRoot, common.AppsDirectoryName, app, "icon"+icon.ext)
		if file.Exists(p) {
			return p, icon.contentType, nil
		}
	}
	return "", "", os.ErrNotExist
}

func storeImageContentType(ext string) string {
	ext = strings.ToLower(ext)
	if ext == ".jpeg" {
		return "image/jpeg"
	}
	for _, icon := range storeIconFiles {
		if icon.ext == ext {
			return icon.contentType
		}
	}
	return ""
}

func safeStoreName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`) && filepath.Base(name) == name
}

// FindStoreAppScreenshot locates a screenshot file for app under workdir.
func FindStoreAppScreenshot(workdir, app, name string) (path, contentType string, err error) {
	if !safeStoreName(app) || !safeStoreName(name) {
		return "", "", os.ErrNotExist
	}
	contentType = storeImageContentType(filepath.Ext(name))
	if contentType == "" {
		return "", "", os.ErrNotExist
	}

	storeRoot, err := StoreRoot(workdir)
	if err != nil {
		return "", "", err
	}

	p := filepath.Join(storeRoot, common.AppsDirectoryName, app, name)
	if file.Exists(p) {
		return p, contentType, nil
	}
	return "", "", os.ErrNotExist
}
