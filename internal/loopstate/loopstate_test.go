package loopstate

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLoad_MissingFileReturnsZeroState(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("Load on missing file should be nil error: %v", err)
	}
	if s.SessionID != "" || s.Iteration != 0 {
		t.Fatalf("expected zero State, got %+v", s)
	}
}

func TestLoad_EmptyFileReturnsZeroState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.SessionID != "" {
		t.Errorf("expected zero state, got %+v", s)
	}
}

func TestLoad_RejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatalf("expected parse error on invalid JSON")
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop.json")
	want := State{
		SessionID:        "session-abc",
		Iteration:        3,
		MaxIterations:    5,
		StartedAt:        time.Now().UTC().Truncate(time.Second),
		LastCodexReplyID: "reply_99",
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SessionID != want.SessionID ||
		got.Iteration != want.Iteration ||
		got.MaxIterations != want.MaxIterations ||
		got.LastCodexReplyID != want.LastCodexReplyID ||
		!got.StartedAt.Equal(want.StartedAt) {
		t.Fatalf("roundtrip mismatch:\nwant %+v\ngot  %+v", want, got)
	}
}

func TestLoad_ForwardCompatIgnoresUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop.json")
	body := `{"sessionId":"a","iteration":1,"futureKey":"ignored","flags":{"x":true}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.SessionID != "a" || s.Iteration != 1 {
		t.Errorf("expected {a,1,...}, got %+v", s)
	}
}

func TestReset_InitializesFreshState(t *testing.T) {
	s := Reset("fresh-session", 7)
	if s.SessionID != "fresh-session" {
		t.Errorf("SessionID = %q", s.SessionID)
	}
	if s.Iteration != 0 {
		t.Errorf("Iteration = %d, want 0", s.Iteration)
	}
	if s.MaxIterations != 7 {
		t.Errorf("MaxIterations = %d, want 7", s.MaxIterations)
	}
	if s.StartedAt.IsZero() {
		t.Error("StartedAt not set")
	}
	if s.LastCodexReplyID != "" || s.TerminatedReason != "" {
		t.Errorf("residual fields: %+v", s)
	}
}

func TestWithLock_SerializesConcurrentMutations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop.json")
	var wg sync.WaitGroup
	const N = 40
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := WithLock(path, func(s *State) error {
				s.Iteration++
				return nil
			}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("WithLock error: %v", err)
	}
	final, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if final.Iteration != N {
		t.Fatalf("final Iteration = %d, want %d (lock failed to serialize)", final.Iteration, N)
	}
}

func TestWithLock_FnErrorDoesNotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop.json")
	if err := Save(path, State{SessionID: "before", Iteration: 2}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	errBoom := os.ErrClosed // any sentinel
	err := WithLock(path, func(s *State) error {
		s.Iteration = 999
		return errBoom
	})
	if err == nil {
		t.Fatalf("expected error to propagate")
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Iteration != 2 {
		t.Fatalf("Iteration = %d, want 2 (fn error should not persist mutation)", got.Iteration)
	}
}
