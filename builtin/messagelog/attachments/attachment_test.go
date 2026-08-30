package attachments

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestInitialStatus(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		scope string
		age   time.Duration
		msgAt time.Time
		want  string
	}{
		{"all downloads", "all", 24 * time.Hour, now.Add(-48 * time.Hour), StatusPending},
		{"hot window inside", "hot_window", 24 * time.Hour, now.Add(-1 * time.Hour), StatusPending},
		{"hot window outside", "hot_window", 24 * time.Hour, now.Add(-48 * time.Hour), StatusPendingLazy},
		{"hot window zero age", "hot_window", 0, now, StatusPendingLazy},
		{"none never downloads", "none", 24 * time.Hour, now, StatusPendingLazy},
		{"unknown scope lazy", "weird", 24 * time.Hour, now, StatusPendingLazy},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := InitialStatus(c.scope, c.age, c.msgAt); got != c.want {
				t.Fatalf("InitialStatus(%q) = %q, want %q", c.scope, got, c.want)
			}
		})
	}
}

func TestClassifyHTTPError(t *testing.T) {
	cases := []struct {
		status  int
		perma   bool
		expired bool
	}{
		{401, true, false},
		{403, true, false},
		{404, true, true},
		{410, true, true},
		{400, true, false},
		{500, false, false},
		{503, false, false},
	}
	for _, c := range cases {
		err := classifyHTTPError(c.status, "x")
		var pe *permanentError
		isPerma := errors.As(err, &pe)
		if isPerma != c.perma {
			t.Fatalf("status %d: permanent=%v, want %v", c.status, isPerma, c.perma)
		}
		if c.expired {
			if err.Error() == "" {
				t.Fatalf("status %d should carry message", c.status)
			}
		}
	}
}

func TestParseURLExpiry(t *testing.T) {
	cases := []struct {
		url  string
		want int64 // unix seconds; 0 = unknown
	}{
		{"https://cdn.example.com/a.png?expires=1800000000&sig=x", 1800000000},
		{"https://cdn.example.com/a.png?x-expires=1800000000", 1800000000},
		{"https://cdn.example.com/a.png?se=1800000000", 1800000000},
		{"https://cdn.example.com/a.png?e=1800000000", 1800000000},
		{"https://cdn.example.com/a.png?X-Amz-Date=20260830T120000Z&X-Amz-Expires=3600",
			time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC).Unix() + 3600},
		{"https://cdn.example.com/a.png", 0},
		{"://bad", 0},
	}
	for _, c := range cases {
		got := parseURLExpiry(c.url)
		var gotSec int64
		if !got.IsZero() {
			gotSec = got.Unix()
		}
		if gotSec != c.want {
			t.Fatalf("parseURLExpiry(%q) = %d, want %d", c.url, gotSec, c.want)
		}
	}
}

func TestRetryBackoff(t *testing.T) {
	seq := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}
	if got := retryBackoff(seq, 1); got != time.Second {
		t.Fatalf("retry 1 = %v", got)
	}
	if got := retryBackoff(seq, 2); got != 5*time.Second {
		t.Fatalf("retry 2 = %v", got)
	}
	if got := retryBackoff(seq, 9); got != 30*time.Second {
		t.Fatalf("retry 9 = %v", got)
	}
	if got := retryBackoff(nil, 3); got != 30*time.Second {
		t.Fatalf("default = %v", got)
	}
}

func TestFileStoreDedupeAndLimits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "att")
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("hello attachment world")
	key1, size1, err := fs.Put(newBytesReader(content), 0)
	if err != nil {
		t.Fatal(err)
	}
	key2, size2, err := fs.Put(newBytesReader(content), 0)
	if err != nil {
		t.Fatal(err)
	}
	if key1 != key2 || size1 != size2 || size1 != int64(len(content)) {
		t.Fatalf("dedupe mismatch: %s/%d vs %s/%d", key1, size1, key2, size2)
	}
	if fs.TotalSize() != int64(len(content)) {
		t.Fatalf("TotalSize = %d, want %d", fs.TotalSize(), len(content))
	}
	// 单文件上限
	if _, _, err := fs.Put(newBytesReader(content), 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
	keys, _ := fs.Keys()
	if len(keys) != 1 {
		t.Fatalf("Keys = %v, want 1", keys)
	}
	rc, err := fs.Open(key1)
	if err != nil {
		t.Fatal(err)
	}
	rc.Close()
	if err := fs.Delete(key1); err != nil {
		t.Fatal(err)
	}
	if fs.TotalSize() != 0 {
		t.Fatalf("TotalSize after delete = %d", fs.TotalSize())
	}
	if err := fs.Delete(key1); err != nil {
		t.Fatalf("delete missing should be nil, got %v", err)
	}
}

func TestDownloadStateMachine(t *testing.T) {
	dir := t.TempDir()
	var srv *httptest.Server
	status := 200
	body := []byte("payload-12345")
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		if status == 200 {
			w.Write(body)
		}
	}))
	defer srv.Close()

	db := newTestDB(t, filepath.Join(dir, "test.db"))
	m, err := NewManager(db, Config{
		Dir:         filepath.Join(dir, "att"),
		Concurrency: 2,
		MaxPending:  100,
		Retries:     2,
		Backoff:     []time.Duration{time.Second, 2 * time.Second},
		MaxSize:     1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	m.budget = newDiskBudget(0, m.store, nil) // 不限磁盘

	// 成功路径
	id := insertRow(t, db, Row{Type: "image", URL: srv.URL + "/a.png", Name: "a.png", Status: StatusPending})
	if _, _, err := m.dl.downloadOne(t.Context(), id, false); err != nil {
		t.Fatalf("download failed: %v", err)
	}
	var row Row
	db.First(&row, id)
	if row.Status != StatusReady || row.StorageKey == "" {
		t.Fatalf("row = %+v, want ready", row)
	}
	if row.SHA256 == "" {
		t.Fatal("sha256 empty")
	}

	// 永久失败：404 → expired
	id404 := insertRow(t, db, Row{Type: "image", URL: srv.URL + "/gone.png", Name: "gone.png", Status: StatusPending})
	status = 404
	if _, _, err := m.dl.downloadOne(t.Context(), id404, false); err == nil {
		t.Fatal("expected error for 404")
	}
	row = Row{}
	db.First(&row, id404)
	if row.Status != StatusExpired {
		t.Fatalf("row status = %q, want expired", row.Status)
	}

	// 临时失败：500 → pending_retry，重试次数递增；耗尽 → pending_lazy
	id5xx := insertRow(t, db, Row{Type: "image", URL: srv.URL + "/err.png", Name: "err.png", Status: StatusPending})
	status = 503
	row0 := Row{ID: id5xx, RetryCount: 0}
	next, ok := m.dl.scheduleRetry(row0, &temporaryError{err: errors.New("boom")})
	if !ok {
		t.Fatal("expected retry scheduled")
	}
	if !next.After(time.Now()) {
		t.Fatal("retry time should be in future")
	}
	row = Row{}
	db.First(&row, id5xx)
	if row.Status != StatusPendingRetry || row.RetryCount != 1 {
		t.Fatalf("row = %+v", row)
	}
	rowExhausted := Row{ID: id5xx, RetryCount: 3}
	if _, ok := m.dl.scheduleRetry(rowExhausted, &temporaryError{err: errors.New("boom")}); ok {
		t.Fatal("retries should be exhausted")
	}
	row = Row{}
	db.First(&row, id5xx)
	if row.Status != StatusPendingLazy {
		t.Fatalf("row status = %q, want pending_lazy", row.Status)
	}

	// 无来源：pending_lazy
	idNoURL := insertRow(t, db, Row{Type: "file", Name: "x.bin", Status: StatusPending})
	if _, _, err := m.dl.downloadOne(t.Context(), idNoURL, false); err == nil {
		t.Fatal("expected error for no URL")
	}
	row = Row{}
	db.First(&row, idNoURL)
	if row.Status != StatusPendingLazy {
		t.Fatalf("row status = %q, want pending_lazy", row.Status)
	}
}
