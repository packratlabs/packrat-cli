package query

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	// ReleasesAPI is GitHub's API for the CLI's repository. It answers which release is the newest.
	ReleasesAPI = "https://api.github.com/repos/packratlabs/packrat-cli"
	// ReleasesDownload is where each release's archives and checksums.txt are.
	ReleasesDownload = "https://github.com/packratlabs/packrat-cli/releases/download"
	maxArchive       = 64 << 20
)

// ErrChecksum is a download that does not match the release's checksums.txt.
var ErrChecksum = errors.New("the download doesn't match the release's checksums.txt, so it wasn't installed")

// ErrNoBinary is an archive with no packrat in it.
var ErrNoBinary = errors.New("the release archive has no packrat binary")

// Releases reads the CLI's releases on GitHub. It never sends a key.
type Releases struct {
	API      string
	Download string
	HTTP     *http.Client
}

// Latest is the newest release's version, without the leading v.
func (r Releases) Latest(ctx context.Context) (string, error) {
	body, err := r.fetch(ctx, strings.TrimRight(r.API, "/")+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return "", err
	}
	var release struct {
		Tag string `json:"tag_name"`
	}
	if err := decode(r.API, body, &release); err != nil {
		return "", err
	}
	if release.Tag == "" {
		return "", fmt.Errorf("reading the answer of %s: %w", r.API, errNoTag)
	}
	return strings.TrimPrefix(release.Tag, "v"), nil
}

var errNoTag = errors.New("the newest release has no tag")

// Binary downloads the release's archive for goos and goarch, checks it against checksums.txt, and
// answers the packrat binary inside it.
func (r Releases) Binary(ctx context.Context, version, goos, goarch string) ([]byte, error) {
	format := "tar.gz"
	if goos == "windows" {
		format = "zip"
	}
	name := fmt.Sprintf("packrat_%s_%s.%s", goos, goarch, format)
	base := strings.TrimRight(r.Download, "/") + "/v" + version + "/"
	archive, err := r.fetch(ctx, base+name, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	sums, err := r.fetch(ctx, base+"checksums.txt", "text/plain")
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(archive)
	if want := checksum(sums, name); want == "" || want != hex.EncodeToString(got[:]) {
		return nil, fmt.Errorf("%s: %w", name, ErrChecksum)
	}
	if format == "zip" {
		return fromZip(archive)
	}
	return fromTarGz(archive)
}

func (r Releases) fetch(ctx context.Context, address, accept string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building the request to %s: %w", address, err)
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", UserAgent)
	response, err := r.HTTP.Do(request) // #nosec G704 -- GitHub's addresses for this repository, or a test's
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", address, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxArchive))
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", address, err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %w", address, &statusError{code: response.StatusCode})
	}
	return body, nil
}

type statusError struct{ code int }

func (e *statusError) Error() string { return fmt.Sprintf("GitHub answered %d", e.code) }

// checksum is the sum that checksums.txt gives name, or "" when it gives none.
func checksum(sums []byte, name string) string {
	for _, line := range strings.Split(string(sums), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == name {
			return fields[0]
		}
	}
	return ""
}

func fromTarGz(archive []byte) ([]byte, error) {
	unzipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("opening the release archive: %w", err)
	}
	files := tar.NewReader(unzipped)
	for {
		header, err := files.Next()
		if errors.Is(err, io.EOF) {
			return nil, ErrNoBinary
		}
		if err != nil {
			return nil, fmt.Errorf("reading the release archive: %w", err)
		}
		if header.Name == "packrat" {
			binary, err := io.ReadAll(io.LimitReader(files, maxArchive))
			if err != nil {
				return nil, fmt.Errorf("reading packrat from the release archive: %w", err)
			}
			return binary, nil
		}
	}
}

func fromZip(archive []byte) ([]byte, error) {
	files, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("opening the release archive: %w", err)
	}
	for _, file := range files.File {
		if file.Name != "packrat.exe" {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("reading packrat from the release archive: %w", err)
		}
		defer func() { _ = opened.Close() }()
		binary, err := io.ReadAll(io.LimitReader(opened, maxArchive))
		if err != nil {
			return nil, fmt.Errorf("reading packrat from the release archive: %w", err)
		}
		return binary, nil
	}
	return nil, ErrNoBinary
}
