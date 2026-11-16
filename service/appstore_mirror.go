package service

import (
	"fmt"
	"net/http"

	"github.com/ReCasaOS/CasaOS-Common/utils/logger"
	"go.uber.org/zap"
)

// The catalogue's mirror.
//
// The App Store is IceWhale's catalogue, fetched as a zip: since February 2025
// the artifact IceWhaleTech/CasaOS-AppStore builds on its gh-pages branch,
// served through jsdelivr, and before that a zip of the IceWhaleTech/_appstore
// repository, which a box installed then and upgraded since may still name in
// its configuration. The day the source goes away, every box keeps the copy it
// already has and never sees an update again, and a fresh install gets the seed
// the installer ships and nothing after. This distribution keeps a copy of the
// artifact, refreshed every night (github.com/ReCasaOS/_appstore); when the
// original stops answering, the copy is fetched instead. The configured URL is
// not changed, on disk or in the dashboard: the original is still the source,
// and it is tried first on every update.

// mirrorZip is this distribution's copy of the catalogue: the same bytes as
// IceWhale's store/main.zip, taken every night.
const mirrorZip = "https://raw.githubusercontent.com/ReCasaOS/_appstore/main/store/main.zip"

// recasaosCatalogue is the store a ReCasaOS box names by default: IceWhale's
// apps, RoqueOS's and our Big Bear fork's merged every night, served from
// recasaos.org. GitHub is the copy, not the source.
const recasaosCatalogue = "https://recasaos.org/store/recasaos.zip"

// recasaosCatalogueGitHub is the same bytes as a GitHub Release asset.
const recasaosCatalogueGitHub = "https://github.com/ReCasaOS/_appstore/releases/latest/download/recasaos.zip"

// recasaosCatalogueIndex is the publication record next to the org zip.
const recasaosCatalogueIndex = "https://recasaos.org/store/store-index.json"

// recasaosCatalogueGitHubIndex is that same record on the GitHub Release.
const recasaosCatalogueGitHubIndex = "https://github.com/ReCasaOS/_appstore/releases/latest/download/store-index.json"

// recasaosCatalogueGitHubRaw is the URL a box still names if it was installed
// before the org became the default.
const recasaosCatalogueGitHubRaw = "https://raw.githubusercontent.com/ReCasaOS/_appstore/catalogue/store/recasaos.zip"

// catalogueMirrors maps a catalogue's URL to this distribution's copy of it.
var catalogueMirrors = map[string]string{
	// the URL every configuration written since February 2025 carries
	"https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@gh-pages/store/main.zip": mirrorZip,
	// the URL a box installed before that, and upgraded, still carries
	"https://github.com/IceWhaleTech/_appstore/archive/refs/heads/main.zip": mirrorZip,
	// the ReCasaOS catalogue: org first, then the GitHub Release zip
	recasaosCatalogue: recasaosCatalogueGitHub,
	// a box that still names the old raw GitHub zip tries the org next,
	// then the GitHub Release (the org's own mirror)
	recasaosCatalogueGitHubRaw: recasaosCatalogue,
}

// deadStores maps a store URL that will never answer again to the catalogue
// that replaces it. The dashboard offers the switch; this table does not
// rewrite the box's configuration.
var deadStores = map[string]string{
	"https://github.com/bigbeartechworld/big-bear-casaos/archive/refs/heads/master.zip": recasaosCatalogue,
}

// SuccessorOf reports whether url is a known-dead store and, if so, the
// catalogue that replaces it.
func SuccessorOf(url string) (successor string, dead bool) {
	successor, dead = deadStores[url]
	return
}

// pickCatalogueSource asks the catalogue's URL whether it answers, then each
// mirror in turn. IceWhale URLs have one hop; the legacy ReCasaOS raw zip
// follows org, then the GitHub Release. It returns the URL to fetch and the
// answer, whose ContentLength says whether anything changed.
func pickCatalogueSource(url string, head func(string) (*http.Response, error)) (string, *http.Response, error) {
	seen := map[string]struct{}{}
	current := url
	var lastErr error

	for {
		if _, already := seen[current]; already {
			return "", nil, lastErr
		}
		seen[current] = struct{}{}

		res, headErr := head(current)
		if headErr == nil && res.StatusCode == http.StatusOK {
			return current, res, nil
		}
		if res != nil && res.Body != nil {
			res.Body.Close()
		}

		hopErr := headErr
		if hopErr == nil {
			hopErr = fmt.Errorf("failed to get appstore size, status code: %d", res.StatusCode)
		}

		if lastErr == nil {
			lastErr = hopErr
		} else if headErr != nil {
			lastErr = fmt.Errorf("%w (and the mirror: %v)", lastErr, headErr)
		} else {
			lastErr = fmt.Errorf("%w (and the mirror answered %d)", lastErr, res.StatusCode)
		}

		mirror, ok := catalogueMirrors[current]
		if !ok {
			return "", nil, lastErr
		}
		if _, hop := seen[mirror]; hop {
			return "", nil, lastErr
		}

		logger.Warn("the catalogue does not answer; trying this distribution's copy of it",
			zap.String("url", current), zap.String("mirror", mirror), zap.Error(hopErr))

		current = mirror
	}
}
