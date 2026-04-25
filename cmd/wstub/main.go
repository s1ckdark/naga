// wstub — minimal WebSocket worker stub for AI orchestration testing.
// Opens N parallel WS connections to /ws?device_id=ID using existing device IDs
// from the Hydra DB. Each connection just holds open and answers pings, so the
// supervisor's scheduleQueue sees multiple "connected" workers and may invoke
// the AI tiebreaker when their rule-based scores tie.
//
// Optionally posts capabilities to /api/devices/<id>/capabilities right after
// the WS handshake so capability-aware scheduling can be exercised. Pass
// --capabilities=gpu,compute (or any CSV) to enable.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	server := flag.String("server", "ws://127.0.0.1:8080/ws", "Hydra WebSocket URL")
	httpServer := flag.String("server-http", "http://127.0.0.1:8080", "Hydra HTTP base URL for capability registration")
	deviceIDs := flag.String("device-ids", "", "Comma-separated device IDs to impersonate")
	capabilities := flag.String("capabilities", "", "Comma-separated capability identifiers to register for each impersonated device (empty = skip registration)")
	flag.Parse()

	if *deviceIDs == "" {
		fmt.Fprintln(os.Stderr, "error: --device-ids required (comma-separated)")
		os.Exit(1)
	}

	caps := splitCSV(*capabilities)

	ids := strings.Split(*deviceIDs, ",")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down")
		cancel()
	}()

	var wg sync.WaitGroup
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		wg.Add(1)
		go func(deviceID string) {
			defer wg.Done()
			runWorker(ctx, *server, *httpServer, deviceID, caps)
		}(id)
	}
	wg.Wait()
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func runWorker(ctx context.Context, server, httpServer, deviceID string, caps []string) {
	u, err := url.Parse(server)
	if err != nil {
		log.Printf("[%s] bad URL: %v", deviceID, err)
		return
	}
	q := u.Query()
	q.Set("device_id", deviceID)
	u.RawQuery = q.Encode()

	registered := false

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
		if err != nil {
			log.Printf("[%s] dial: %v (retry 5s)", deviceID, err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Printf("[%s] connected", deviceID)

		// Register capabilities once after the first successful connect, and
		// again after every reconnect so server restarts are recoverable.
		if len(caps) > 0 {
			if err := registerCapabilities(ctx, httpServer, deviceID, caps); err != nil {
				log.Printf("[%s] capability register failed: %v", deviceID, err)
			} else {
				registered = true
				log.Printf("[%s] capabilities registered: %v", deviceID, caps)
			}
		}

		conn.SetPingHandler(func(string) error {
			return conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(5*time.Second))
		})

		// Read loop — log task assignments, ack everything else.
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				log.Printf("[%s] read err: %v (reconnecting)", deviceID, err)
				conn.Close()
				time.Sleep(2 * time.Second)
				break
			}
			log.Printf("[%s] recv: %s", deviceID, string(data))
		}
		_ = registered // keep variable referenced in case future logic needs it
	}
}

func registerCapabilities(ctx context.Context, httpServer, deviceID string, caps []string) error {
	body, err := json.Marshal(map[string]any{"capabilities": caps})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	endpoint := strings.TrimRight(httpServer, "/") + "/api/devices/" + url.PathEscape(deviceID) + "/capabilities"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}
	return nil
}
