# Production deployment

Hubchat runs as one compiled binary with PostgreSQL as its only required
dependency. SMTP and S3-compatible storage are optional adapters. The commands
below are a release runbook, not a development shortcut.

## Release artifact

Build and inspect the artifact from a clean checkout:

```bash
make check
make build
make checksums
./dist/hubchat version
```

Copy only `dist/hubchat` and `dist/SHA256SUMS` to the release host. Verify the
checksum before starting it:

```bash
cd /srv/hubchat
sha256sum --check SHA256SUMS
```

The binary embeds the dashboard, portal, widget, and OpenAPI contract. The
runtime host does not need Node.js or the web source tree.

## Database rollout

Take and verify a backup before applying migrations:

```bash
pg_dump --format=custom --file=/var/backups/hubchat-$(date -u +%Y%m%dT%H%M%SZ).dump "$HUBCHAT_DATABASE_URL"
pg_restore --list /var/backups/hubchat-*.dump >/dev/null
HUBCHAT_DATABASE_URL="$HUBCHAT_DATABASE_URL" ./hubchat migrate
```

Run the service with migration verification enabled. A normal restart must not
silently change the schema:

```bash
HUBCHAT_MIGRATE=verify ./hubchat serve --roles=http,realtime,worker,scheduler
```

`migrate` is a separate release step. If it fails, keep the previous binary and
database version serving until the migration issue is resolved.

## Process roles

The default is one process:

```bash
./hubchat serve --roles=http,realtime,worker,scheduler
```

At higher measured load, split roles while keeping the same binary and release
artifact:

```bash
./hubchat serve --roles=http,realtime
./hubchat serve --roles=worker,scheduler
```

Do not run multiple schedulers unless the job lease and advisory-lock metrics
show that the deployment needs it. PostgreSQL remains the coordination layer.

## Service hardening

The service account should have access only to its data directory, config
secrets, and the Unix socket/TCP connection required for PostgreSQL. Terminate
TLS at a reverse proxy, forward the original host and scheme, and preserve
WebSocket upgrades for `/ws`.

A systemd deployment should use the equivalent of:

```ini
[Service]
User=hubchat
Group=hubchat
WorkingDirectory=/srv/hubchat
EnvironmentFile=/etc/hubchat/hubchat.env
ExecStart=/srv/hubchat/hubchat serve --roles=http,realtime,worker,scheduler
Restart=on-failure
RestartSec=5s
TimeoutStopSec=35s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/var/lib/hubchat
```

Set `HUBCHAT_DATA_DIR=/var/lib/hubchat/files` for the local attachment backend,
or configure the S3-compatible adapter and keep local disk for temporary state
only. Secrets belong in the environment file or a secret manager, never in the
unit file or repository.

## Optional DevLite observability

Set `DEVLITE_API_KEY` in the deployment secret store to enable DevLite. The
integration mirrors Hubchat's structured `slog` records, groups error-valued
records, automatically instruments HTTP requests, slow requests, and panics,
reports the running release, and emits 30-second Go runtime, PostgreSQL pool,
and job-queue gauges.

```bash
DEVLITE_API_KEY=dl_live_xxxxx
DEVLITE_ENVIRONMENT=production
DEVLITE_SERVICE_NAME=hubchat
DEVLITE_RELEASE=v1.2.3
DEVLITE_SAMPLE_RATE=1
DEVLITE_FLUSH_INTERVAL=5s
DEVLITE_METRICS_INTERVAL=30s
```

`DEVLITE_ENDPOINT` is optional and should be set only for a self-hosted DevLite
ingest API. Hubchat requires it to be an absolute HTTPS URL. Request bodies,
query strings, and headers are never sent; SDK scrubbing and source-context
error capture remain enabled. Telemetry uses a bounded queue, performs network
delivery only from its background flusher, times requests out after two
seconds, and cannot prevent the service from starting or shutting down.

## Readiness and rollout checks

After the process starts, require all of these before routing customer traffic:

```bash
HUBCHAT_SMOKE_BASE_URL="https://support.example.com" make production-http-check
HUBCHAT_LOAD_BASE_URL="https://support.example.com" \
HUBCHAT_LOAD_DURATION_MS=10000 HUBCHAT_LOAD_CONCURRENCY=32 \
  make production-load-check
```

For a self-contained staging gate that builds and starts the exact artifact:

```bash
HUBCHAT_BINARY_DATABASE_URL="$HUBCHAT_DATABASE_URL" make production-binary-check
```

The target reserves a local port, starts HTTP-only with `HUBCHAT_DEV=0`, runs
the smoke and load checks, then requires a clean shutdown. Use it against an
isolated staging database, never the live workspace database.

Before capacity sign-off, run the data-heavy PostgreSQL gate against a
dedicated test or staging database:

```bash
HUBCHAT_TEST_DATABASE_URL="postgres://.../hubchat_capacity?sslmode=require" \
  make test-capacity
```

The gate seeds 25,000 conversations and messages, exercises concurrent indexed
inbox reads, checks workspace isolation, and fails when p95 exceeds the
configured limit. Use staging-sized values for `HUBCHAT_SCALE_CONVERSATIONS`,
`HUBCHAT_SCALE_WORKERS`, and `HUBCHAT_SCALE_REQUESTS`; never point it at a
database containing live customer data because it resets the database.

For optional provider verification in staging, run the provider gate with
provider-specific environment variables:

```bash
HUBCHAT_PROVIDER_S3_ENDPOINT="https://s3.example.com" \
HUBCHAT_PROVIDER_S3_BUCKET="hubchat-staging" \
HUBCHAT_PROVIDER_S3_ACCESS_KEY="..." \
HUBCHAT_PROVIDER_S3_SECRET_KEY="..." \
HUBCHAT_PROVIDER_SMTP_HOST="smtp.example.com" \
HUBCHAT_PROVIDER_SMTP_PORT=587 \
HUBCHAT_PROVIDER_SMTP_FROM="support@example.com" \
HUBCHAT_PROVIDER_SMTP_ENCRYPTION=starttls \
HUBCHAT_RUN_PROVIDER_TESTS=1 \
  go test ./internal/file ./internal/mailer -tags=provider -count=1 -v
```

Use a dedicated staging bucket/prefix and a controlled recipient. The test
deletes its object after verification and does not inspect or modify customer
database records.

`/healthz` proves the process is alive. `/readyz` proves configured database
readiness. The smoke check verifies all embedded browser surfaces and the API;
the bounded load check verifies that static surfaces stay available while the
API rate limiter protects the service.

During rollout, monitor request latency and errors, PostgreSQL pool saturation,
job depth/dead letters, WebSocket connections, webhook/email failures, and
storage errors. Stop routing traffic if readiness fails or static assets no
longer match the embedded release.

## Backup and restore verification

Database backups are not sufficient when attachments use local disk or object
storage. Back up the database and the attachment object store together, retain
the export manifest/checksums, and perform a restore rehearsal before calling a
release recoverable:

1. Restore the dump into an isolated PostgreSQL database.
2. Restore the attachment objects into an isolated storage prefix.
3. Verify each copied workspace archive before import:
   `./dist/hubchat workspace verify --file workspace.json.gz --json`.
4. Start the exact release binary with `HUBCHAT_MIGRATE=verify`.
5. Run the core support/portal journey and download an attachment.
6. Compare the restored export manifest and checksums.
7. Record the restore timestamp and discard the isolated environment only
   after the checks pass.

Never test restore procedures against the live workspace database. Portability
imports remain separately guarded by preview, backup verification, and explicit
confirmation.
