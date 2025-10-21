package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var (
	addr     string
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for testing
		},
	}
)

func init() {
	flag.StringVar(&addr, "addr", ":8080", "HTTP service address")
}

// Message represents the WebSocket message format
type Message struct {
	Operation string `json:"operation"`
	Data      string `json:"data"`
}

// handleWebSocket handles a single WebSocket connection with a bash session
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("new connection from %s", r.RemoteAddr)

	// Start bash with a pseudo-terminal
	cmd := exec.Command("bash")
	cmd.Env = append(os.Environ(), "PS1=/app # ")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("failed to start bash: %v", err)
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseAbnormalClosure, "failed to start bash"))
		return
	}
	defer func() {
		ptmx.Close()
		cmd.Process.Kill()
		cmd.Wait()
		log.Printf("connection from %s closed", r.RemoteAddr)
	}()

	var wg sync.WaitGroup

	// Goroutine to read from bash and send to WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("pty read error: %v", err)
				}
				return
			}

			msg := Message{
				Data: string(buf[:n]),
			}

			if err := conn.WriteJSON(msg); err != nil {
				log.Printf("websocket write error: %v", err)
				return
			}
		}
	}()

	// Goroutine to read from WebSocket and send to bash
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			var msg Message
			err := conn.ReadJSON(&msg)
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.Printf("websocket read error: %v", err)
				}
				return
			}

			// Handle different operations
			switch msg.Operation {
			case "stdin", "":
				// Write data to bash stdin
				if _, err := ptmx.Write([]byte(msg.Data)); err != nil {
					log.Printf("pty write error: %v", err)
					return
				}
			case "resize":
				// Handle terminal resize if needed
				log.Printf("resize operation received (not implemented)")
			default:
				log.Printf("unknown operation: %s", msg.Operation)
			}
		}
	}()

	wg.Wait()
}

func main() {
	flag.Parse()

	http.HandleFunc("/ws", handleWebSocket)

	log.Printf("WebSocket bash server starting on %s", addr)
	log.Printf("Connect with: tcsh -url ws://localhost%s/ws", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
