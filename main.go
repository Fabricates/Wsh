package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/gorilla/websocket"
)

var (
	urlStr     string
	maxSize    int
	uploadTmpl *template.Template
	dataTmpl   *template.Template
	// resizeTmpl    *template.Template
	uploadEmpty   string
	uploadTmplStr string
	dataTmplStr   string
	resizeTmplStr string
	newline       string
	eof           string
	eofLen        int
)

func init() {
	flag.StringVar(&urlStr, "url", "", "WebSocket endpoint (ws:// or wss://)")
	flag.StringVar(&uploadTmplStr, "up", `{"operation":"stdin","data":"{{.data}}"}`, "Upstream template")
	flag.StringVar(&dataTmplStr, "data", `{{.data}}`, "Downstream data field")
	flag.StringVar(&resizeTmplStr, "resize", `{"operation":"resize","rows":{{.rows}},"cols":{{.cols}}}`, "Resize window template")
	flag.StringVar(&newline, "newline", `\r`, "New line separator")
	flag.StringVar(&eof, "eof", `/app # `, "EOF of the response message")
	flag.IntVar(&maxSize, "max", 4159, "Maximum payload size per message (bytes)")
}

func parse(t *template.Template, ctx any) []byte {
	var buf = bytes.NewBuffer([]byte{})
	if e := t.ExecuteTemplate(buf, "", ctx); e != nil {
		panic(e)
	}
	return buf.Bytes()
}

func main() {
	flag.Parse()

	if urlStr == "" {
		log.Fatal("-url is required")
	}

	u, err := url.Parse(urlStr)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") {
		log.Fatalf("invalid websocket url: %v", err)
	}

	uploadTmpl = template.Must(template.New("").Parse(uploadTmplStr))
	dataTmpl = template.Must(template.New("").Parse(dataTmplStr))
	// resizeTmpl = template.Must(template.New("resize").Parse(uploadTmplStr))

	uploadEmpty = string(parse(uploadTmpl, map[string]any{"data": ""}))

	eofLen = len(eof)

	// dial websocket
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(urlStr, nil)
	if err != nil {
		log.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	fmt.Printf("[connected]\n%s", eof)

	// handle interrupts
	ints := make(chan os.Signal, 1)
	signal.Notify(ints, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ints
		fmt.Printf("\nclosing")
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		os.Exit(0)
	}()

	// stdin loop: accepts commands starting with '/' or sends content as data
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "/") {
			command(line, conn)
			continue
		}
		// normal send
		if err := send([]byte(line+newline), conn); err != nil {
			fmt.Printf("\nsend error: %v\n%s", err, eof)
			continue
		}
		processIncoming(conn)
	}
	if err := s.Err(); err != nil {
		fmt.Printf("\nstdin error: %v\n%s", err, eof)
	}
}

func command(line string, conn *websocket.Conn) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "/upload":
		if len(parts) < 3 {
			fmt.Println("usage: /upload <localpath> <remotepath>")
			return
		}
		local := parts[1]
		remote := parts[2]
		if err := upload(local, remote, conn); err != nil {
			fmt.Printf("upload failed: %v\n", err)
		} else {
			fmt.Printf("upload queued: %s -> %s\n", local, remote)
		}
	case "/download":
		if len(parts) < 3 {
			fmt.Println("usage: /download <localpath> <remotepath>")
			return
		}
		local := parts[1]
		remote := parts[2]
		if err := download(local, remote, conn); err != nil {
			fmt.Printf("download failed: %v\n", err)
		} else {
			fmt.Printf("download requested: %s <- %s\n", local, remote)
		}
	case "/quit", "/exit":
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		os.Exit(0)
	default:
		// send unknown commands to remote as data (strip leading '/')
		payload := []byte(strings.TrimPrefix(line, "/") + "\n")
		if err := send(payload, conn); err != nil {
			fmt.Printf("failed to send command: %v\n", err)
		}
	}
}

func upload(local, remote string, conn *websocket.Conn) error {
	// Step 1: read file
	b, err := os.ReadFile(local)
	if err != nil {
		return err
	}

	// Step 2: encode to base64
	enc := base64.StdEncoding.EncodeToString(b)

	// Step 3: write temp .b64 file
	b64Path := remote + ".b64"

	// Step 4: split base64 content into chunks and send each via template
	overhead := len(uploadEmpty)
	allowed := maxSize - overhead
	if allowed <= 0 {
		return errors.New("max size too small for template overhead")
	}

	// Send chunks
	for i := 0; i < len(enc); i += allowed {
		end := i + allowed
		if end > len(enc) {
			end = len(enc)
		}
		chunk := enc[i:end]
		msg := parse(uploadTmpl, map[string]any{"data": chunk})
		if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
			return fmt.Errorf("failed to send chunk: %w", err)
		}
	}

	// Step 5: send decode command to decode base64 file to target and remove temp file
	decodeCmd := fmt.Sprintf("base64 -d %s > %s && rm %s\n", b64Path, remote, b64Path)
	decodeMsgData := parse(uploadTmpl, map[string]any{"data": decodeCmd})
	if err := conn.WriteMessage(websocket.TextMessage, []byte(decodeMsgData)); err != nil {
		return fmt.Errorf("failed to send decode command: %w", err)
	}

	processIncoming(conn)

	return nil
}

func download(local, remote string, conn *websocket.Conn) error {
	// Send download command via template with remote path in data field
	cmd := fmt.Sprintf("cat %s | base64 -w 0 ; echo%s", remote, newline)
	msg := parse(uploadTmpl, map[string]any{"data": cmd})

	i := strings.LastIndex(local, string(os.PathSeparator))
	if i > 0 {
		if err := os.MkdirAll(local[:i], os.ModeAppend); err != nil {
			return fmt.Errorf("unable to create folder: %s", local[:i])
		}
	}

	f, err := os.OpenFile(local, os.O_CREATE, os.ModeAppend)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		return fmt.Errorf("failed to send download command: %w", err)
	}

	done := false
	r, w := io.Pipe()

	// Run io.Copy in a goroutine so it doesn't block the message reading loop
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(f, base64.NewDecoder(base64.StdEncoding, r))
		copyDone <- copyErr
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("\nread error: %v\n%s", err, eof)
			return nil
		}

		if l := len(msg); l > len(eof) && string(msg[l-eofLen:]) == eof {
			msg = msg[:l-eofLen]
			done = true
		}

		if _, err = w.Write(msg); err != nil {
			fmt.Printf("\nwrite error: %v\n%s", err, eof)
		}

		if done {
			w.Close() // Close the pipe writer to signal EOF to the decoder
			if copyErr := <-copyDone; copyErr != nil {
				fmt.Printf("\ncopy error: %v\n%s", copyErr, eof)
			}
			fmt.Printf("\ndownloaded: %s -> %s\n%s", remote, local, eof)
			break
		}
	}
	return nil
}

func processIncoming(conn *websocket.Conn) {
	sb := strings.Builder{}
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// Check if it's a timeout or normal closure
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				fmt.Printf("\nconnection closed\n%s", eof)
				return
			}
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				// Timeout - assume no more messages, continue normally
				return
			}
			fmt.Printf("\nread error: %v\n%s", err, eof)
			return
		}
		// Extract data from downstream template
		line := string(extract(msg))
		sb.WriteString(line)
		fmt.Print(line)
		if s := sb.String(); strings.HasSuffix(s, eof) && sb.Len() > eofLen {
			break
		}
	}
}

func extract(msg []byte) []byte {
	// Try JSON parse first
	var j map[string]interface{}
	if err := json.Unmarshal(msg, &j); err == nil {
		if dataVal, ok := j["data"].(string); ok {
			return []byte(dataVal)
		}
	}

	// Simple template match: if template is "prefix$suffix", extract middle
	return parse(dataTmpl, j)
}

func send(data []byte, conn *websocket.Conn) error {
	return sendWithTemplate(data, conn)
}

func sendWithTemplate(data []byte, conn *websocket.Conn) error {
	// simple replace
	msg := parse(uploadTmpl, map[string]any{"data": string(data)})
	if len(msg) <= maxSize {
		return conn.WriteMessage(websocket.TextMessage, []byte(msg))
	}

	// chunk the data itself if tpl contains single $ occurrence
	overhead := len(uploadEmpty)
	allowed := maxSize - overhead
	if allowed <= 0 {
		return errors.New("max size too small for template overhead")
	}
	raw := string(data)
	for i := 0; i < len(raw); i += allowed {
		end := i + allowed
		if end > len(raw) {
			end = len(raw)
		}
		chunk := raw[i:end]
		m := parse(uploadTmpl, map[string]any{"data": chunk})
		if err := conn.WriteMessage(websocket.TextMessage, []byte(m)); err != nil {
			return err
		}
	}
	return nil
}
