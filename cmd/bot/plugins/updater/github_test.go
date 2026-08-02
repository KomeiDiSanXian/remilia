package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLatestRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			w.Write([]byte(`{"tag_name":"v1.31.0","assets":[
				{"name":"remilia_Linux_x86_64.tar.gz","browser_download_url":"https://example.com/a.tar.gz"},
				{"name":"checksums.txt","browser_download_url":"https://example.com/checksums.txt"}
			]}`))
		case strings.HasSuffix(r.URL.Path, "/releases"):
			w.Write([]byte(`[
				{"tag_name":"v1.32.0-rc.1","prerelease":true,"draft":false},
				{"tag_name":"v1.31.0","prerelease":false,"draft":false}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldAPI := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldAPI }()

	c := newGitHubClient("KomeiDiSanXian", "remilia", "", 5*time.Second)

	rel, err := c.latestRelease(context.Background(), false)
	if err != nil {
		t.Fatalf("latestRelease: %v", err)
	}
	if rel.TagName != "v1.31.0" {
		t.Errorf("tag = %q, want v1.31.0", rel.TagName)
	}
	if _, ok := rel.Asset("remilia_Linux_x86_64.tar.gz"); !ok {
		t.Error("asset lookup failed")
	}
	if _, ok := rel.Asset("missing"); ok {
		t.Error("missing asset should not be found")
	}

	rel, err = c.latestRelease(context.Background(), true)
	if err != nil {
		t.Fatalf("latestRelease(prerelease): %v", err)
	}
	if rel.TagName != "v1.32.0-rc.1" {
		t.Errorf("prerelease tag = %q, want v1.32.0-rc.1", rel.TagName)
	}
}

func TestLatestReleaseNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	oldAPI := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldAPI }()

	c := newGitHubClient("KomeiDiSanXian", "remilia", "", 5*time.Second)
	if _, err := c.latestRelease(context.Background(), false); err != ErrNoRelease {
		t.Errorf("err = %v, want ErrNoRelease", err)
	}
}

func TestLatestReleaseRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	oldAPI := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldAPI }()

	c := newGitHubClient("KomeiDiSanXian", "remilia", "", 5*time.Second)
	if _, err := c.latestRelease(context.Background(), false); err == nil || !strings.Contains(err.Error(), "限流") {
		t.Errorf("err = %v, want rate limit error", err)
	}
}
