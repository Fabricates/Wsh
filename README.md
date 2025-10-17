wsh - WebSocket remote shell

This tool connects to a WebSocket endpoint and forwards local stdin to the remote and prints remote messages to stdout.

Features:
- Configurable upstream (`-up`) and downstream (`-down`) templates. Use a literal `$` in the template to indicate where the data payload should be substituted.
- Optional base64 encoding for outbound (`-up-b64`) and decoding for inbound (`-down-b64`) payloads.
- Upload and download helpers using the simple commands:
  - /upload <localpath> <remotepath>
  - /download <localpath> <remotepath>

Examples:

Connect to a websocket and send raw stdin as JSON field `data`:

  wsh -url ws://example.com/ws -up '{"op":"stdin","data":"$"}' -up-b64

Upload a file to the remote (tool will base64 the file contents and send an `upload` op):

  /upload ./local.bin /tmp/remote.bin

Request a download from the remote (remote should reply with `{"op":"download","path":"<path>","data":"<base64>"}`):

  /download ./local.out /remote/path/out.bin

Flags:
  -url string   WebSocket endpoint (ws:// or wss://)
  -up string    Upstream template, literal '$' is replaced by data (default "$")
  -down string  Downstream template, literal '$' is replaced by data (default "$")
  -max int      Maximum payload size per message (bytes) (default 65536)
  -up-b64       Base64-encode outgoing data when inserting into upstream template
  -down-b64     Base64-decode incoming data when extracting from downstream messages
