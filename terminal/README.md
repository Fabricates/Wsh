# Terminal WebSocket Server

A simple WebSocket server for testing `wsh`. Each WebSocket connection spawns a bash session, forwarding input/output between the WebSocket client and the bash process.

## Features

- Each WebSocket connection gets its own isolated bash session
- Bidirectional communication: WebSocket messages → bash stdin, bash stdout/stderr → WebSocket messages
- Uses pseudo-terminal (PTY) for proper bash interaction
- Compatible with `wsh` client message format

## Usage

First, ensure dependencies are installed:

```bash
go mod download
```

Start the server:

```bash
go run terminal/server.go
```

Or specify a custom address:

```bash
go run terminal/server.go -addr :9000
```

Connect with `wsh`:

```bash
go run main.go -url ws://localhost:8080/ws
```

## Message Format

The server expects and sends JSON messages with the following format:

```json
{
  "operation": "stdin",
  "data": "ls -la\r"
}
```

- `operation`: Can be "stdin" or empty for sending commands to bash
- `data`: The actual data (command text or output)

## Example Session

1. Start the server:
   ```bash
   go run terminal/server.go
   ```

2. In another terminal, connect with wsh:
   ```bash
   go run main.go -url ws://localhost:8080/ws
   ```

3. Type commands and see the output:
   ```
   [connected]
   /app # ls
   LICENSE  Makefile  README.md  go.mod  main.go  terminal
   /app # pwd
   /data/src/oss/Wsh
   /app #
   ```

## Implementation Details

- Uses `github.com/creack/pty` for pseudo-terminal support
- Uses `github.com/gorilla/websocket` for WebSocket handling
- Each connection runs in isolated goroutines for reading and writing
- Custom PS1 prompt set to `/app # ` for easy EOF detection
