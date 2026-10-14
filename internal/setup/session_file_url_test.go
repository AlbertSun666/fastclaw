package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/users"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

// A reply's relative file link is served from its conversation's folder,
// found from the session id: an API conversation's folder is named by the
// caller's conversation key, a project chat's sits under the project.
func TestFileRelativeToSession(t *testing.T) {
	ctx := context.Background()
	st, err := store.NewDBStore("sqlite", "file:"+filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAgent(ctx, &store.AgentRecord{ID: "agt_1", UserID: "u_owner", Name: "a"}); err != nil {
		t.Fatal(err)
	}
	for _, sess := range []struct{ user, key, channel, chat, project string }{
		{"u_enduser", "s-1-api", "api", "snapok:edit:1", ""},
		{"u_owner", "s-2-proj", "web", "s-2-proj", "p_1"},
	} {
		if err := st.SaveSession(ctx, sess.user, "agt_1", sess.key, &store.SessionRecord{Channel: sess.channel, ChatID: sess.chat, ProjectID: sess.project}); err != nil {
			t.Fatal(err)
		}
	}
	ws := workspace.NewLocalFS(t.TempDir())
	put := func(project, chat, name, body string) {
		if err := ws.Put(ctx, "agt_1", project, chat, name, strings.NewReader(body), int64(len(body)), "image/png"); err != nil {
			t.Fatal(err)
		}
	}
	put("", "snapok:edit:1", "out.png", "api image")
	put("p_1", "s-2-proj", "chart.png", "project image")
	s := &Server{dataStore: st, workspaceStore: ws}

	get := func(uid, session, path string) (int, string) {
		r := httptest.NewRequest(http.MethodGet, "/api/agents/agt_1/files/x?sessionId="+session, nil)
		r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{UserID: uid, Role: users.RoleUser, AuthMethod: "session"}))
		r.SetPathValue("id", "agt_1")
		r.SetPathValue("path", path)
		w := httptest.NewRecorder()
		s.handleAgentFile(w, r)
		return w.Code, w.Body.String()
	}
	for _, c := range []struct {
		name, user, session, path string
		code                      int
		body                      string
	}{
		{"owner views an API end-user's image", "u_owner", "s-1-api", "out.png", 200, "api image"},
		{"project chat", "u_owner", "s-2-proj", "chart.png", 200, "project image"},
		{"escape the folder", "u_owner", "s-1-api", "../s-2-proj/chart.png", 400, ""},
		{"unknown session", "u_owner", "s-9-missing", "out.png", 404, ""},
	} {
		code, body := get(c.user, c.session, c.path)
		if code != c.code || (c.body != "" && body != c.body) {
			t.Errorf("%s: %d %q, want %d %q", c.name, code, body, c.code, c.body)
		}
	}
}
