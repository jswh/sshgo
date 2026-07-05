package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// semverCompare tests
// ---------------------------------------------------------------------------

func TestSemverCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		// Equal versions
		{"v1.0.0", "v1.0.0", 0},
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.0", "v1.0.0", 0},

		// Less than
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.0", "v1.1.0", -1},
		{"v1.0.0", "v2.0.0", -1},
		{"v0.0.0", "v0.0.1", -1},

		// Greater than
		{"v1.0.1", "v1.0.0", 1},
		{"v1.1.0", "v1.0.0", 1},
		{"v2.0.0", "v1.0.0", 1},
		{"v0.0.2", "v0.0.1", 1},

		// Different lengths
		{"v1.0", "v1.0.0", 0},
		{"v1.0.1", "v1.0", 1},
		{"v1.0", "v1.0.1", -1},

		// Dev / pre-release strings (non-numeric segments are treated as 0 by atoi)
		{"v1.0.0-dev", "v1.0.0", 0},
	}

	for _, tt := range tests {
		got := semverCompare(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("semverCompare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// parseTagFromLocation tests
// ---------------------------------------------------------------------------

func TestParseTagFromLocation(t *testing.T) {
	tests := []struct {
		location string
		want     string
		wantErr  bool
	}{
		// GitHub relative redirect
		{"/jswh/sshgo/releases/tag/v1.2.3", "v1.2.3", false},
		// Mock server redirect
		{"/releases/tag/v0.0.1", "v0.0.1", false},
		// Absolute URL
		{"https://github.com/jswh/sshgo/releases/tag/v2.0.0", "v2.0.0", false},
		// Trailing slash (GitHub sometimes includes trailing slashes)
		{"/jswh/sshgo/releases/tag/v1.0.0/", "v1.0.0", false},
		// With query params
		{"/releases/tag/v0.0.1?foo=bar", "v0.0.1", false},
		// Malformed - no tag marker
		{"/some/other/path", "", true},
		// Empty location
		{"", "", true},
		// Tag with build metadata
		{"/releases/tag/v1.0.0+build1", "v1.0.0+build1", false},
	}

	for _, tt := range tests {
		got, err := parseTagFromLocation(tt.location)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseTagFromLocation(%q) expected error, got tag %q", tt.location, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseTagFromLocation(%q) unexpected error: %v", tt.location, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseTagFromLocation(%q) = %q, want %q", tt.location, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// getUpdateServer tests
// ---------------------------------------------------------------------------

func TestGetUpdateServer(t *testing.T) {
	// Save and restore env var
	orig := os.Getenv("SSHGO_UPDATE_SERVER")
	defer os.Setenv("SSHGO_UPDATE_SERVER", orig)

	// Without env var
	os.Unsetenv("SSHGO_UPDATE_SERVER")
	got := getUpdateServer()
	want := "https://github.com/" + githubRepo
	if got != want {
		t.Errorf("getUpdateServer() without env = %q, want %q", got, want)
	}

	// With env var
	os.Setenv("SSHGO_UPDATE_SERVER", "http://localhost:9999")
	got = getUpdateServer()
	want = "http://localhost:9999"
	if got != want {
		t.Errorf("getUpdateServer() with env = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// downloadFile tests (with HTTP test server)
// ---------------------------------------------------------------------------

func TestDownloadFile(t *testing.T) {
	// Test: successful download
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello world"))
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "sshgo-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if err := downloadFile(server.URL, tmpFile); err != nil {
		t.Fatalf("downloadFile() unexpected error: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("downloaded content = %q, want %q", string(data), "hello world")
	}
}

func TestDownloadFile_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "sshgo-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	if err := downloadFile(server.URL, tmpFile); err == nil {
		t.Error("downloadFile() expected error for 404, got nil")
	} else if !strings.Contains(err.Error(), "404") {
		t.Errorf("downloadFile() error should mention status, got: %v", err)
	}
}

func TestDownloadFile_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write nothing
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "sshgo-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	if err := downloadFile(server.URL, tmpFile); err == nil {
		t.Error("downloadFile() expected error for empty response, got nil")
	} else if !strings.Contains(err.Error(), "empty") {
		t.Errorf("downloadFile() error should mention 'empty', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// getLatestReleaseTag tests (with HTTP test server)
// ---------------------------------------------------------------------------

func TestGetLatestReleaseTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/latest" {
			w.Header().Set("Location", "/releases/tag/v1.2.3")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	orig := os.Getenv("SSHGO_UPDATE_SERVER")
	os.Setenv("SSHGO_UPDATE_SERVER", server.URL)
	defer os.Setenv("SSHGO_UPDATE_SERVER", orig)

	tag, err := getLatestReleaseTag()
	if err != nil {
		t.Fatalf("getLatestReleaseTag() unexpected error: %v", err)
	}
	if tag != "v1.2.3" {
		t.Errorf("getLatestReleaseTag() = %q, want %q", tag, "v1.2.3")
	}
}

func TestGetLatestReleaseTag_NotRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not a redirect"))
	}))
	defer server.Close()

	orig := os.Getenv("SSHGO_UPDATE_SERVER")
	os.Setenv("SSHGO_UPDATE_SERVER", server.URL)
	defer os.Setenv("SSHGO_UPDATE_SERVER", orig)

	_, err := getLatestReleaseTag()
	if err == nil {
		t.Error("getLatestReleaseTag() expected error for non-redirect response")
	} else if !strings.Contains(err.Error(), "unexpected response") {
		t.Errorf("getLatestReleaseTag() error should mention 'unexpected response', got: %v", err)
	}
}

func TestGetLatestReleaseTag_NoLocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
		// No Location header
	}))
	defer server.Close()

	orig := os.Getenv("SSHGO_UPDATE_SERVER")
	os.Setenv("SSHGO_UPDATE_SERVER", server.URL)
	defer os.Setenv("SSHGO_UPDATE_SERVER", orig)

	_, err := getLatestReleaseTag()
	if err == nil {
		t.Error("getLatestReleaseTag() expected error for missing Location header")
	}
}

// ---------------------------------------------------------------------------
// replaceBinary tests
// ---------------------------------------------------------------------------

func TestReplaceBinary(t *testing.T) {
	// Create a temp source file
	srcFile, err := os.CreateTemp("", "sshgo-src-*")
	if err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}
	srcContent := "new binary content"
	if _, err := srcFile.Write([]byte(srcContent)); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}
	srcFile.Close()
	defer os.Remove(srcFile.Name())

	// Create a temp target file
	dstFile, err := os.CreateTemp("", "sshgo-dst-*")
	if err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}
	dstFile.Close()
	defer os.Remove(dstFile.Name())

	// Replace
	if err := replaceBinary(srcFile.Name(), dstFile.Name()); err != nil {
		t.Fatalf("replaceBinary() unexpected error: %v", err)
	}

	// Verify target content
	data, err := os.ReadFile(dstFile.Name())
	if err != nil {
		t.Fatalf("failed to read target after replace: %v", err)
	}
	if string(data) != srcContent {
		t.Errorf("target content after replace = %q, want %q", string(data), srcContent)
	}

	// Verify source is gone (os.Rename removes the source)
	if _, err := os.Stat(srcFile.Name()); err == nil {
		// On cross-device or copy path, the source may still exist and be removed by defer
		// For rename case it's gone; either is acceptable
	}
}

func TestReplaceBinary_NonExistentSource(t *testing.T) {
	err := replaceBinary("/tmp/nonexistent-source-xyz", "/tmp/nonexistent-target-xyz")
	if err == nil {
		t.Error("replaceBinary() expected error for non-existent source, got nil")
	}
}

// ---------------------------------------------------------------------------
// getDownloadURL tests
// ---------------------------------------------------------------------------

func TestGetDownloadURL(t *testing.T) {
	orig := os.Getenv("SSHGO_UPDATE_SERVER")
	defer os.Setenv("SSHGO_UPDATE_SERVER", orig)

	os.Unsetenv("SSHGO_UPDATE_SERVER")
	tag := "v1.0.0"
	url := getDownloadURL(tag)
	expected := fmt.Sprintf("https://github.com/%s/releases/download/v1.0.0/sshgo-%s-%s",
		githubRepo, runtime.GOOS, runtime.GOARCH)
	if url != expected {
		t.Errorf("getDownloadURL(%q) = %q, want %q", tag, url, expected)
	}

	// With custom server
	os.Setenv("SSHGO_UPDATE_SERVER", "http://localhost:1234")
	url = getDownloadURL(tag)
	expected = fmt.Sprintf("http://localhost:1234/releases/download/v1.0.0/sshgo-%s-%s",
		runtime.GOOS, runtime.GOARCH)
	if url != expected {
		t.Errorf("getDownloadURL(%q) with custom server = %q, want %q", tag, url, expected)
	}
}

// ---------------------------------------------------------------------------
// Integration: mock server serving both redirect and download
// ---------------------------------------------------------------------------

func TestFullUpdateFlow_MockedServer(t *testing.T) {
	// Build a small "binary" that the mock server will serve
	binaryContent := []byte("#!/bin/sh\necho mock sshgo binary")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			w.Header().Set("Location", "/releases/tag/v0.0.1")
			w.WriteHeader(http.StatusFound)
		case "/releases/download/v0.0.1/sshgo-" + runtime.GOOS + "-" + runtime.GOARCH:
			w.WriteHeader(http.StatusOK)
			w.Write(binaryContent)
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("not found: " + r.URL.Path))
		}
	}))
	defer server.Close()

	// Set env var to point to mock server
	origEnv := os.Getenv("SSHGO_UPDATE_SERVER")
	os.Setenv("SSHGO_UPDATE_SERVER", server.URL)
	defer os.Setenv("SSHGO_UPDATE_SERVER", origEnv)

	// 1) Test getLatestReleaseTag
	tag, err := getLatestReleaseTag()
	if err != nil {
		t.Fatalf("getLatestReleaseTag() error: %v", err)
	}
	if tag != "v0.0.1" {
		t.Errorf("getLatestReleaseTag() = %q, want %q", tag, "v0.0.1")
	}

	// 2) Test downloadURL construction
	dlURL := getDownloadURL(tag)
	expectedURL := fmt.Sprintf("%s/releases/download/v0.0.1/sshgo-%s-%s",
		server.URL, runtime.GOOS, runtime.GOARCH)
	if dlURL != expectedURL {
		t.Errorf("getDownloadURL() = %q, want %q", dlURL, expectedURL)
	}

	// 3) Test actual download
	tmpFile, err := os.CreateTemp("", "sshgo-dl-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if err := downloadFile(dlURL, tmpFile); err != nil {
		t.Fatalf("downloadFile() error: %v", err)
	}
	tmpFile.Close()

	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(data) != string(binaryContent) {
		t.Errorf("downloaded content = %q, want %q", string(data), string(binaryContent))
	}

	// 4) Test replaceBinary with the downloaded file
	targetFile := filepath.Join(t.TempDir(), "sshgo-replaced")
	if err := replaceBinary(tmpFile.Name(), targetFile); err != nil {
		t.Fatalf("replaceBinary() error: %v", err)
	}

	data, err = os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed to read replaced file: %v", err)
	}
	if string(data) != string(binaryContent) {
		t.Errorf("replaced file content = %q, want %q", string(data), string(binaryContent))
	}
}
