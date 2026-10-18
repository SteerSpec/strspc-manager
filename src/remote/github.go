// Package remote fetches rule directories from remote sources and caches
// them on the local filesystem. The only supported scheme today is
// github://, backed by the GitHub REST API tarball endpoint.
//
// URI grammar:
//
//	github://<owner>/<repo>@<ref>[/<path>]
//
// <ref> is an exact tag or commit SHA (treated as immutable), or the
// literal "latest", which is resolved through the releases API and
// re-resolved once the cache TTL has elapsed.
package remote

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Scheme is the URI scheme handled by this package.
	Scheme = "github://"

	// LatestRef is the mutable ref resolved through the releases API.
	LatestRef = "latest"

	// DefaultBaseURL is the GitHub REST API root.
	DefaultBaseURL = "https://api.github.com"

	// DefaultTTL bounds how long a resolved "latest" tag is trusted.
	DefaultTTL = 24 * time.Hour

	// MaxExtractBytes caps the total uncompressed size extracted from one
	// tarball (50 MB).
	MaxExtractBytes int64 = 50 << 20

	defaultTimeout = 60 * time.Second
	latestPointer  = "latest.json"
)

// ErrNotFound is wrapped into errors returned when GitHub answers 404.
var ErrNotFound = errors.New("not found")

// Ref is a parsed github:// reference.
type Ref struct {
	Owner string
	Repo  string
	Ref   string // tag, commit SHA, or "latest"
	Path  string // slash-separated path inside the repository, "" for root
}

// String renders the reference in normalised github:// form.
func (r Ref) String() string {
	s := Scheme + r.Owner + "/" + r.Repo + "@" + r.Ref
	if r.Path != "" {
		s += "/" + r.Path
	}
	return s
}

// IsImmutable reports whether the ref can never move (anything but "latest").
func (r Ref) IsImmutable() bool { return r.Ref != LatestRef }

// ParseGitHub parses a github://owner/repo@ref/path URI. The @ref part is
// mandatory; the path is optional and normalised (no leading/trailing
// slashes, no "." or ".." segments).
func ParseGitHub(uri string) (Ref, error) {
	if !strings.HasPrefix(uri, Scheme) {
		return Ref{}, fmt.Errorf("invalid github source %q: must start with %s", uri, Scheme)
	}
	rest := strings.TrimPrefix(uri, Scheme)
	if strings.ContainsAny(rest, "\\ \t\n") {
		return Ref{}, fmt.Errorf("invalid github source %q: illegal character", uri)
	}

	owner, rest, ok := strings.Cut(rest, "/")
	if !ok || owner == "" {
		return Ref{}, fmt.Errorf("invalid github source %q: expected github://owner/repo@ref[/path]", uri)
	}

	repoRef, p, _ := strings.Cut(rest, "/")
	repo, ref, ok := strings.Cut(repoRef, "@")
	if !ok {
		return Ref{}, fmt.Errorf("invalid github source %q: missing @ref (use an exact tag, a commit SHA, or @latest)", uri)
	}
	if repo == "" || ref == "" {
		return Ref{}, fmt.Errorf("invalid github source %q: owner, repo and ref must be non-empty", uri)
	}
	for _, s := range []string{owner, repo, ref} {
		if strings.ContainsAny(s, "/:@") || s == "." || s == ".." {
			return Ref{}, fmt.Errorf("invalid github source %q: illegal component %q", uri, s)
		}
	}

	clean := ""
	if p != "" {
		if hasDotDot(p) {
			return Ref{}, fmt.Errorf("invalid github source %q: path must not contain \"..\"", uri)
		}
		clean = strings.TrimPrefix(path.Clean("/"+p), "/")
	}

	return Ref{Owner: owner, Repo: repo, Ref: ref, Path: clean}, nil
}

// TokenFromEnv returns the GitHub token from GITHUB_TOKEN, then GH_TOKEN.
// Callers decide whether to use it; the Fetcher never reads the environment.
func TokenFromEnv() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GH_TOKEN")
}

// Fetcher downloads github:// references into a local cache directory.
// The zero value is usable once CacheDir is set.
type Fetcher struct {
	Client   *http.Client  // nil → default client with a 60s timeout
	Token    string        // bearer token sent as Authorization when non-empty
	BaseURL  string        // API root, "" → DefaultBaseURL
	CacheDir string        // required; content lands under <CacheDir>/github/
	TTL      time.Duration // how long a resolved "latest" tag is trusted, 0 → DefaultTTL
	Force    bool          // ignore cached content and re-download

	now func() time.Time // test hook
}

// EnsureDir makes sure the directory for ref exists in the cache and
// returns its absolute path. Immutable refs are downloaded at most once;
// "latest" is re-resolved through the releases API after TTL.
//
// Layout: <CacheDir>/github/<owner>/<repo>/<ref>/<sha256(path)[:12]>/
func (f *Fetcher) EnsureDir(ctx context.Context, ref Ref) (string, error) {
	if f.CacheDir == "" {
		return "", errors.New("remote: cache directory not configured")
	}
	if ref.Owner == "" || ref.Repo == "" || ref.Ref == "" {
		return "", fmt.Errorf("remote: incomplete reference %q", ref.String())
	}
	for _, c := range []string{ref.Owner, ref.Repo, ref.Ref} {
		if c == "." || c == ".." || strings.ContainsAny(c, `/\`) {
			return "", fmt.Errorf("remote: illegal component %q in reference %q", c, ref.String())
		}
	}
	if hasDotDot(ref.Path) || strings.Contains(ref.Path, `\`) {
		return "", fmt.Errorf("remote: illegal path in reference %q", ref.String())
	}

	repoDir, err := filepath.Abs(filepath.Join(f.CacheDir, "github", ref.Owner, ref.Repo))
	if err != nil {
		return "", fmt.Errorf("remote: resolving cache dir: %w", err)
	}

	concrete := ref
	if !ref.IsImmutable() {
		tag, err := f.resolveLatest(ctx, ref, repoDir)
		if err != nil {
			return "", err
		}
		concrete.Ref = tag
	}

	dir := filepath.Join(repoDir, concrete.Ref, pathKey(concrete.Path))
	if !f.Force {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
	}

	if err := f.download(ctx, concrete, dir); err != nil {
		return "", err
	}
	return dir, nil
}

// pathKey derives the per-path cache segment.
func pathKey(p string) string {
	sum := sha256.Sum256([]byte(p))
	return hex.EncodeToString(sum[:])[:12]
}

type latestRecord struct {
	Tag        string    `json:"tag"`
	ResolvedAt time.Time `json:"resolved_at"`
}

// resolveLatest returns the tag_name of the latest release, using the
// cached pointer while it is younger than TTL. When the API is unreachable
// and a stale pointer exists, the stale tag is returned so offline runs
// keep working.
func (f *Fetcher) resolveLatest(ctx context.Context, ref Ref, repoDir string) (string, error) {
	ttl := f.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	pointer := filepath.Join(repoDir, latestPointer)

	var cached *latestRecord
	if data, err := os.ReadFile(pointer); err == nil {
		var rec latestRecord
		if json.Unmarshal(data, &rec) == nil && rec.Tag != "" {
			cached = &rec
		}
	}
	if cached != nil && !f.Force && f.clock().Sub(cached.ResolvedAt) < ttl {
		return cached.Tag, nil
	}

	tag, err := f.fetchLatestTag(ctx, ref)
	if err != nil {
		if cached != nil && !errors.Is(err, ErrNotFound) {
			return cached.Tag, nil
		}
		return "", err
	}

	rec, _ := json.Marshal(latestRecord{Tag: tag, ResolvedAt: f.clock()})
	_ = writeFileAtomic(pointer, rec) // best effort
	return tag, nil
}

func (f *Fetcher) fetchLatestTag(ctx context.Context, ref Ref) (string, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/latest", f.baseURL(),
		url.PathEscape(ref.Owner), url.PathEscape(ref.Repo))
	resp, err := f.get(ctx, endpoint, "application/vnd.github+json")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", f.statusError(resp, ref, "resolving @latest")
	}

	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("remote: decoding latest release for %s/%s: %w", ref.Owner, ref.Repo, err)
	}
	if body.TagName == "" {
		return "", fmt.Errorf("remote: latest release of %s/%s has no tag_name", ref.Owner, ref.Repo)
	}
	return body.TagName, nil
}

// download fetches the tarball for a concrete ref and extracts ref.Path
// into dir via a temporary sibling directory and an atomic rename. Under
// Force an existing dir is swapped out by two renames; it is never removed
// before its replacement is ready.
func (f *Fetcher) download(ctx context.Context, ref Ref, dir string) error {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/tarball/%s", f.baseURL(),
		url.PathEscape(ref.Owner), url.PathEscape(ref.Repo), url.PathEscape(ref.Ref))
	resp, err := f.get(ctx, endpoint, "application/vnd.github+json")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return f.statusError(resp, ref, "downloading tarball")
	}

	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("remote: creating cache dir: %w", err)
	}
	tmp, err := os.MkdirTemp(parent, ".tmp-*")
	if err != nil {
		return fmt.Errorf("remote: creating temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	n, err := extractTarball(resp.Body, ref.Path, tmp)
	if err != nil {
		cleanup()
		return fmt.Errorf("remote: extracting %s: %w", ref.String(), err)
	}
	if n == 0 {
		cleanup()
		return fmt.Errorf("remote: path %q not found in %s/%s@%s", ref.Path, ref.Owner, ref.Repo, ref.Ref)
	}

	// With Force, move the published directory aside first so the path is
	// never absent: readers either see the old generation or the new one,
	// and a crash between the two renames leaves the old generation on
	// disk under .old-* instead of losing it.
	var old string
	if f.Force {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			old, err = os.MkdirTemp(parent, ".old-*")
			if err != nil {
				cleanup()
				return fmt.Errorf("remote: staging replaced cache dir: %w", err)
			}
			_ = os.Remove(old)
			if err := os.Rename(dir, old); err != nil {
				cleanup()
				return fmt.Errorf("remote: moving replaced cache dir aside: %w", err)
			}
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		cleanup()
		if old != "" {
			// Restore the previous generation rather than leave a hole.
			_ = os.Rename(old, dir)
		}
		// A concurrent fetch may have won the race; accept its result.
		if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
			return nil
		}
		return fmt.Errorf("remote: installing cached dir: %w", err)
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}
	return nil
}

// extractTarball streams a gzip-compressed tarball, keeping only regular
// files under <top-level-prefix>/<sub>/ and writing them below dest.
// It returns the number of files written.
func extractTarball(r io.Reader, sub, dest string) (int, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return 0, err
	}

	tr := tar.NewReader(gz)
	var total int64
	files := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return files, fmt.Errorf("tar: %w", err)
		}

		if hasDotDot(hdr.Name) {
			return files, fmt.Errorf("tar: entry %q escapes extraction root", hdr.Name)
		}
		rel, ok := stripPrefix(hdr.Name, sub)
		if !ok {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if rel == "" {
				continue
			}
			target, err := safeJoin(destAbs, rel)
			if err != nil {
				return files, err
			}
			if err := os.MkdirAll(target, 0o755); err != nil {
				return files, err
			}
		case tar.TypeReg:
			if rel == "" {
				continue
			}
			if hdr.Size < 0 {
				return files, fmt.Errorf("tar: negative size for %q", hdr.Name)
			}
			total += hdr.Size
			if total > MaxExtractBytes {
				return files, fmt.Errorf("archive exceeds %d bytes extraction cap", MaxExtractBytes)
			}
			target, err := safeJoin(destAbs, rel)
			if err != nil {
				return files, err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return files, err
			}
			if err := writeEntry(target, tr, hdr.Size); err != nil {
				return files, err
			}
			files++
		default:
			// Symlinks, hard links, devices and friends are never materialised.
			continue
		}
	}
	return files, nil
}

// stripPrefix removes the tarball's top-level directory and the requested
// sub-path from name, returning the remaining relative path (slash
// separated). ok is false when the entry is outside sub.
func stripPrefix(name, sub string) (string, bool) {
	name = strings.TrimPrefix(name, "./")
	_, rest, found := strings.Cut(name, "/")
	if !found {
		// The top-level directory entry itself.
		return "", sub == ""
	}
	rest = strings.TrimSuffix(rest, "/")
	if sub == "" {
		return rest, true
	}
	if rest == sub {
		return "", true
	}
	if strings.HasPrefix(rest, sub+"/") {
		return strings.TrimPrefix(rest, sub+"/"), true
	}
	return "", false
}

// safeJoin joins rel (slash separated) under base and rejects any result
// that escapes base (zip-slip guard).
func safeJoin(base, rel string) (string, error) {
	if strings.Contains(rel, "\\") {
		return "", fmt.Errorf("tar: illegal entry name %q", rel)
	}
	if hasDotDot(rel) {
		return "", fmt.Errorf("tar: entry %q escapes extraction root", rel)
	}
	clean := path.Clean("/" + rel)
	if clean == "/" {
		return "", fmt.Errorf("tar: illegal entry name %q", rel)
	}
	target := filepath.Join(base, filepath.FromSlash(clean))
	if target != base && !strings.HasPrefix(target, base+string(filepath.Separator)) {
		return "", fmt.Errorf("tar: entry %q escapes extraction root", rel)
	}
	return target, nil
}

// hasDotDot reports whether any slash-separated segment of p is "..".
func hasDotDot(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func writeEntry(target string, r io.Reader, size int64) error {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(r, size)); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// writeFileAtomic mirrors schema.writeCache: temp file, fsync, rename.
func writeFileAtomic(p string, data []byte) error {
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".pointer-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		if removeErr := os.Remove(p); removeErr != nil && !os.IsNotExist(removeErr) {
			_ = os.Remove(tmpName)
			return err
		}
		if err2 := os.Rename(tmpName, p); err2 != nil {
			_ = os.Remove(tmpName)
			return err2
		}
	}
	return nil
}

func (f *Fetcher) get(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("remote: building request: %w", err)
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "strspc-manager")
	if f.Token != "" {
		req.Header.Set("Authorization", "Bearer "+f.Token)
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote: %w", err)
	}
	return resp, nil
}

// statusError turns a non-200 response into a descriptive error. The body
// is drained so the connection can be reused.
func (f *Fetcher) statusError(resp *http.Response, ref Ref, what string) error {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	target := fmt.Sprintf("%s/%s@%s", ref.Owner, ref.Repo, ref.Ref)
	switch resp.StatusCode {
	case http.StatusNotFound:
		if f.Token == "" {
			return fmt.Errorf("remote: %s for %s: %w (private repo or missing token; set GITHUB_TOKEN)", what, target, ErrNotFound)
		}
		return fmt.Errorf("remote: %s for %s: %w (check owner, repo and ref)", what, target, ErrNotFound)
	case http.StatusUnauthorized:
		return fmt.Errorf("remote: %s for %s: HTTP 401 unauthorized (invalid token)", what, target)
	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return fmt.Errorf("remote: %s for %s: HTTP 403 rate limited (set GITHUB_TOKEN to raise the limit)", what, target)
		}
		return fmt.Errorf("remote: %s for %s: HTTP 403 forbidden", what, target)
	default:
		return fmt.Errorf("remote: %s for %s: HTTP %d", what, target, resp.StatusCode)
	}
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: defaultTimeout}
}

func (f *Fetcher) baseURL() string {
	if f.BaseURL != "" {
		return strings.TrimRight(f.BaseURL, "/")
	}
	return DefaultBaseURL
}

func (f *Fetcher) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}
