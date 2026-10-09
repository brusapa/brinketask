// Command fakepush is the push service of the end-to-end test (D-73). It
// receives what the application server sends to a device, checks the VAPID
// signature and decrypts the message as a browser would, and keeps it for
// the test, which then hands it to the browser's service worker. It is a
// test tool: it is not part of the application image.
//
// Environment: VAPID_PUBLIC_KEY (the server's key, to check signatures),
// FAKEPUSH_LISTEN_ADDR (default 127.0.0.1:18091) and FAKEPUSH_URL, the base
// URL the application server uses to reach it (default
// http://localhost:18091).
//
// Routes, besides the devices' endpoints at FAKEPUSH_URL/<name>:
//
//	POST /devices/{name}  creates a device; answers PushSubscription.toJSON()
//	GET  /messages        the messages received, decrypted
//	GET  /failures        requests refused for a bad signature or encryption
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/brusapa/brinketask/internal/webpush/webpushtest"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	vapidPublic := os.Getenv("VAPID_PUBLIC_KEY")
	if vapidPublic == "" {
		logger.Error("VAPID_PUBLIC_KEY is required")
		os.Exit(1)
	}
	listen := envOr("FAKEPUSH_LISTEN_ADDR", "127.0.0.1:18091")
	baseURL := envOr("FAKEPUSH_URL", "http://localhost:18091")

	service := webpushtest.NewService(vapidPublic)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /devices/{name}", func(w http.ResponseWriter, r *http.Request) {
		sub, err := service.AddDevice(baseURL, r.PathValue("name"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// The shape of PushSubscription.toJSON(), which the web client sends
		// to the application server.
		writeJSON(w, http.StatusCreated, map[string]any{
			"endpoint": sub.Endpoint,
			"keys":     map[string]string{"p256dh": sub.P256dh, "auth": sub.Auth},
		})
	})
	mux.HandleFunc("GET /messages", func(w http.ResponseWriter, _ *http.Request) {
		type message struct {
			Device  string `json:"device"`
			Payload string `json:"payload"`
			TTL     string `json:"ttl"`
		}
		messages := []message{}
		for _, m := range service.Messages() {
			messages = append(messages, message{Device: m.Device, Payload: string(m.Payload), TTL: m.TTL})
		}
		writeJSON(w, http.StatusOK, messages)
	})
	mux.HandleFunc("GET /failures", func(w http.ResponseWriter, _ *http.Request) {
		failures := service.Failures()
		if failures == nil {
			failures = []string{}
		}
		writeJSON(w, http.StatusOK, failures)
	})
	// Everything else is a device's endpoint.
	mux.Handle("/", service)

	logger.Info("fake push service listening", "addr", listen, "url", baseURL)
	server := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := server.ListenAndServe(); err != nil {
		logger.Error("fake push service", "error", err)
		os.Exit(1)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
