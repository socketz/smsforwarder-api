// Package admin is the operator web console (separate from the public API, and by
// default bound to localhost). It shows delivery logs (the admin-only view of
// per-channel failures), manages users, and edits operator settings. Session-cookie
// auth with a single bootstrap credential from env. Modeled on secretary's console.
package admin

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/socketz/smsforwarder-api/internal/store"
)

const cookieName = "sfadmin"
const sessionTTL = 12 * time.Hour

type Server struct {
	st   *store.Store
	user string
	pass string

	mu       sync.Mutex
	sessions map[string]time.Time // token -> expiry
}

func New(st *store.Store, user, pass string) *Server {
	return &Server{st: st, user: user, pass: pass, sessions: map[string]time.Time{}}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/login", s.loginPage)
	mux.HandleFunc("POST /admin/login", s.doLogin)
	mux.HandleFunc("GET /admin/logout", s.logout)

	mux.Handle("GET /admin", s.auth(s.dashboard))
	mux.Handle("GET /admin/users", s.auth(s.users))
	mux.Handle("POST /admin/users/action", s.auth(s.userAction))
	mux.Handle("GET /admin/users/devices", s.auth(s.devices))
	mux.Handle("POST /admin/users/devices/action", s.auth(s.deviceAction))
	mux.Handle("GET /admin/logs", s.auth(s.logs))
	mux.Handle("POST /admin/logs/clear", s.auth(s.logsClear))
	mux.Handle("GET /admin/settings", s.auth(s.settingsPage))
	mux.Handle("POST /admin/settings", s.auth(s.settingsSave))
	mux.Handle("POST /admin/settings/reset-telegram", s.auth(s.resetTelegram))
	mux.Handle("GET /admin/data", s.auth(s.data))
	mux.Handle("POST /admin/data/action", s.auth(s.dataAction))
	return mux
}

// ---- sessions / auth ----

func (s *Server) auth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || !s.validSession(c.Value) {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		h(w, r)
	})
}

func (s *Server) validSession(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.sessions, token)
		return false
	}
	return true
}

func (s *Server) newSession() string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	tok := base64.RawURLEncoding.EncodeToString(buf)
	s.mu.Lock()
	s.sessions[tok] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return tok
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	render(w, "login", map[string]any{"Err": r.URL.Query().Get("e")})
}

func (s *Server) doLogin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	u := r.PostForm.Get("username")
	p := r.PostForm.Get("password")
	okU := subtle.ConstantTimeCompare([]byte(u), []byte(s.user)) == 1
	okP := subtle.ConstantTimeCompare([]byte(p), []byte(s.pass)) == 1
	if !okU || !okP {
		http.Redirect(w, r, "/admin/login?e=1", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: s.newSession(), Path: "/admin",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/admin", MaxAge: -1})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// ---- pages ----

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, _ := s.st.Stats(ctx)
	logs, _ := s.st.ListDeliveryLogs(ctx, "", 0, 20)
	render(w, "dashboard", map[string]any{"Stats": stats, "Logs": logs})
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.st.ListUsersWithCounts(r.Context())
	render(w, "users", map[string]any{"Users": rows})
}

func (s *Server) userAction(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.PostForm.Get("id"), 10, 64)
	switch r.PostForm.Get("action") {
	case "ban":
		_ = s.st.SetUserStatus(r.Context(), id, "banned")
	case "unban":
		_ = s.st.SetUserStatus(r.Context(), id, "active")
	case "revoke":
		_ = s.st.RevokeUserDevices(r.Context(), id)
	case "delete":
		_ = s.st.DeleteUser(r.Context(), id)
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	uid, _ := strconv.ParseInt(r.URL.Query().Get("user"), 10, 64)
	devs, _ := s.st.ListDevices(r.Context(), uid)
	render(w, "devices", map[string]any{"UserID": uid, "Devices": devs})
}

func (s *Server) deviceAction(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.PostForm.Get("id"), 10, 64)
	uid := r.PostForm.Get("user")
	uidInt, _ := strconv.ParseInt(uid, 10, 64)
	switch r.PostForm.Get("action") {
	case "revoke":
		_ = s.st.RevokeDevice(r.Context(), id)
	case "delete":
		_ = s.st.DeleteDevice(r.Context(), id)
	case "delete_revoked":
		_, _ = s.st.DeleteRevokedDevices(r.Context(), uidInt)
	}
	http.Redirect(w, r, "/admin/users/devices?user="+uid, http.StatusSeeOther)
}

func (s *Server) logsClear(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.PostForm.Get("scope") == "all" {
		_, _ = s.st.DeleteAllDeliveryLogs(r.Context())
	} else {
		const days = 30
		_, _ = s.st.DeleteDeliveryLogsOlderThan(r.Context(), store.Now()-days*86400)
	}
	http.Redirect(w, r, "/admin/logs", http.StatusSeeOther)
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	uid, _ := strconv.ParseInt(r.URL.Query().Get("user"), 10, 64)
	logs, _ := s.st.ListDeliveryLogs(r.Context(), status, uid, 200)
	render(w, "logs", map[string]any{"Logs": logs, "Status": status, "User": uid})
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r.Context(), r.URL.Query().Get("saved") != "")
}

func (s *Server) settingsSave(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()
	_ = s.st.SetSetting(ctx, "bot_token", r.PostForm.Get("bot_token"))
	_ = s.st.SetSetting(ctx, "bot_username", r.PostForm.Get("bot_username"))
	http.Redirect(w, r, "/admin/settings?saved=1", http.StatusSeeOther)
}

func (s *Server) resetTelegram(w http.ResponseWriter, r *http.Request) {
	_, _ = s.st.ClearAllTelegramBindings(r.Context())
	http.Redirect(w, r, "/admin/settings?saved=1", http.StatusSeeOther)
}

func (s *Server) data(w http.ResponseWriter, r *http.Request) {
	render(w, "data", map[string]any{"C": s.st.Counts(r.Context())})
}

func (s *Server) dataAction(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	switch r.PostForm.Get("action") {
	case "purge_bindcodes":
		_, _ = s.st.PurgeBindCodes(r.Context(), store.Now())
	case "clear_outbox":
		_, _ = s.st.ClearOutbox(r.Context())
	case "clear_logs":
		_, _ = s.st.DeleteAllDeliveryLogs(r.Context())
	}
	http.Redirect(w, r, "/admin/data", http.StatusSeeOther)
}

func (s *Server) renderSettings(w http.ResponseWriter, ctx context.Context, saved bool) {
	tok, _ := s.st.GetSetting(ctx, "bot_token")
	usr, _ := s.st.GetSetting(ctx, "bot_username")
	render(w, "settings", map[string]any{"BotToken": tok, "BotUsername": usr, "Saved": saved})
}
