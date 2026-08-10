# Release process

HubChat releases are built from a clean checkout and shipped as one binary.

## Required checks

```bash
make check
make build
make checksums
./dist/hubchat version
```

Run integration tests against the dedicated test database when PostgreSQL is
available. Never point destructive integration or capacity checks at a live
workspace database.

## Database safety

Before applying migrations:

1. take a PostgreSQL backup;
2. verify the backup can be listed/restored;
3. apply migrations as a separate release step;
4. start the new binary with migration verification enabled;
5. run health, readiness, embedded-surface, API, and bounded-load checks.

If migration or readiness fails, keep the previous binary and database version
serving while the issue is investigated.

## Release notes

Every release should state user-visible changes, migration requirements,
security fixes, compatibility notes, and any known limitations. Do not describe
generated assets or a successful build as proof that the live customer journey
was verified; run the production journey checks separately.
