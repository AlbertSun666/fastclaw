package session

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/store"
)

// flakyOwnerStore fails the first session-owner lookups, like a busy
// database right after start-up.
type flakyOwnerStore struct {
	store.Store
	failures atomic.Int32
}

func (f *flakyOwnerStore) LookupSessionOwner(ctx context.Context, agentID, sessionKey string) (string, error) {
	if f.failures.Add(-1) >= 0 {
		return "", errors.New("database is locked")
	}
	return f.Store.LookupSessionOwner(ctx, agentID, sessionKey)
}

// The agent's owner reads an end-user's API conversation. A failed owner
// lookup must not stick: once the database answers, the conversation is
// readable without a restart.
func TestSessionOwnerLookupFailureIsNotCached(t *testing.T) {
	ctx := context.Background()
	db, err := store.NewDBStore("sqlite", "file:"+filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, u := range []*store.UserRecord{
		{ID: "u_owner", Username: "owner", Email: "o@x", Role: "user", Status: "active"},
		{ID: "u_end", Username: "end", Email: "e@x", Role: "app_user", Status: "active", OwnerUserID: "u_owner", ExternalID: "alice"},
		{ID: "u_other", Username: "other", Email: "x@x", Role: "user", Status: "active"},
	} {
		if err := db.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []struct{ user, key string }{{"u_end", "s-1-api"}, {"u_other", "s-2-other"}} {
		if err := db.SaveSession(ctx, s.user, "agt_1", s.key, &store.SessionRecord{Channel: "api", ChatID: s.key}); err != nil {
			t.Fatal(err)
		}
	}

	st := &flakyOwnerStore{Store: db}
	st.failures.Store(1)
	a := NewStoreAdapter(st, "u_owner")
	if got := a.resolveSessionOwner(ctx, "agt_1", "s-1-api"); got != "u_owner" {
		t.Fatalf("failed lookup = %q, want the caller", got)
	}
	if got := a.resolveSessionOwner(ctx, "agt_1", "s-1-api"); got != "u_end" {
		t.Fatalf("after the database recovers = %q, want the end-user", got)
	}
	// Someone else's session stays denied (and that answer is cached).
	if got := a.resolveSessionOwner(ctx, "agt_1", "s-2-other"); got != "u_owner" {
		t.Fatalf("other user's session = %q", got)
	}

	// Concurrent requests share the cache safely (go test -race).
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			NewStoreAdapter(db, "u_owner").resolveSessionOwner(ctx, "agt_1", "s-1-api")
			a.resolveSessionOwner(ctx, "agt_1", "s-2-other")
		}()
	}
	wg.Wait()
}
