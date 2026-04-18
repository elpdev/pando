# chatui

First vertical slice for a terminal-native chat client and relay in Go.

Current scope:

- WebSocket relay with in-memory mailbox delivery
- Bubble Tea shell app with a dedicated chat route model
- Plaintext 1:1 message flow between mailbox IDs

## Project Shape

- `cmd/chatui`: thin client entrypoint
- `cmd/chatui-relay`: thin relay entrypoint
- `internal/clientcmd`: client startup and flag wiring
- `internal/relaycmd`: relay startup and flag wiring
- `internal/config`: shared runtime config types and validation
- `internal/logging`: shared logger setup
- `internal/protocol`: relay/client wire types
- `internal/transport`: transport interface boundary
- `internal/transport/ws`: WebSocket transport implementation
- `internal/ui`: shell-level Bubble Tea app
- `internal/ui/chat`: chat route model
- `internal/relay`: relay server and mailbox behavior

## Run

Start the relay:

```bash
go run ./cmd/chatui-relay
```

Open one terminal for Alice:

```bash
go run ./cmd/chatui --mailbox alice --to bob
```

Open another terminal for Bob:

```bash
go run ./cmd/chatui --mailbox bob --to alice
```

If Bob is offline when Alice sends a message, the relay will keep that message in memory and deliver it when Bob subscribes.

## Current Limitations

- Plaintext transport only
- No identity, contacts, encryption, persistence, or device trust yet
- Offline queue is in-memory only and disappears when the relay stops
