package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestHealthz_DBUp_HeartbeatRecent_Returns200OK covers spec §3.5 happy path.
func TestHealthz_DBUp_HeartbeatRecent_Returns200OK(t *testing.T) {
	db := openMemDB(t)
	hb := NewHeartbeatCell(time.Now)
	hb.Touch()
	brief := NewBriefCell()
	brief.SetPath("/etc/regatta/brief.md")
	h := Handler(Dependencies{DB: db, Heartbeat: hb, Brief: brief, Version: "v0.1.0"})
	req := httptest.NewRequest("GET", "/healthz", nil)
	req.Header.Set("Accept", "application/json")
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rr.Code)
	}
	var got Response
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusOK {
		t.Fatalf("status: want ok, got %s", got.Status)
	}
}

// TestHealthz_DBDown_Returns503 covers down-aggregation + 503 contract.
func TestHealthz_DBDown_Returns503(t *testing.T) {
	db := openMemDB(t)
	_ = db.Close() // induce ping failure
	hb := NewHeartbeatCell(time.Now)
	hb.Touch()
	brief := NewBriefCell()
	brief.SetPath("/x")
	h := Handler(Dependencies{DB: db, Heartbeat: hb, Brief: brief, Version: "v"})
	req := httptest.NewRequest("GET", "/healthz", nil)
	req.Header.Set("Accept", "application/json")
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: want 503, got %d", rr.Code)
	}
}

// TestHealthz_NoBriefLoaded_StaysOK pins brief-not-configured as OK (not degraded).
func TestHealthz_NoBriefLoaded_StaysOK(t *testing.T) {
	db := openMemDB(t)
	hb := NewHeartbeatCell(time.Now)
	hb.Touch()
	brief := NewBriefCell()
	// no SetPath — operator running without a program brief (the
	// default --spawner=claude flow); empty path must report OK, not
	// degraded, so /healthz keeps signaling real degradation.
	h := Handler(Dependencies{DB: db, Heartbeat: hb, Brief: brief, Version: "v"})
	req := httptest.NewRequest("GET", "/healthz", nil)
	req.Header.Set("Accept", "application/json")
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rr.Code)
	}
	var got Response
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Status != StatusOK {
		t.Fatalf("status: want ok (brief is optional), got %s body=%v", got.Status, got)
	}
	if got.Checks["brief"].Status != "ok" {
		t.Fatalf("brief check status=%q want ok", got.Checks["brief"].Status)
	}
}

// TestHealthz_HeartbeatStale_ReturnsDegradedNot503 covers degraded 200 contract.
func TestHealthz_HeartbeatStale_ReturnsDegradedNot503(t *testing.T) {
	db := openMemDB(t)
	hb := NewHeartbeatCell(time.Now)
	// never Touch ⇒ stale
	brief := NewBriefCell()
	brief.SetPath("/x")
	h := Handler(Dependencies{DB: db, Heartbeat: hb, Brief: brief, Version: "v"})
	req := httptest.NewRequest("GET", "/healthz", nil)
	req.Header.Set("Accept", "application/json")
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200 for degraded, got %d", rr.Code)
	}
	var got Response
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Status != StatusDegraded {
		t.Fatalf("want degraded, got %s", got.Status)
	}
}

// TestHealthz_NoAccept_ReturnsJSONEnvelope asserts the plaintext W7.0 shim is gone (Wave C2).
func TestHealthz_NoAccept_ReturnsJSONEnvelope(t *testing.T) {
	db := openMemDB(t)
	hb := NewHeartbeatCell(time.Now)
	hb.Touch()
	brief := NewBriefCell()
	brief.SetPath("/x")
	h := Handler(Dependencies{DB: db, Heartbeat: hb, Brief: brief, Version: "v"})
	req := httptest.NewRequest("GET", "/healthz", nil) // no Accept header
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type=%q want application/json", ct)
	}
	var got Response
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != StatusOK {
		t.Fatalf("status=%q want ok", got.Status)
	}
}

// TestHeartbeatPool_SetMaxOpenConns1_NoContention covers spec §3.5 dedicated pool.
func TestHeartbeatPool_SetMaxOpenConns1_NoContention(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := ConfigureHeartbeatPool(db); err != nil {
		t.Fatalf("ConfigureHeartbeatPool: %v", err)
	}
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("want MaxOpenConns=1, got %d", got)
	}
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
