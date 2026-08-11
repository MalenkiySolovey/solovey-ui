package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	MaxRedirects      = 3
	MaxResponseHeader = 64 << 10
	FetchTimeout      = 15 * time.Second
	ArtifactTimeout   = 5 * time.Minute
	MaxReadIdle       = 30 * time.Second
)

type Source struct {
	ID                 string
	Origin             string
	ManifestPath       string
	ExpectedProvenance string
	RedirectOrigins    []string
}

type Fetcher struct {
	Client   *http.Client
	Now      func() time.Time
	ReadIdle time.Duration
}

func (source Source) Validate() (*url.URL, error) {
	if !identifier.MatchString(source.ID) || !identifier.MatchString(source.ExpectedProvenance) ||
		!strings.HasPrefix(source.ManifestPath, "/") ||
		strings.Contains(source.ManifestPath, "..") || strings.ContainsAny(source.ManifestPath, "?#\\") {
		return nil, errors.New("release source identity is invalid")
	}
	origin, err := url.Parse(source.Origin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" {
		return nil, errors.New("release source origin is invalid")
	}
	if len(source.RedirectOrigins) > MaxRedirects {
		return nil, errors.New("release redirect origin inventory is too large")
	}
	seen := map[string]struct{}{originKey(origin): {}}
	for _, raw := range source.RedirectOrigins {
		candidate, parseErr := url.Parse(raw)
		if parseErr != nil || candidate.Scheme != "https" || candidate.Host == "" || candidate.User != nil || candidate.Path != "" ||
			candidate.RawQuery != "" || candidate.Fragment != "" {
			return nil, errors.New("release redirect origin is invalid")
		}
		key := originKey(candidate)
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.New("release redirect origin is duplicated")
		}
		seen[key] = struct{}{}
	}
	return origin, nil
}

func (f Fetcher) Fetch(ctx context.Context, source Source) ([]byte, error) {
	origin, err := source.Validate()
	if err != nil {
		return nil, err
	}
	client := f.Client
	if client == nil {
		client = &http.Client{}
	}
	copyClient := *client
	allowed := source.allowedOrigins(origin)
	previousCheck := copyClient.CheckRedirect
	copyClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > MaxRedirects || request.URL.User != nil || !originIsAllowed(request.URL, allowed) {
			return errors.New("release source redirect left its pinned origin")
		}
		if previousCheck != nil {
			return previousCheck(request, via)
		}
		return nil
	}
	fetchCtx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	requestURL := *origin
	requestURL.Path = path.Clean(source.ManifestPath)
	request, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.solovey.release+json")
	request.Header.Set("User-Agent", "solovey-ui-release-client/1")
	response, err := copyClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL == nil || !originIsAllowed(response.Request.URL, allowed) {
		return nil, errors.New("release response origin is invalid")
	}
	if headerBytes(response.Header) > MaxResponseHeader {
		return nil, errors.New("release response headers exceed limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("release source returned status %d", response.StatusCode)
	}
	if response.ContentLength > MaxManifestBytes {
		return nil, errors.New("release response exceeds limit")
	}
	limited := io.LimitReader(&idleBoundReader{body: response.Body, timeout: f.readIdle()}, MaxManifestBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > MaxManifestBytes {
		return nil, errors.New("release response size is invalid")
	}
	return data, nil
}

func (f Fetcher) FetchArtifact(ctx context.Context, source Source, artifact Artifact, destination io.Writer) (int64, error) {
	origin, err := source.Validate()
	if err != nil {
		return 0, err
	}
	if destination == nil || !artifactName.MatchString(artifact.Name) || artifact.Size <= 0 || artifact.Size > MaxArtifactBytes ||
		!digest.MatchString(artifact.SHA256) || artifact.Provenance != source.ExpectedProvenance {
		return 0, errors.New("release artifact request is invalid")
	}
	client := f.Client
	if client == nil {
		client = &http.Client{}
	}
	copyClient := *client
	allowed := source.allowedOrigins(origin)
	previousCheck := copyClient.CheckRedirect
	copyClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > MaxRedirects || request.URL.User != nil || !originIsAllowed(request.URL, allowed) {
			return errors.New("release artifact redirect left its pinned origin")
		}
		if previousCheck != nil {
			return previousCheck(request, via)
		}
		return nil
	}
	fetchCtx, cancel := context.WithTimeout(ctx, ArtifactTimeout)
	defer cancel()
	requestURL := *origin
	requestURL.Path = path.Join(path.Dir(source.ManifestPath), artifact.Name)
	request, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", artifact.MediaType)
	request.Header.Set("User-Agent", "solovey-ui-release-client/1")
	response, err := copyClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL == nil || !originIsAllowed(response.Request.URL, allowed) ||
		headerBytes(response.Header) > MaxResponseHeader || response.StatusCode < 200 || response.StatusCode >= 300 ||
		(response.ContentLength >= 0 && response.ContentLength != artifact.Size) {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return 0, errors.New("release artifact response is invalid")
	}
	hash := sha256.New()
	boundedBody := &idleBoundReader{body: response.Body, timeout: f.readIdle()}
	written, err := io.Copy(io.MultiWriter(destination, hash), io.LimitReader(boundedBody, artifact.Size+1))
	if err != nil {
		return written, err
	}
	if written != artifact.Size || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
		return written, errors.New("release artifact size or digest mismatch")
	}
	return written, nil
}

func (source Source) allowedOrigins(origin *url.URL) map[string]struct{} {
	allowed := map[string]struct{}{originKey(origin): {}}
	for _, raw := range source.RedirectOrigins {
		candidate, _ := url.Parse(raw)
		allowed[originKey(candidate)] = struct{}{}
	}
	return allowed
}

func originKey(value *url.URL) string {
	if value == nil {
		return ""
	}
	return strings.ToLower(value.Scheme) + "://" + strings.ToLower(value.Host)
}

func originIsAllowed(value *url.URL, allowed map[string]struct{}) bool {
	if value == nil || value.User != nil {
		return false
	}
	_, ok := allowed[originKey(value)]
	return ok
}

func headerBytes(headers http.Header) int {
	total := 0
	for name, values := range headers {
		total += len(name)
		for _, value := range values {
			total += len(value)
		}
	}
	return total
}

func (f Fetcher) readIdle() time.Duration {
	if f.ReadIdle <= 0 || f.ReadIdle > MaxReadIdle {
		return MaxReadIdle
	}
	return f.ReadIdle
}

type idleBoundReader struct {
	body    io.ReadCloser
	timeout time.Duration
}

func (reader *idleBoundReader) Read(buffer []byte) (int, error) {
	expired := make(chan struct{})
	timer := time.AfterFunc(reader.timeout, func() {
		close(expired)
		_ = reader.body.Close()
	})
	count, err := reader.body.Read(buffer)
	if !timer.Stop() {
		<-expired
		return count, errors.New("release response exceeded the read idle limit")
	}
	return count, err
}
