# tcsh - WebSocket remote shell

tcsh is a small CLI that connects to a WebSocket endpoint and forwards local stdin to the remote while printing remote output to stdout.

Key points (as implemented in `main.go`):

- Upstream and downstream behavior is template-driven. By default upstream messages are JSON objects with an `operation` and a `data` field. Downstream data is extracted using a small downstream template.
- There is a built-in `/get` command to download a remote file. The download flow requests the remote to emit base64 and decodes it locally.
- Regular lines from stdin (not starting with `/`) are sent using the upstream template. Long payloads are automatically chunked to respect the `-max` size.

Commands
--------
- `/get <localpath> <remotepath>` — request the remote to send the contents of `<remotepath>` encoded as base64; the client decodes and writes to `<localpath>`.
- `/quit` or `/exit` — close the connection and exit.

Templates
---------
By default the tool uses these templates (see `-up` and `-data` flags):

- Upstream (default):

  {"operation":"stdin","data":"{{.data}}"}

- Downstream data template (default):

  {{.data}}

The upstream template is used to wrap any data you send (stdin lines or messages sent by the program). The template system is Go text/template — the library will be given a single variable named `data` whose value is the text payload to send.

Chunking behavior
-----------------
If the rendered upstream message exceeds the `-max` byte size it will be split. The code determines the template overhead and splits the payload so each rendered message is within the limit.

Download flow (what `/get` does)
--------------------------------
1. The client sends a download command using the upstream template. The current implementation sends:

   cat <remotepath> | base64 -w 0

   inside the upstream template's `data` field.
2. The program reads downstream messages and extracts the `data` field using the downstream template.
3. Extracted chunks are written to a pipe and streamed through a base64 decoder into the target file.

Only the `/get` (download) flow uses base64 encoding on the remote side. Regular stdin and other commands are sent as plaintext inside the upstream template.

Examples
--------
Connect and use defaults:

  tcsh -url ws://example.com/ws

Request a download from the remote and save to `./out.bin`:

  /get ./out.bin /remote/path/out.bin

Send an arbitrary command to the remote (commands beginning with `/` are forwarded as data):

  /echo hello

Send raw input (not a slash command). The line will be wrapped by the upstream template and sent:

  echo "hello" | tcsh -url ws://example.com/ws

Flags
-----
- `-url string`     WebSocket endpoint (ws:// or wss://) (required)
- `-up string`      Upstream template (default: `{"operation":"stdin","data":"{{.data}}"}`)
- `-data string`    Downstream data template (default: `{{.data}}`)
- `-resize string`  Resize window template (default: `{"operation":"resize","rows":{{.rows}},"cols":{{.cols}}}`)
- `-newline string` Newline sequence inserted into `data` when sending (default: `\r`)
- `-max int`        Maximum payload size per message (bytes) (default: 4159)
- `-debug bool`     Enable debug output

Build
-----
You can build with Go (Go 1.18+):

```bash
go build ./...
```

Notes
-----
- The README documents the behavior of the current `main.go`. If you need an `/upload` command or different remote-side helpers, we can add symmetric upload support that encodes local files to base64, splits chunks into `data` fields and sends a final decode command to the remote (the codebase already contains the basic chunking helpers to make that straightforward).
