package server

import (
	"errors"
	"net/http"

	"github.com/Jacobsonradical/PerchBoard/internal/reader"
)

func (s *Server) handleReader(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	raw, service := r.URL.Query().Get("url"), r.URL.Query().Get("service")
	if _, err := reader.ValidateURL(raw); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if service != "local" && service != "archive" {
		writeErr(w, http.StatusBadRequest, errors.New("unknown reading service"))
		return
	}
	article, err := reader.Fetch(r.Context(), raw, service)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, article)
}
