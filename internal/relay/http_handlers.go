package relay

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/relayapi"
)

func (s *Server) handleDirectory(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeRequest(w, r) {
		return
	}
	if r.URL.Path == "/directory/discoverable" {
		s.handleDiscoverableDirectory(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/directory/devices/") {
		s.handleDirectoryDeviceLookup(w, r)
		return
	}
	mailbox := strings.TrimPrefix(r.URL.Path, "/directory/mailboxes/")
	if mailbox == "" || strings.Contains(mailbox, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		entry, err := s.queue.GetDirectoryEntry(mailbox)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				s.writeJSONError(w, http.StatusNotFound, "directory entry not found")
				return
			}
			s.writeJSONError(w, http.StatusInternalServerError, "load directory entry")
			return
		}
		s.writeJSON(w, http.StatusOK, entry)
	case http.MethodPut:
		var entry relayapi.SignedDirectoryEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			s.writeJSONError(w, http.StatusBadRequest, "decode directory entry")
			return
		}
		if entry.Entry.Mailbox != mailbox {
			s.writeJSONError(w, http.StatusBadRequest, "directory mailbox mismatch")
			return
		}
		if err := relayapi.VerifySignedDirectoryEntry(entry); err != nil {
			s.logger.Warn("reject directory entry", "mailbox", mailbox, "error", err)
			s.writeJSONError(w, http.StatusBadRequest, "invalid directory entry")
			return
		}
		if err := s.queue.PutDirectoryEntry(entry); err != nil {
			if errors.Is(err, ErrDirectoryConflict) {
				s.logger.Warn("reject directory entry conflict", "mailbox", mailbox, "error", err)
				s.writeJSONError(w, http.StatusConflict, "directory entry conflicts with existing mailbox owner")
				return
			}
			s.writeJSONError(w, http.StatusInternalServerError, "save directory entry")
			return
		}
		s.writeJSON(w, http.StatusOK, entry)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDiscoverableDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries, err := s.queue.ListDiscoverableEntries()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "load discoverable directory entries")
		return
	}
	s.writeJSON(w, http.StatusOK, relayapi.ListDirectoryResponse{Entries: entries})
}

func (s *Server) handleDirectoryDeviceLookup(w http.ResponseWriter, r *http.Request) {
	mailbox := strings.TrimPrefix(r.URL.Path, "/directory/devices/")
	if mailbox == "" || strings.Contains(mailbox, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entry, err := s.queue.LookupDirectoryEntryByDeviceMailbox(mailbox)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.writeJSONError(w, http.StatusNotFound, "directory entry not found")
			return
		}
		s.writeJSONError(w, http.StatusInternalServerError, "load directory entry")
		return
	}
	s.writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleRendezvous(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeRequest(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/rendezvous/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		payloads, err := s.queue.GetRendezvousPayloads(id, time.Now().UTC())
		if err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, "load rendezvous payloads")
			return
		}
		s.writeJSON(w, http.StatusOK, relayapi.GetRendezvousResponse{Payloads: payloads})
	case http.MethodPut:
		decision := s.allowRateLimit("rendezvous:"+r.RemoteAddr, time.Now().UTC())
		if !decision.Allowed {
			s.logger.Warn("rate limit exceeded", "scope", "rendezvous", "remote_addr", r.RemoteAddr, "limit", decision.Limit, "count", decision.Count, "window_started_at", decision.WindowStartedAt)
			s.writeJSONError(w, http.StatusTooManyRequests, "relay rate limit exceeded")
			return
		}
		var request relayapi.PutRendezvousRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			s.writeJSONError(w, http.StatusBadRequest, "decode rendezvous payload")
			return
		}
		now := time.Now().UTC()
		if err := validateRendezvousPayload(request.Payload, now, s.options.MaxMessageBytes); err != nil {
			s.logger.Warn("reject rendezvous payload", "id", id, "error", err)
			s.writeJSONError(w, http.StatusBadRequest, "invalid rendezvous payload")
			return
		}
		existing, err := s.queue.GetRendezvousPayloads(id, now)
		if err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, "load rendezvous payloads")
			return
		}
		if len(existing) >= maxRendezvousPayloads {
			s.writeJSONError(w, http.StatusConflict, "rendezvous slot is full")
			return
		}
		if err := s.queue.PutRendezvousPayload(id, request.Payload); err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, "save rendezvous payload")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := s.queue.DeleteRendezvous(id); err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, "delete rendezvous payloads")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.landing.Execute(w, nil); err != nil {
		http.Error(w, "render landing page", http.StatusInternalServerError)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) authorizeRequest(w http.ResponseWriter, r *http.Request) bool {
	if s.options.AuthToken == "" || r.Header.Get(authHeader) == s.options.AuthToken {
		return true
	}
	s.logger.Warn("reject unauthorized request", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
	http.Error(w, "relay auth token is required", http.StatusUnauthorized)
	return false
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeJSONError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, struct {
		Message string `json:"message"`
	}{Message: message})
}
