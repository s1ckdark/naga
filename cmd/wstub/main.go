// wstub — minimal WebSocket worker stub for AI orchestration testing.
// Opens N parallel WS connections to /ws?device_id=ID using existing device IDs
// from the Hydra DB. Each connection just holds open and answers pings, so the
// supervisor's scheduleQueue sees multiple "connected" workers and may invoke
// the AI tiebreaker when their rule-based scores tie.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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
	deviceIDs := flag.String("device-ids", "", "Comma-separated device IDs to impersonate")
	flag.Parse()

	if *deviceIDs == "" {
		fmt.Fprintln(os.Stderr, "error: --device-ids required (comma-separated)")
		os.Exit(1)
	}

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
			runWorker(ctx, *server, deviceID)
		}(id)
	}
	wg.Wait()
}

func runWorker(ctx context.Context, server, deviceID string) {
	u, err := url.Parse(server)
	if err != nil {
		log.Printf("[%s] bad URL: %v", deviceID, err)
		return
	}
	q := u.Query()
	q.Set("device_id", deviceID)
	u.RawQuery = q.Encode()

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
	}
}
