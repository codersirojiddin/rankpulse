package handlers

import (
	"encoding/json"
	"net/http"
)

// writeJSON is a small helper to keep handler bodies free of
// repeated header/encoding boilerplate.
func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
