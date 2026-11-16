package service

import (
	"crypto/md5" // nolint: gosec
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ReCasaOS/CasaOS-AppManagement/codegen"
	"github.com/ReCasaOS/CasaOS-AppManagement/common"
	"github.com/ReCasaOS/CasaOS-AppManagement/pkg/config"
	"github.com/ReCasaOS/CasaOS-AppManagement/pkg/utils/downloadHelper"
	"github.com/ReCasaOS/CasaOS-Common/utils/file"
	"github.com/ReCasaOS/CasaOS-Common/utils/logger"
	"github.com/samber/lo"
	"go.uber.org/zap"
)

type AppStore interface {
	Catalog() (map[string]*ComposeApp, error)
	CategoryMap() (map[string]codegen.CategoryInfo, error)
	ComposeApp(id string) (*ComposeApp, error)
	Recommend() ([]string, error)
	UpdateCatalog() error
	WorkDir() (string, error)
}

type appStore struct {
	categoryMap map[string]codegen.CategoryInfo
	catalog     map[string]*ComposeApp
	recommend   []string
	url         string

	lastAPPStoreSize int64
	lastETag         string
	lastSHA256       string
	lastAppCount     int
	// minAppCount is the floor below which a new catalog is refused. Zero
	// means the production floor (250) for the ReCasaOS catalogue and no
	// absolute floor for other stores. Tests inject a smaller value.
	minAppCount int
	// source is the URL the last update fetched: the catalogue, or this
	// distribution's copy of it when the catalogue did not answer.
	source string
}

const (
	minCatalogueApps  = 250
	minCatalogueRatio = 0.9
	storeIndexFile    = "store-index.json"
)

var (
	appStoreMap = make(map[string]*appStore)

	ErrNotAppStore             = fmt.Errorf("not an appstore")
	ErrDefaultAppStoreNotFound = fmt.Errorf("default appstore not found")
	ErrCatalogTooSmall         = fmt.Errorf("catalog is below the size floor")
	ErrStoreIndexHashMismatch  = fmt.Errorf("store-index sha256 does not match the zip")
)

func (s *appStore) CategoryMap() (map[string]codegen.CategoryInfo, error) {
	if s.categoryMap != nil {
		return s.categoryMap, nil
	}

	workdir, err := s.WorkDir()
	if err != nil {
		return nil, err
	}

	storeRoot, err := StoreRoot(workdir)
	if err != nil {
		return nil, err
	}

	categoryMap := LoadCategoryMap(storeRoot)

	s.categoryMap = categoryMap

	return s.categoryMap, nil
}

func (s *appStore) UpdateCatalog() error {
	isSuccessful := false

	if _, err := url.Parse(s.url); err != nil {
		return err
	}

	if _, dead := SuccessorOf(s.url); dead {
		logger.Info("appstore URL is a known-dead source; keeping last-good", zap.String("url", s.url))
		return nil
	}

	http.DefaultClient.Timeout = 5 * time.Second
	// the catalogue, or this distribution's copy of it when the catalogue
	// does not answer (appstore_mirror.go)
	source, res, err := pickCatalogueSource(s.url, http.Head)
	if err != nil {
		return err
	}
	if res != nil && res.Body != nil {
		defer res.Body.Close()
	}

	etag := res.Header.Get("ETag")
	if etag != "" && etag == s.lastETag {
		logger.Info("appstore etag not changed", zap.String("url", source))
		return nil
	}

	// The zip's sha256 lives on the sibling index (or recasaos.zip.sha256),
	// not inside the zip: a sha256-of-containing-zip cannot be a field of
	// a file packed into that zip. An in-zip store-index.json is ignored.
	expected, hasIndex := fetchIndexSHA256(source)
	if s.lastSHA256 != "" && hasIndex && strings.EqualFold(expected, s.lastSHA256) {
		logger.Info("appstore index sha256 not changed", zap.String("url", source))
		return nil
	}
	logger.Info("appstore changed, update app store", zap.String("url", source))
	s.source = source

	workdir, err := s.WorkDir()
	if err != nil {
		return err
	}
	tmpDir := workdir + ".tmp"
	zipPath := workdir + ".zip"

	defer func() {
		if err := file.RMDir(tmpDir); err != nil {
			logger.Error("failed to remove temp appstore workdir", zap.Error(err), zap.String("tmpDir", tmpDir))
		}
		if err := os.Remove(zipPath); err != nil && !os.IsNotExist(err) {
			logger.Error("failed to remove temp appstore zip", zap.Error(err), zap.String("zipPath", zipPath))
		}
	}()

	// HttpGetter.GetFile range-resumes into dst when the server advertises
	// Accept-Ranges. A leftover workdir.zip from a previous attempt would
	// be treated as a prefix of this catalogue.
	if err := os.Remove(zipPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	if err := downloadHelper.DownloadToFile(s.source, zipPath); err != nil {
		return err
	}

	sum, err := sha256File(zipPath)
	if err != nil {
		return err
	}
	if err := verifyZipSHA256(source, sum, expected, hasIndex); err != nil {
		return err
	}

	if err := downloadHelper.UnpackZip(zipPath, tmpDir); err != nil {
		return err
	}

	tmpRoot, err := StoreRoot(tmpDir)
	if err != nil {
		return err
	}

	catalog, err := BuildCatalog(tmpRoot)
	if err != nil {
		return err
	}
	if err := s.catalogTooSmall(len(catalog)); err != nil {
		return err
	}

	placeholderFile := filepath.Join(tmpRoot, ".casaos-appstore")
	if err := file.CreateFileAndWriteContent(placeholderFile, s.url); err != nil {
		return err
	}

	// make a backup of existing workdir
	if file.Exists(workdir) {
		backupDir := workdir + ".backup"

		if err := file.RMDir(backupDir); err != nil {
			return err
		}

		if err := os.Rename(workdir, backupDir); err != nil {
			return err
		}

		defer func() {
			if isSuccessful {
				if err := file.RMDir(backupDir); err != nil {
					logger.Error("failed to remove backup appstore workdir", zap.Error(err), zap.String("backupDir", backupDir))
				}
				return
			}

			if err := file.RMDir(workdir); err != nil {
				logger.Error("failed to remove appstore workdir", zap.Error(err), zap.String("workdir", workdir))
			}

			if err := os.Rename(backupDir, workdir); err != nil {
				logger.Error("failed to restore backup appstore workdir", zap.Error(err), zap.String("backupDir", backupDir), zap.String("workdir", workdir))
			}
		}()
	}

	if err := os.Rename(tmpDir, workdir); err != nil {
		return err
	}

	storeRoot, err := StoreRoot(workdir)
	if err != nil {
		return err
	}

	s.catalog = catalog
	s.categoryMap = LoadCategoryMap(storeRoot)
	s.recommend = LoadRecommend(storeRoot)

	s.lastETag = etag
	s.lastSHA256 = sum
	s.lastAppCount = len(s.catalog)
	s.lastAPPStoreSize = res.ContentLength
	isSuccessful = true

	return nil
}

func (s *appStore) Recommend() ([]string, error) {
	if s.recommend != nil && len(s.recommend) > 0 {
		return s.recommend, nil
	}

	workdir, err := s.WorkDir()
	if err != nil {
		return nil, err
	}

	storeRoot, err := StoreRoot(workdir)
	if err != nil {
		return nil, err
	}

	return LoadRecommend(storeRoot), nil
}

func (s *appStore) Catalog() (map[string]*ComposeApp, error) {
	if s.catalog != nil && len(s.catalog) > 0 {
		return s.catalog, nil
	}

	workdir, err := s.WorkDir()
	if err != nil {
		return nil, err
	}

	storeRoot, err := StoreRoot(workdir)
	if err != nil {
		return nil, err
	}

	catalog, err := BuildCatalog(storeRoot)
	if err != nil {
		return nil, err
	}

	s.catalog = catalog

	return s.catalog, nil
}

func (s *appStore) ComposeApp(appStoreID string) (*ComposeApp, error) {
	catalog, err := s.Catalog()
	if err != nil {
		return nil, err
	}

	if composeApp, ok := catalog[appStoreID]; ok {
		return composeApp, nil
	}

	return nil, nil
}

func (s *appStore) WorkDir() (string, error) {
	if s.url == "default" {
		return filepath.Join(config.AppInfo.AppStorePath, s.url), nil
	}

	parsedURL, err := url.Parse(s.url)
	if err != nil {
		return "", err
	}

	appstoreKey := strings.ToLower(parsedURL.Path)

	hash := fmt.Sprintf("%x", md5.Sum([]byte(appstoreKey))) //nolint: gosec

	return filepath.Join(config.AppInfo.AppStorePath, parsedURL.Host, hash), nil
}

func AppStoreByURL(appstoreURL string) (AppStore, error) {
	_, err := url.Parse(appstoreURL)
	if err != nil {
		return nil, err
	}

	// a appstoreKey is a normalized appstore url where everything is in lowercase
	appstoreKey := strings.ToLower(appstoreURL)
	if appstore, ok := appStoreMap[appstoreKey]; ok {
		return appstore, nil
	}

	appStoreMap[appstoreKey] = &appStore{
		url:     appstoreURL,
		catalog: map[string]*ComposeApp{},
	}

	return appStoreMap[appstoreKey], nil
}

func NewDefaultAppStore() (AppStore, error) {
	storeRoot := filepath.Join(config.AppInfo.AppStorePath, "default")

	if !file.Exists(storeRoot) {
		return nil, ErrDefaultAppStoreNotFound
	}

	categoryMap := LoadCategoryMap(storeRoot)

	catalog, err := BuildCatalog(storeRoot)
	if err != nil {
		return nil, err
	}

	recommend := LoadRecommend(storeRoot)

	return &appStore{
		url:         "default",
		categoryMap: categoryMap,
		catalog:     catalog,
		recommend:   recommend,
	}, nil
}

func LoadCategoryMap(storeRoot string) map[string]codegen.CategoryInfo {
	categoryListFile := filepath.Join(storeRoot, common.CategoryListFileName)

	// unmarsal category list
	categoryList := []codegen.CategoryInfo{}

	if !file.Exists(categoryListFile) {
		return map[string]codegen.CategoryInfo{}
	}

	buf := file.ReadFullFile(categoryListFile)

	if err := json.Unmarshal(buf, &categoryList); err != nil {
		logger.Error("failed to unmarshal category list", zap.Error(err), zap.String("categoryListFile", categoryListFile))
		return map[string]codegen.CategoryInfo{}
	}

	categoryList = lo.Filter(categoryList, func(category codegen.CategoryInfo, i int) bool {
		return category.Name != nil && *category.Name != ""
	})

	categoryList = lo.Map(categoryList, func(category codegen.CategoryInfo, i int) codegen.CategoryInfo {
		if category.Font == nil || *category.Font == "" {
			category.Font = lo.ToPtr(common.DefaultCategoryFont)
		}

		if category.Description == nil {
			category.Description = lo.ToPtr("")
		}

		return category
	})

	return lo.SliceToMap(categoryList, func(category codegen.CategoryInfo) (string, codegen.CategoryInfo) {
		return *category.Name, category
	})
}

func LoadRecommend(storeRoot string) []string {
	recommendListFile := filepath.Join(storeRoot, common.RecommendListFileName)

	// unmarsal recommend list
	recommendList := []interface{}{}

	if !file.Exists(recommendListFile) {
		logger.Info("recommend list file not found", zap.String("recommendListFile", recommendListFile))
		return []string{}
	}

	buf := file.ReadFullFile(recommendListFile)
	if err := json.Unmarshal(buf, &recommendList); err != nil {
		logger.Error("failed to unmarshal recommend list", zap.Error(err), zap.String("recommendListFile", recommendListFile))
		return []string{}
	}

	result := lo.Map(recommendList, func(item interface{}, i int) string {
		recommendItem, ok := item.(map[string]interface{})
		if !ok {
			return ""
		}

		storeAppID, ok := recommendItem["appid"]
		if !ok {
			return ""
		}

		appID, _ := storeAppID.(string) // the list is a store's: anything but text is no app
		return appID
	})

	return result
}

func BuildCatalog(storeRoot string) (map[string]*ComposeApp, error) {
	catalog := map[string]*ComposeApp{}

	// walk through each folder under storeRoot/Apps and build the catalog
	if err := filepath.WalkDir(filepath.Join(storeRoot, common.AppsDirectoryName), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			return nil
		}

		composeFile := filepath.Join(path, common.ComposeYAMLFileName)
		if !file.Exists(composeFile) {
			// retry with ".yaml" extension
			composeFile = strings.TrimSuffix(composeFile, ".yml") + ".yaml"
			if !file.Exists(composeFile) {
				return nil
			}
		}

		composeYAML := file.ReadFullFile(composeFile)
		if len(composeYAML) == 0 {
			return nil
		}

		composeApp, err := NewComposeAppFromYAML(composeYAML, true, false)
		if err != nil {
			logger.Info("failed to parse compose app - contact the contributor of this app to fix it", zap.Error(err), zap.String("composeFile", composeFile))
			return fs.SkipDir // skip invalid compose app
		}

		catalog[composeApp.Name] = composeApp

		return nil
	}); err != nil {
		return nil, err
	}

	return catalog, nil
}

func StoreRoot(workdir string) (string, error) {
	storeRoot := ""

	// locate the path that contains the Apps directory
	if err := filepath.WalkDir(workdir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() && d.Name() == common.AppsDirectoryName {
			storeRoot = filepath.Dir(path)
			return filepath.SkipDir
		}

		return nil
	}); err != nil {
		return "", err
	}

	if storeRoot != "" {
		return storeRoot, nil
	}

	return "", ErrNotAppStore
}

func (s *appStore) catalogTooSmall(n int) error {
	min := s.minAppCount
	if min == 0 && s.isReCasaOSCatalogue() {
		min = minCatalogueApps
	}
	if min > 0 && n < min {
		return fmt.Errorf("%w: %d apps, floor %d", ErrCatalogTooSmall, n, min)
	}
	if s.lastAppCount > 0 && float64(n) < float64(s.lastAppCount)*minCatalogueRatio {
		return fmt.Errorf("%w: %d apps, last successful count %d", ErrCatalogTooSmall, n, s.lastAppCount)
	}
	return nil
}

func (s *appStore) isReCasaOSCatalogue() bool {
	for _, u := range []string{s.url, s.source} {
		switch u {
		case recasaosCatalogue, recasaosCatalogueGitHub, recasaosCatalogueGitHubRaw:
			return true
		}
	}
	return false
}

func sha256File(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type storeIndexFileBody struct {
	SHA256 string `json:"sha256"`
}

func verifyZipSHA256(zipURL, sum, expected string, hasIndex bool) error {
	if hasIndex {
		if !strings.EqualFold(expected, sum) {
			return fmt.Errorf("%w: index %s zip %s", ErrStoreIndexHashMismatch, expected, sum)
		}
		return nil
	}
	if sidecar, ok := fetchSidecarSHA256(zipURL); ok && !strings.EqualFold(sidecar, sum) {
		return fmt.Errorf("%w: sidecar %s zip %s", ErrStoreIndexHashMismatch, sidecar, sum)
	}
	return nil
}

func fetchSidecarSHA256(zipURL string) (string, bool) {
	res, err := http.Get(zipURL + ".sha256") //nolint:gosec // catalogue URL already picked from the configured store
	if err != nil {
		return "", false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", false
	}
	buf, err := io.ReadAll(res.Body)
	if err != nil {
		return "", false
	}
	fields := strings.Fields(string(buf))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", false
	}
	return fields[0], true
}

func fetchIndexSHA256(zipURL string) (string, bool) {
	indexURL := siblingIndexURL(zipURL)
	if indexURL == "" {
		return "", false
	}

	res, err := http.Get(indexURL) //nolint:gosec // catalogue URL already picked from the configured store
	if err != nil {
		return "", false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", false
	}

	buf, err := io.ReadAll(res.Body)
	if err != nil {
		return "", false
	}

	var index storeIndexFileBody
	if err := json.Unmarshal(buf, &index); err != nil || index.SHA256 == "" {
		return "", false
	}
	return index.SHA256, true
}

func siblingIndexURL(zipURL string) string {
	switch zipURL {
	case recasaosCatalogue:
		return recasaosCatalogueIndex
	case recasaosCatalogueGitHub:
		return recasaosCatalogueGitHubIndex
	}

	u, err := url.Parse(zipURL)
	if err != nil {
		return ""
	}
	dir := path.Dir(u.Path)
	if dir == "." || dir == "/" {
		return ""
	}
	u.Path = path.Join(dir, storeIndexFile)
	return u.String()
}
