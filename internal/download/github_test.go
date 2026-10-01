package download

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDownloadAssetResumesAfterInterruptedResponse(t *testing.T) {
	content := []byte(strings.Repeat("model data ", 100))
	cut := len(content) / 3
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			if got := r.Header.Get("Range"); got != "" {
				t.Errorf("first request has Range %q", got)
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			_, _ = w.Write(content[:cut])
		case 2:
			if got, want := r.Header.Get("Range"), fmt.Sprintf("bytes=%d-", cut); got != want {
				t.Errorf("Range = %q, want %q", got, want)
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cut, len(content)-1, len(content)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(content[cut:])
		default:
			t.Error("unexpected extra download request")
		}
	}))
	defer server.Close()

	asset := &GitHubAsset{
		Digest:             fmt.Sprintf("sha256:%x", sha256.Sum256(content)),
		Size:               int64(len(content)),
		BrowserDownloadURL: server.URL,
	}
	dest := filepath.Join(t.TempDir(), "model.gram")
	if err := NewClient(nil).DownloadAsset(context.Background(), asset, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(content) {
		t.Fatalf("downloaded content differs: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}
}

func TestDownloadAssetRestartsWhenServerIgnoresRange(t *testing.T) {
	content := []byte("complete asset")
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=4-" {
			t.Errorf("Range = %q", got)
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	asset := &GitHubAsset{Digest: "sha256:" + digest, Size: int64(len(content)), BrowserDownloadURL: server.URL}
	dest := filepath.Join(t.TempDir(), "model.gram")
	if err := os.WriteFile(dest+".tmp."+digest, []byte("old!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewClient(nil).DownloadAsset(context.Background(), asset, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(content) {
		t.Fatalf("downloaded content differs: %v", err)
	}
}

func TestDownloadAssetDiscardsCompleteCorruptTemporaryFile(t *testing.T) {
	content := []byte("fresh asset")
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "" {
			t.Errorf("unexpected Range %q", got)
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	asset := &GitHubAsset{Digest: "sha256:" + digest, Size: int64(len(content)), BrowserDownloadURL: server.URL}
	dest := filepath.Join(t.TempDir(), "model.gram")
	if err := os.WriteFile(dest+".tmp."+digest, []byte("bad payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewClient(nil).DownloadAsset(context.Background(), asset, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(content) {
		t.Fatalf("downloaded content differs: %v", err)
	}
}
