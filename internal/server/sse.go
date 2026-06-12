package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEEvent est envoyé au client pendant la conversion.
type SSEEvent struct {
	Type    string `json:"type"`    // "progress" | "result" | "error"
	Stage   string `json:"stage"`   // ex: "translating batch 3/12"
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	File    string `json:"file"`    // nom du fichier en cours
	Payload string `json:"payload"` // SRT sérialisé en base64 à la complétion
}

// WriteSSE sérialise event en Server-Sent Events et le flush immédiatement.
// Il renvoie une erreur si le ResponseWriter n'implémente pas http.Flusher ou
// si l'écriture échoue.
func WriteSSE(w http.ResponseWriter, event SSEEvent) error {
	f, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("sse: ResponseWriter ne supporte pas Flush")
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("sse: sérialisation JSON: %w", err)
	}

	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data); err != nil {
		return fmt.Errorf("sse: écriture: %w", err)
	}
	f.Flush()
	return nil
}
