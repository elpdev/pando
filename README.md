# pando

First vertical slice for a terminal-native chat client and relay in Go.

Current scope:

- WebSocket relay with durable mailbox delivery
- Bubble Tea shell app with a dedicated chat route model
- Invite-based contact exchange and verified device bundles
- Encrypted 1:1 message envelopes between mailbox IDs
- Encrypted local chat history per peer conversation
- Relay size, TTL, and rate-limit controls
- Client reconnect with backoff after relay disconnects
- Contact fingerprint display and explicit verification flow
- Optional relay auth tokens for private deployments
- Trusted device enrollment, listing, and revocation
- Automatic contact device-bundle refresh during normal messaging
- Duplicate suppression for replayed relay envelopes
- Delivery acknowledgements reflected in local chat history
- Shareable invite codes for simpler secure onboarding

## Project Shape

- `cmd/pando`: thin client entrypoint
- `cmd/pando-relay`: thin relay entrypoint
- `cmd/pandoctl`: local identity and contact management
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
go run ./cmd/pando-relay
```

## ONCE Packaging

The relay is packaged to fit ONCE's expected runtime shape.

Container behavior:

- serves HTTP on port `80`
- exposes a healthcheck endpoint at `/up`
- stores durable relay state in `/storage/relay.db`

Build the image:

```bash
docker build -t pando-relay .
```

The Docker build uses the repo's vendored Go dependencies, so it does not need to fetch modules during image build.

On GitHub, every push to `main` also publishes the relay image automatically to GHCR with these tags:

- `ghcr.io/elpdev/pando-relay:latest`
- `ghcr.io/elpdev/pando-relay:main`
- `ghcr.io/elpdev/pando-relay:vX.Y.Z`
- `ghcr.io/elpdev/pando-relay:sha-<commit>`

Every push to `main` also creates:

- a semver git tag such as `v0.1.0`, `v0.2.0`, `v1.0.0`
- a matching GitHub Release
- attached release binaries for Linux, macOS, and Windows

GitHub Release notes are generated automatically from the commits since the previous version tag.

Version bumps follow conventional commits since the previous tag:

- `feat:` bumps the minor version
- `fix:`, `docs:`, `chore:`, `refactor:`, `test:` and other non-breaking commits bump the patch version
- `!` in the type line or `BREAKING CHANGE:` in the body bumps the major version

Doc-only and repository-metadata-only pushes are skipped and do not create a release.

The relay image is published as a multi-arch container for:

- `linux/amd64`
- `linux/arm64`

Run it locally like ONCE would:

```bash
docker run --rm -p 8080:80 -v "$PWD/storage:/storage" pando-relay
```

Test the healthcheck:

```bash
curl http://localhost:8080/up
```

The relay WebSocket endpoint will then be:

```text
ws://localhost:8080/ws
```

If you deploy it at `relay.lbp.dev`, your clients should use:

```text
wss://relay.lbp.dev/ws
```

If your ONCE deployment supports custom command arguments, you can still override defaults such as auth token or queue limits, for example:

```text
--auth-token <shared-token> --ttl 24h --max-message-bytes 65536 --rate-limit-per-minute 120
```

Optional hardening flags:

```bash
go run ./cmd/pando-relay --ttl 24h --max-message-bytes 65536 --rate-limit-per-minute 120 --auth-token secret-token
```

Initialize Alice and Bob locally and exchange invite codes:

```bash
go run ./cmd/pandoctl --help
go run ./cmd/pandoctl init --mailbox alice
go run ./cmd/pandoctl init --mailbox bob
go run ./cmd/pandoctl invite-code --mailbox alice --copy
go run ./cmd/pandoctl invite-code --mailbox bob --copy
go run ./cmd/pandoctl add-contact --mailbox alice --code '<bob-invite-code>'
go run ./cmd/pandoctl add-contact --mailbox bob --code '<alice-invite-code>'
go run ./cmd/pandoctl list-contacts --mailbox alice
go run ./cmd/pandoctl verify-contact --mailbox alice --contact bob --fingerprint <bob-fingerprint>
```

If you still want file-based exchange, `export-invite` and `import-contact` still work.

## Local Test

Use this when you want to test everything on one machine.

1. Start the relay:

```bash
go run ./cmd/pando-relay
```

2. Initialize identities:

```bash
go run ./cmd/pandoctl init --mailbox alice
go run ./cmd/pandoctl init --mailbox bob
```

3. Exchange invite codes:

Alice:

```bash
go run ./cmd/pandoctl invite-code --mailbox alice --copy
```

Bob:

```bash
go run ./cmd/pandoctl invite-code --mailbox bob --copy
```

Then paste each code into the other side:

```bash
go run ./cmd/pandoctl add-contact --mailbox alice --code '<bob-invite-code>'
go run ./cmd/pandoctl add-contact --mailbox bob --code '<alice-invite-code>'
```

4. Verify fingerprints out of band:

```bash
go run ./cmd/pandoctl show-contact --mailbox alice --contact bob
go run ./cmd/pandoctl show-contact --mailbox bob --contact alice
go run ./cmd/pandoctl verify-contact --mailbox alice --contact bob --fingerprint <bob-fingerprint>
go run ./cmd/pandoctl verify-contact --mailbox bob --contact alice --fingerprint <alice-fingerprint>
```

5. Start both chat clients:

```bash
go run ./cmd/pando --mailbox alice --to bob
go run ./cmd/pando --mailbox bob --to alice
```

6. Test the basics:

- send Alice -> Bob
- send Bob -> Alice
- close Bob, send from Alice, reopen Bob, confirm offline delivery
- stop and restart the relay, then confirm queued delivery still works

## Remote Test

Use this when testing with a friend on another network.

Yes: the relay must be reachable by both people. If you host it at `relay.lbp.dev`, both clients need to connect to that public relay URL.

1. Run the relay on a public host:

```bash
go run ./cmd/pando-relay --addr ":8080"
```

2. Put it behind a public endpoint such as:

```text
ws://relay.lbp.dev/ws
```

If you terminate TLS, use:

```text
wss://relay.lbp.dev/ws
```

3. Give both people the same relay URL. If you want a private relay, also set a shared token:

```bash
go run ./cmd/pando-relay --auth-token '<shared-token>'
```

4. Each person initializes locally on their own machine:

```bash
go run ./cmd/pandoctl init --mailbox alice
go run ./cmd/pandoctl init --mailbox bob
```

5. Exchange invite codes over a channel you already trust enough to compare fingerprints afterward.

6. Import and verify:

```bash
go run ./cmd/pandoctl add-contact --mailbox alice --code '<bob-invite-code>'
go run ./cmd/pandoctl add-contact --mailbox bob --code '<alice-invite-code>'
go run ./cmd/pandoctl verify-contact --mailbox alice --contact bob --fingerprint <bob-fingerprint>
go run ./cmd/pandoctl verify-contact --mailbox bob --contact alice --fingerprint <alice-fingerprint>
```

7. Start the clients against the public relay:

```bash
go run ./cmd/pando --mailbox alice --to bob --relay wss://relay.lbp.dev/ws
go run ./cmd/pando --mailbox bob --to alice --relay wss://relay.lbp.dev/ws
```

With relay auth enabled:

```bash
go run ./cmd/pando --mailbox alice --to bob --relay wss://relay.lbp.dev/ws --relay-token '<shared-token>'
go run ./cmd/pando --mailbox bob --to alice --relay wss://relay.lbp.dev/ws --relay-token '<shared-token>'
```

## Notes

- The relay only sees mailbox identifiers and ciphertext for encrypted chat messages.
- Fingerprint verification is what protects against importing the wrong contact bundle.
- For internet-facing use, prefer `wss://` instead of `ws://`.
- If you run the relay on `relay.lbp.dev`, make sure your reverse proxy forwards WebSocket upgrades to `/ws`.

Enroll a second trusted device for Alice:

```bash
go run ./cmd/pandoctl create-enrollment --account alice --mailbox alice-phone --out /tmp/alice-phone-request.json
go run ./cmd/pandoctl approve-enrollment --mailbox alice --request /tmp/alice-phone-request.json --out /tmp/alice-phone-approval.json
go run ./cmd/pandoctl complete-enrollment --mailbox alice-phone --approval /tmp/alice-phone-approval.json
go run ./cmd/pandoctl list-devices --mailbox alice
```

Revoke a trusted device:

```bash
go run ./cmd/pandoctl revoke-device --mailbox alice --device alice-phone
```

Open one terminal for Alice:

```bash
go run ./cmd/pando --mailbox alice --to bob
go run ./cmd/pando --mailbox alice --to bob --relay-token secret-token
```

Open another terminal for Bob:

```bash
go run ./cmd/pando --mailbox bob --to alice
```

If Bob is offline when Alice sends a message, the relay will keep that ciphertext in its local queue store and deliver it when Bob subscribes.

## Current Limitations

- No automatic history sync to newly enrolled devices
- Delivery state is message-level only; no per-device breakdown or read receipts yet
