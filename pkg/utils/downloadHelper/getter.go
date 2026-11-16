package downloadHelper

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-getter"
)

// Download fetches src into dst and unpacks it when it is an archive.
//
// An app store URL is typed by the user and the server behind it is not
// trusted, so only http and https are fetched: no file, git, hg, s3 or gcs
// getter, no X-Terraform-Get or <meta name="terraform-get"> redirect to one,
// no .netrc credentials sent to whatever host the URL names, and symlinks
// are never copied.
func Download(src string, dst string) error {
	if err := newClient(src, dst, getter.ClientModeAny).Get(); err != nil {
		return err
	}

	// A URL whose path ends in "/" is a directory to go-getter, and with
	// X-Terraform-Get off its directory download fetches nothing and returns
	// nil. The caller would take an empty dst for the new catalogue.
	entries, err := os.ReadDir(dst)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(entries) == 0 {
		return errors.New("nothing was downloaded: the URL must name an archive such as a .zip, not a directory ending in '/'")
	}

	return nil
}

// DownloadToFile fetches src as a single file at dst without unpacking.
// A catalogue zip can then be hashed before UnpackZip extracts it.
// go-getter's ClientModeFile still decompresses a .zip URL, so this talks
// to HttpGetter.GetFile and keeps the same http(s)-only, no-netrc rules.
func DownloadToFile(src, dst string) error {
	u, err := url.Parse(src)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %s not supported", u.Scheme)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return (&getter.HttpGetter{XTerraformGetDisabled: true}).GetFile(dst, u)
}

// UnpackZip extracts zipPath into dest. Dest is a directory.
func UnpackZip(zipPath, dest string) error {
	return new(getter.ZipDecompressor).Decompress(dest, zipPath, true, 0)
}

func newClient(src string, dst string, mode getter.ClientMode) *getter.Client {
	httpGetter := &getter.HttpGetter{XTerraformGetDisabled: true}
	return &getter.Client{
		Ctx:             context.Background(),
		Src:             src,
		Dst:             dst,
		Mode:            mode,
		DisableSymlinks: true,
		// an option rather than the Getters field, so a nested Get inherits it
		Options: []getter.ClientOption{getter.WithGetters(map[string]getter.Getter{
			"http":  httpGetter,
			"https": httpGetter,
		})},
	}
}
