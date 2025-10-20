// Package main implement a websocket-based shell client
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
	"regexp"
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
	uploadEmpty         string
	uploadTmplStr       string
	dataTmplStr         string
	resizeTmplStr       string
	newline             string
	ps1                 = "> "
	debug               bool
	escape              = regexp.MustCompile("\x1b\\[([0-9]+;)?[0-9]*[a-zA-Z]")
	escapes             = func(raw []byte) string { return string(escape.ReplaceAll(raw, []byte{})) }
	maxLineBufferLength = 1024
)

func init() {
	flag.StringVar(&urlStr, "url", "", "WebSocket endpoint (ws:// or wss://)")
	flag.StringVar(&uploadTmplStr, "up", `{"operation":"stdin","data":"{{.data}}"}`, "Upstream template")
	flag.StringVar(&dataTmplStr, "data", `{{.data}}`, "Downstream data field")
	flag.StringVar(&resizeTmplStr, "resize", `{"operation":"resize","rows":{{.rows}},"cols":{{.cols}}}`, "Resize window template")
	flag.StringVar(&newline, "newline", "\r", "New line separator")
	flag.IntVar(&maxSize, "max", 4159, "Maximum payload size per message (bytes)")
	flag.BoolVar(&debug, "debug", false, "Enable debug output")
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

	// dial websocket
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	connection, _, err := dialer.Dial(urlStr, nil)
	if err != nil {
		log.Fatalf("dial ws: %v", err)
	}
	defer connection.Close()

	conn := &session{connection}

	fmt.Printf("[connected]")

	// handle interrupts
	ints := make(chan os.Signal, 1)
	signal.Notify(ints, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ints
		fmt.Printf("\nclosing")
		conn.Send(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		os.Exit(0)
	}()

	// change PS1
	if err = process(fmt.Sprintf("export PS1='%s';unset LS_COLORS; export TERM=xterm-mono", ps1), conn); err != nil {
		panic(err)
	}

	// stdin loop: accepts commands starting with '/' or sends content as data
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "/") {
			command(line, conn)
			fmt.Printf("\r\n%s", ps1)
			continue
		}
		// normal send
		if err := process(line, conn); err != nil {
			fmt.Printf("\nsend error: %v\n%s", err, ps1)
			continue
		}
	}
	if err := s.Err(); err != nil {
		fmt.Printf("\nstdin error: %v\n%s", err, ps1)
	}
}

func command(line string, conn *session) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "/get":
		if len(parts) < 3 {
			fmt.Println("usage: /get <localpath> <remotepath>")
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
		conn.Send(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		os.Exit(0)
	default:
		// send unknown commands to remote as data (strip leading '/')
		payload := strings.TrimPrefix(line, "/")
		if err := sendWithTemplate(payload, conn); err != nil {
			fmt.Printf("failed to send command: %v\n", err)
		}
	}
}

func download(local, remote string, conn *session) error {
	i := strings.LastIndex(local, string(os.PathSeparator))
	if i > 0 {
		if err := os.MkdirAll(local[:i], os.ModePerm); err != nil {
			return fmt.Errorf("unable to create folder: %s", local[:i])
		}
	}

	f, err := os.OpenFile(local, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.ModePerm)
	if err != nil {
		return err
	}
	defer f.Close()

	// Send download command via template with remote path in data field
	cmd := fmt.Sprintf("cat %s | base64 -w 0", remote)
	if err := sendWithTemplate(cmd, conn); err != nil {
		return fmt.Errorf("failed to send download command: %w", err)
	}

	done := false
	r, w := io.Pipe()

	// Run io.Copy in a goroutine so it doesn't block the message reading loop
	go func() {
		for {
			_, copyErr := io.Copy(f, base64.NewDecoder(base64.StdEncoding, r))
			if copyErr == io.EOF {
				return
			} else if copyErr != nil {
				fmt.Printf("\ncopy error: %v\n%s", copyErr, ps1)
			}
		}
	}()

	defer w.Close()

	echo := true
	output := ""
	for {
		_, msg, err := conn.Read()
		if err != nil {
			fmt.Printf("\nread error: %v\n%s", err, ps1)
			return nil
		}

		text := escapes(extract(msg))
		lines := strings.Split(strings.Trim(text, "\r\n"), "\r\n")
		fl := lines[0]
		output += fl
		sl, el := 0, len(lines)
		if echo {
			sl++
			if strings.HasSuffix(output, cmd) {
				echo = false
			}
		}
		payload := ""
		if strings.HasSuffix(output, ps1) {
			text = strings.Trim(text[:len(text)-len(ps1)], "\r\n")
			if len(text) > 0 {
				payload = text
			}
			done = true
		} else if sl == 0 || len(strings.Trim(text, "\r\n")) > len(fl) {
			payload = strings.Join(lines[sl:el], "\r\n")
		}

		if _, err = w.Write([]byte(payload)); err != nil {
			fmt.Printf("\nwrite error: %v\n%s", err, ps1)
			fmt.Println(text)
		}

		if done {
			break
		}
		if len(output) > maxLineBufferLength {
			output = string(output[len(output)-maxLineBufferLength:])
		}
	}
	return nil
}

func process(prompt string, conn *session) error {
	if err := sendWithTemplate(prompt, conn); err != nil {
		return err
	}

	echo, output := true, ""
	for {
		_, msg, err := conn.Read()
		if err != nil {
			// Check if it's a timeout or normal closure
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				fmt.Printf("\nconnection closed\n%s", ps1)
				return err
			}
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				// Timeout - assume no more messages, continue normally
				return err
			}
			fmt.Printf("\nread error: %v\n%s", err, ps1)
			return err
		}
		// Extract data from downstream template
		text := strings.Trim(escapes(extract(msg)), "\r\n")
		lines := strings.Split(text, "\r\n")
		fl := lines[0]
		sl, el := 0, len(lines)
		output += fl
		if echo {
			sl++
			if strings.HasSuffix(output, prompt) {
				echo = false
			}
		}
		if strings.HasSuffix(text, ps1) {
			el--
			if sl < el {
				fmt.Print(strings.Join(lines[sl:el], "\r\n") + "\r\n" + ps1)
			} else {
				fmt.Printf("\r\n%s", ps1)
			}
			break
		} else if sl == 0 || len(strings.Trim(text, "\r\n")) > len(fl) {
			fmt.Print(strings.Join(lines[sl:el], "\r\n"))
		}
	}
	return nil
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

func sendWithTemplate(data string, conn *session) error {
	// simple replace
	msg := parse(uploadTmpl, map[string]any{"data": data + ternary(newline == "\r", "\\r", "\\n")})
	if len(msg) <= maxSize {
		return conn.Send(websocket.TextMessage, []byte(msg))
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
		if err := conn.Send(websocket.TextMessage, []byte(m)); err != nil {
			return err
		}
	}
	return nil
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

type session struct {
	conn *websocket.Conn
}

func (c session) Read() (t int, m []byte, e error) {
	defer func() {
		if debug {
			fmt.Println(t, string(m), e)
		}
	}()
	return c.conn.ReadMessage()
}

func (c session) Send(messageType int, data []byte) error {
	return c.conn.WriteMessage(messageType, data)
}
