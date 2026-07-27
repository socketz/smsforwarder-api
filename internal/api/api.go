// Package api wires the HTTP endpoints: auth, channel CRUD/test, and SMS upload.
package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/shuttlelab/smsforwarder-api/internal/auth"
	"github.com/shuttlelab/smsforwarder-api/internal/channels"
	"github.com/shuttlelab/smsforwarder-api/internal/ratelimit"
	"github.com/shuttlelab/smsforwarder-api/internal/store"
)

type Server struct {
	st          *store.Store
	reg         *channels.Registry
	limit       *ratelimit.Limiter
	botUsername string // empty => Telegram not configured on this server
}

func NewServer(st *store.Store, reg *channels.Registry, limit *ratelimit.Limiter, botUsername string) *Server {
	return &Server{st: st, reg: reg, limit: limit, botUsername: botUsername}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mw := auth.Middleware(s.st)
	protected := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, mw(h)) }

	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /api/register", s.register)
	mux.HandleFunc("POST /api/login", s.login)

	protected("POST /api/logout", s.logout)
	protected("GET /api/channels", s.listChannels)
	protected("POST /api/channels", s.createChannel)
	protected("GET /api/channels/{id}", s.getChannel)
	protected("PATCH /api/channels/{id}", s.updateChannel)
	protected("DELETE /api/channels/{id}", s.deleteChannel)
	protected("POST /api/channels/{id}/test", s.testChannel)
	protected("POST /api/channels/{id}/bind-code", s.telegramBindCode)
	protected("POST /api/sms", s.postSMS)

	return mux
}

// ---- auth ----

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Username) > 32 {
		badRequest(w, "username must be 3-32 chars")
		return
	}
	if len(req.Password) < 6 {
		badRequest(w, "password must be at least 6 chars")
		return
	}
	if _, err := s.st.GetUserByUsername(r.Context(), req.Username); err == nil {
		writeErr(w, http.StatusConflict, "username already taken")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		serverError(w, err)
		return
	}
	if _, err := s.st.CreateUser(r.Context(), req.Username, hash); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DeviceLabel string `json:"device_label"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	u, err := s.st.GetUserByUsername(r.Context(), strings.TrimSpace(req.Username))
	if err != nil || !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if u.Status != "active" {
		writeErr(w, http.StatusForbidden, "account is not active")
		return
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		serverError(w, err)
		return
	}
	if _, err := s.st.CreateDevice(r.Context(), u.ID, hash, req.DeviceLabel); err != nil {
		serverError(w, err)
		return
	}
	// Plaintext token is returned exactly once; the app stores it.
	writeJSON(w, http.StatusOK, map[string]any{
		"device_token": token,
		"username":     u.Username,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.st.RevokeDevice(r.Context(), auth.DeviceFrom(r.Context())); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- channels ----

type channelDTO struct {
	ID      int64           `json:"id"`
	Type    string          `json:"type"`
	Enabled bool            `json:"enabled"`
	Config  json.RawMessage `json:"config"`
	Ready   bool            `json:"ready"`
}

func toDTO(c store.Channel) channelDTO {
	cfg := c.Config
	if cfg == "" {
		cfg = "{}"
	}
	return channelDTO{
		ID: c.ID, Type: c.Type, Enabled: c.Enabled,
		Config: json.RawMessage(cfg),
		Ready:  channels.Ready(c.Type, c.Config),
	}
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListChannels(r.Context(), auth.UserFrom(r.Context()).ID)
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]channelDTO, 0, len(list))
	for _, c := range list {
		out = append(out, toDTO(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type    string          `json:"type"`
		Enabled *bool           `json:"enabled"`
		Config  json.RawMessage `json:"config"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !channels.KnownType(req.Type) {
		badRequest(w, "unknown channel type")
		return
	}
	config := normalizeConfig(req.Config)
	if err := channels.ValidateConfig(req.Type, config); err != nil {
		badRequest(w, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	c, err := s.st.CreateChannel(r.Context(), auth.UserFrom(r.Context()).ID, req.Type, enabled, config)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(*c))
}

func (s *Server) getChannel(w http.ResponseWriter, r *http.Request) {
	c := s.loadOwnedChannel(w, r)
	if c == nil {
		return
	}
	writeJSON(w, http.StatusOK, toDTO(*c))
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	c := s.loadOwnedChannel(w, r)
	if c == nil {
		return
	}
	var req struct {
		Enabled *bool           `json:"enabled"`
		Config  json.RawMessage `json:"config"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	enabled := c.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	config := c.Config
	if len(req.Config) > 0 {
		config = normalizeConfig(req.Config)
		if err := channels.ValidateConfig(c.Type, config); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	if err := s.st.UpdateChannel(r.Context(), c.UserID, c.ID, enabled, config); err != nil {
		serverError(w, err)
		return
	}
	c.Enabled, c.Config = enabled, config
	writeJSON(w, http.StatusOK, toDTO(*c))
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.st.DeleteChannel(r.Context(), auth.UserFrom(r.Context()).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "channel not found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) testChannel(w http.ResponseWriter, r *http.Request) {
	c := s.loadOwnedChannel(w, r)
	if c == nil {
		return
	}
	if !channels.Ready(c.Type, c.Config) {
		badRequest(w, "channel is not fully configured yet")
		return
	}
	sender, ok := s.reg.Sender(c.Type)
	if !ok {
		writeErr(w, http.StatusNotImplemented, "this channel type is not available yet")
		return
	}
	err := sender.Send(r.Context(), c.Config, channels.Message{
		Sender:    "TEST",
		Content:   "这是一条来自「短信转发」的测试消息",
		Timestamp: store.Now(),
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// telegramBindCode issues a one-time code + deep link. The user opens the link,
// taps Start, and the embedded bot ties their chat_id to this channel.
func (s *Server) telegramBindCode(w http.ResponseWriter, r *http.Request) {
	c := s.loadOwnedChannel(w, r)
	if c == nil {
		return
	}
	if c.Type != channels.TypeTelegram {
		badRequest(w, "not a telegram channel")
		return
	}
	if s.botUsername == "" {
		writeErr(w, http.StatusNotImplemented, "telegram is not configured on the server")
		return
	}
	code, err := randomCode()
	if err != nil {
		serverError(w, err)
		return
	}
	const ttlSeconds = 600 // 10 minutes
	if err := s.st.CreateBindCode(r.Context(), code, c.UserID, c.ID, store.Now()+ttlSeconds); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":     code,
		"deeplink": "https://t.me/" + s.botUsername + "?start=" + code,
	})
}

// ---- SMS upload ----

func (s *Server) postSMS(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	if !s.limit.Allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	var req struct {
		Sender      string `json:"sender"`
		Content     string `json:"content"`
		Timestamp   int64  `json:"timestamp"` // unix milliseconds
		ClientMsgID string `json:"client_msg_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Content == "" {
		badRequest(w, "content is required")
		return
	}
	smsTs := req.Timestamp / 1000 // ms -> s
	if smsTs <= 0 {
		smsTs = store.Now()
	}

	list, err := s.st.ListChannels(r.Context(), u.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	enqueued := 0
	for _, c := range list {
		if !c.Enabled || !channels.Ready(c.Type, c.Config) {
			continue
		}
		ok, err := s.st.EnqueueOutbox(r.Context(), u.ID, c.ID, req.Sender, req.Content, smsTs, req.ClientMsgID)
		if err != nil {
			serverError(w, err)
			return
		}
		if ok {
			enqueued++
		}
	}
	// 200 = the SMS is durably accepted; the app can stop retrying.
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enqueued": enqueued})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- helpers ----

func (s *Server) loadOwnedChannel(w http.ResponseWriter, r *http.Request) *store.Channel {
	id, ok := pathID(w, r)
	if !ok {
		return nil
	}
	c, err := s.st.GetChannel(r.Context(), auth.UserFrom(r.Context()).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "channel not found")
		return nil
	}
	if err != nil {
		serverError(w, err)
		return nil
	}
	return c
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return 0, false
	}
	return id, true
}

// randomCode returns a URL-safe one-time code (valid as a Telegram start param).
func randomCode() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func normalizeConfig(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		badRequest(w, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func badRequest(w http.ResponseWriter, msg string) { writeErr(w, http.StatusBadRequest, msg) }
func serverError(w http.ResponseWriter, err error) {
	writeErr(w, http.StatusInternalServerError, err.Error())
}
