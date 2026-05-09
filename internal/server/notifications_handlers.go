package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/odyssey/agenc/internal/database"
)

// notificationBodyMaxBytes caps the length of the body_markdown column to
// prevent agents from posting unboundedly large content.
const notificationBodyMaxBytes = 256 * 1024

// CreateNotificationRequest is the JSON body for POST /notifications.
type CreateNotificationRequest struct {
	Kind         string `json:"kind"`
	SourceRepo   string `json:"source_repo,omitempty"`
	MissionID    string `json:"mission_id,omitempty"`
	Title        string `json:"title"`
	BodyMarkdown string `json:"body_markdown"`
}

// NotificationResponse is the JSON shape returned for notification reads.
// Times are RFC3339 strings; ReadAt and MissionID are omitted when null.
type NotificationResponse struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	SourceRepo   string `json:"source_repo,omitempty"`
	MissionID    string `json:"mission_id,omitempty"`
	Title        string `json:"title"`
	BodyMarkdown string `json:"body_markdown"`
	CreatedAt    string `json:"created_at"`
	ReadAt       string `json:"read_at,omitempty"`
}

func toNotificationResponse(n *database.Notification) NotificationResponse {
	resp := NotificationResponse{
		ID:           n.ID,
		Kind:         n.Kind,
		SourceRepo:   n.SourceRepo,
		Title:        n.Title,
		BodyMarkdown: n.BodyMarkdown,
		CreatedAt:    n.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if n.MissionID != nil {
		resp.MissionID = *n.MissionID
	}
	if n.ReadAt != nil {
		resp.ReadAt = n.ReadAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return resp
}

func (s *Server) handleCreateNotification(w http.ResponseWriter, r *http.Request) error {
	var req CreateNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid JSON body")
	}
	if req.Kind == "" {
		return newHTTPError(http.StatusBadRequest, "kind is required")
	}
	if req.Title == "" {
		return newHTTPError(http.StatusBadRequest, "title is required")
	}
	if strings.ContainsAny(req.Title, "\r\n") {
		return newHTTPError(http.StatusBadRequest, "title must not contain newlines")
	}

	body := capNotificationBody(req.BodyMarkdown)

	n := &database.Notification{
		ID:           uuid.New().String(),
		Kind:         req.Kind,
		SourceRepo:   req.SourceRepo,
		Title:        req.Title,
		BodyMarkdown: body,
	}
	if req.MissionID != "" {
		missionID := req.MissionID
		n.MissionID = &missionID
	}
	if err := s.db.CreateNotification(n); err != nil {
		s.logger.Printf("CreateNotification failed: %v", err)
		return newHTTPErrorf(http.StatusInternalServerError, "failed to create notification: %v", err)
	}
	writeJSON(w, http.StatusCreated, toNotificationResponse(n))
	return nil
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) error {
	params := database.ListNotificationsParams{
		UnreadOnly: r.URL.Query().Get("unread") == "true",
		SourceRepo: r.URL.Query().Get("repo"),
		Kind:       r.URL.Query().Get("kind"),
	}
	list, err := s.db.ListNotifications(params)
	if err != nil {
		s.logger.Printf("ListNotifications failed: %v", err)
		return newHTTPErrorf(http.StatusInternalServerError, "failed to list notifications: %v", err)
	}
	out := make([]NotificationResponse, 0, len(list))
	for _, n := range list {
		out = append(out, toNotificationResponse(n))
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) handleGetNotification(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if id == "" {
		return newHTTPError(http.StatusBadRequest, "notification id is required")
	}
	resolvedID, err := s.db.ResolveNotificationID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, err.Error())
	}
	n, err := s.db.GetNotification(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "notification not found: "+id)
	}
	writeJSON(w, http.StatusOK, toNotificationResponse(n))
	return nil
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if id == "" {
		return newHTTPError(http.StatusBadRequest, "notification id is required")
	}
	resolvedID, err := s.db.ResolveNotificationID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, err.Error())
	}
	if err := s.db.MarkNotificationRead(resolvedID); err != nil {
		s.logger.Printf("MarkNotificationRead failed for '%v': %v", id, err)
		return newHTTPErrorf(http.StatusInternalServerError, "failed to mark notification read: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleCountUnreadNotifications(w http.ResponseWriter, r *http.Request) error {
	count, err := s.db.CountUnreadNotifications()
	if err != nil {
		s.logger.Printf("CountUnreadNotifications failed: %v", err)
		return newHTTPErrorf(http.StatusInternalServerError, "failed to count notifications: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
	return nil
}

// capNotificationBody truncates body content over notificationBodyMaxBytes
// and appends a footer marker so callers can see the original size.
func capNotificationBody(s string) string {
	if len(s) <= notificationBodyMaxBytes {
		return s
	}
	return s[:notificationBodyMaxBytes] + fmt.Sprintf("\n\n---\n*[truncated: original was %d bytes]*", len(s))
}
