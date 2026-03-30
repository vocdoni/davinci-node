package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestStaticHandlerRejectsPathTraversalOutsideWebapp(t *testing.T) {
	c := qt.New(t)

	tmpDir := t.TempDir()
	oldWD, err := os.Getwd()
	c.Assert(err, qt.IsNil)
	c.Assert(os.Chdir(tmpDir), qt.IsNil)
	t.Cleanup(func() {
		c.Assert(os.Chdir(oldWD), qt.IsNil)
	})

	c.Assert(os.Mkdir(filepath.Join(tmpDir, webappdir), 0o755), qt.IsNil)
	c.Assert(os.WriteFile(filepath.Join(tmpDir, webappdir, "index.html"), []byte("index"), 0o644), qt.IsNil)

	sentinelPath := filepath.Join(tmpDir, "sentinel.txt")
	sentinelContents := "outside-webapp"
	c.Assert(os.WriteFile(sentinelPath, []byte(sentinelContents), 0o644), qt.IsNil)

	req := httptest.NewRequest(http.MethodGet, "/app../sentinel.txt", nil)
	rec := httptest.NewRecorder()

	staticHandler(rec, req)

	c.Assert(rec.Code, qt.Not(qt.Equals), http.StatusOK)
	c.Assert(rec.Body.String(), qt.Not(qt.Contains), sentinelContents)
}

func TestStaticHandlerRejectsCleanedParentPath(t *testing.T) {
	c := qt.New(t)

	tmpDir := t.TempDir()
	oldWD, err := os.Getwd()
	c.Assert(err, qt.IsNil)
	c.Assert(os.Chdir(tmpDir), qt.IsNil)
	t.Cleanup(func() {
		c.Assert(os.Chdir(oldWD), qt.IsNil)
	})

	c.Assert(os.Mkdir(filepath.Join(tmpDir, webappdir), 0o755), qt.IsNil)
	c.Assert(os.WriteFile(filepath.Join(tmpDir, webappdir, "index.html"), []byte("index"), 0o644), qt.IsNil)

	parentIndexPath := filepath.Join(tmpDir, "index.html")
	parentIndexContents := "parent-index"
	c.Assert(os.WriteFile(parentIndexPath, []byte(parentIndexContents), 0o644), qt.IsNil)

	req := httptest.NewRequest(http.MethodGet, "/app/..", nil)
	rec := httptest.NewRecorder()

	staticHandler(rec, req)

	c.Assert(rec.Code, qt.Equals, http.StatusNotFound)
	c.Assert(rec.Body.String(), qt.Not(qt.Contains), parentIndexContents)
}

func TestStaticHandlerRejectsNestedParentTraversal(t *testing.T) {
	c := qt.New(t)

	tmpDir := t.TempDir()
	oldWD, err := os.Getwd()
	c.Assert(err, qt.IsNil)
	c.Assert(os.Chdir(tmpDir), qt.IsNil)
	t.Cleanup(func() {
		c.Assert(os.Chdir(oldWD), qt.IsNil)
	})

	c.Assert(os.Mkdir(filepath.Join(tmpDir, webappdir), 0o755), qt.IsNil)
	c.Assert(os.WriteFile(filepath.Join(tmpDir, webappdir, "index.html"), []byte("index"), 0o644), qt.IsNil)

	req := httptest.NewRequest(http.MethodGet, "/app/foo/../../secret.txt", nil)
	rec := httptest.NewRecorder()

	staticHandler(rec, req)

	c.Assert(rec.Code, qt.Equals, http.StatusNotFound)
}

func TestStaticHandlerServesAppIndex(t *testing.T) {
	c := qt.New(t)

	tmpDir := t.TempDir()
	oldWD, err := os.Getwd()
	c.Assert(err, qt.IsNil)
	c.Assert(os.Chdir(tmpDir), qt.IsNil)
	t.Cleanup(func() {
		c.Assert(os.Chdir(oldWD), qt.IsNil)
	})

	c.Assert(os.Mkdir(filepath.Join(tmpDir, webappdir), 0o755), qt.IsNil)
	indexContents := "webapp-index"
	c.Assert(os.WriteFile(filepath.Join(tmpDir, webappdir, "index.html"), []byte(indexContents), 0o644), qt.IsNil)

	req := httptest.NewRequest(http.MethodGet, "/app/", nil)
	rec := httptest.NewRecorder()

	staticHandler(rec, req)

	c.Assert(rec.Code, qt.Equals, http.StatusOK)
	c.Assert(rec.Body.String(), qt.Contains, indexContents)
}

func TestStaticHandlerRedirectsUncleanPathToCanonicalAppPath(t *testing.T) {
	c := qt.New(t)

	tmpDir := t.TempDir()
	oldWD, err := os.Getwd()
	c.Assert(err, qt.IsNil)
	c.Assert(os.Chdir(tmpDir), qt.IsNil)
	t.Cleanup(func() {
		c.Assert(os.Chdir(oldWD), qt.IsNil)
	})

	c.Assert(os.Mkdir(filepath.Join(tmpDir, webappdir), 0o755), qt.IsNil)
	c.Assert(os.Mkdir(filepath.Join(tmpDir, webappdir, "assets"), 0o755), qt.IsNil)
	c.Assert(os.WriteFile(filepath.Join(tmpDir, webappdir, "assets", "index.html"), []byte("asset-index"), 0o644), qt.IsNil)

	req := httptest.NewRequest(http.MethodGet, "/app/foo/../assets", nil)
	rec := httptest.NewRecorder()

	staticHandler(rec, req)

	c.Assert(rec.Code, qt.Equals, http.StatusMovedPermanently)
	c.Assert(rec.Header().Get("Location"), qt.Equals, "/app/assets")
}
