# chatui

First vertical slice for a terminal-native chat client and relay in Go.

Current scope:

- WebSocket relay with in-memory mailbox delivery
- Bubble Tea shell app with a dedicated chat route model
- Invite-based contact exchange and verified device bundles
- Encrypted 1:1 message envelopes between mailbox IDs

## Project Shape

- `cmd/chatui`: thin client entrypoint
- `cmd/chatui-relay`: thin relay entrypoint
- `cmd/chatuictl`: local identity and contact management
- `internal/clientcmd`: client startup and flag wiring
- `internal/ctlcmd`: control command wiring
- `internal/relaycmd`: relay startup and flag wiring
- `internal/config`: shared runtime config types and validation
- `internal/identity`: account and device key material
- `internal/logging`: shared logger setup
- `internal/messaging`: client-side message preparation and decode logic
- `internal/protocol`: relay/client wire types
- `internal/session`: encrypted message envelope helpers
- `internal/store`: local identity and contact persistence
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

Initialize Alice and Bob locally and exchange invites:

```bash
go run ./cmd/chatuictl --help
go run ./cmd/chatuictl init --mailbox alice
go run ./cmd/chatuictl init --mailbox bob
go run ./cmd/chatuictl export-invite --mailbox alice --out /tmp/alice-invite.json
go run ./cmd/chatuictl export-invite --mailbox bob --out /tmp/bob-invite.json
go run ./cmd/chatuictl import-contact --mailbox alice --invite /tmp/bob-invite.json
go run ./cmd/chatuictl import-contact --mailbox bob --invite /tmp/alice-invite.json
```

Open one terminal for Alice:

```bash
go run ./cmd/chatui --mailbox alice --to bob
```

Open another terminal for Bob:

```bash
go run ./cmd/chatui --mailbox bob --to alice
```

If Bob is offline when Alice sends a message, the relay will keep that ciphertext in memory and deliver it when Bob subscribes.

## Current Limitations

- No durable relay persistence yet
- No trusted multi-device enrollment or revocation yet
- No encrypted local history yet
- Offline queue is in-memory only and disappears when the relay stops
