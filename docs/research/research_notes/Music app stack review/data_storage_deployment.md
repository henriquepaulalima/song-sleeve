# Database, storage and deployment fitness

Research date: 2026-10-07. Canonical current rules reviewed in `docs/implementation-plan.md`: online-only accepted mutations; stable tracks/fresh replacement content; manual artists/playlists/groups; optional cloud audio; installed local mirrors; one selected whole song on web; inclusive 30-minute/128 MiB admission; durable account deletion/device receipts. No app implementation or deployment was performed.

## Is PostgreSQL the appropriate catalog and coordination database?

### Takeaway
**Keep PostgreSQL.** The application's hard parts are relationship integrity and transactional coordination, rather than flexible metadata documents. SQLite is appropriate for installed-device read replicas; changing the server to a document or distributed embedded database would not remove the synchronization/deletion state machines.

### Cited Findings
- PostgreSQL provides multi-column foreign keys, unique constraints and partial unique indexes; cross-table restrictions should use foreign keys rather than a row CHECK pretending to validate other tables. — [PostgreSQL constraints](https://www.postgresql.org/docs/current/ddl-constraints.html)
- PostgreSQL `SKIP LOCKED` deliberately produces an inconsistent view suitable for multiple consumers of queue-like tables, rather than ordinary application reads. — [PostgreSQL SELECT](https://www.postgresql.org/docs/current/sql-select.html)
- SQLite permits many readers but only one writer at a time per database; its own guidance favors client/server databases for many concurrent writers or network-separated applications. — [SQLite appropriate uses](https://www.sqlite.org/whentouse.html)
- MongoDB supports multi-document transactions, but its documentation says distributed transactions cost more than single-document writes and should not replace effective schema design. — [MongoDB transactions](https://www.mongodb.com/docs/manual/core/transactions/)
- Turso describes an embedded SQLite-compatible database with cloud synchronization/offline application use cases. — [Turso introduction](https://docs.turso.tech/introduction)

### Inferences
- Model the “track document” as an API representation backed by ordinary typed columns and relationships, not a reason to add MongoDB. Preserve `tracks.id`; each explicit source submission inserts fresh content and changes its reference atomically. Keep cleanup facts for the deleted binary outside the removed content entity.
- Enforce `(user_id, artist_id)`, playlist-song and group-playlist ownership with composite references. Keep separate membership tables with unique membership keys, a shared owner and independent order; no recursive generic collection abstraction is needed.
- Add checks for positive finite duration, byte cap, allowed source/status values; use same-track ownership in the current-content reference as well as same-user ownership. Null artist remains valid.
- Commit accepted changes, request receipt and outbox together. Use short transactions/row revisions for metadata and durable operation ownership for transfers; never keep a transaction open for network I/O. A partial unique active-slot index or equivalent transactionally updated coordinator can enforce one active `(user, track)` operation.
- A PostgreSQL-backed worker queue is sufficient initially. `SKIP LOCKED` can claim ready jobs; leases, retries, expiration and capability draining still need service rules. No Redis/Kafka/distributed database is justified before measured bottlenecks.
- Turso/local synchronization is less compelling here because offline writes are prohibited. MongoDB would require transaction/reference validation for this relational model. SQLite on a single Railway volume could serve a tiny single-writer deployment, but narrows future replicated API/worker operation and adds migration pressure. No measured workload justifies those substitutions.
- Index account-scoped cursor pagination, memberships and ready-job claims; bound connection pools and worker concurrency. Resource forecasts need library sizes, active devices, transfer rate and query measurements; do not infer a monthly total from framework names.

### Gaps
- Final schema, index plans, contention tests and receipt/event retention remain implementation work. The stack does not solve stale grants, late writes or account-delete races automatically.

## What does Railway Buckets actually provide, and what must the application add?

### Takeaway
**Railway's private S3-compatible bucket fits the song datalake**, but publication must isolate writable staging from immutable accepted objects. Recovery and hard deletion need explicit application jobs and a backup policy.

### Cited Findings
- Railway lists PUT/GET/HEAD/DELETE, LIST, COPY, presigned URLs, tagging and multipart uploads. Configurable S3 server-side encryption, object versioning, object locks and lifecycle rules are unsupported. Its FAQ explicitly confirms **encryption at rest**, **no automatic bucket backups/snapshots**, and **public-network-only access**. The FAQ was verified in official page/raw source because the text extractor omitted accordion answers. Whole-bucket deletion has a 52-hour recovery window; that is not a documented per-object delete guarantee. — [Railway bucket documentation](https://docs.railway.com/storage-buckets), [official source](https://raw.githubusercontent.com/railwayapp/docs/main/content/docs/storage-buckets.md)
- Bucket cost is **$0.015/GB-month**, averaged across workspace/environment instances and rounded to whole GB-month; operations and bucket egress are free. Worker uploads to the bucket incur service egress. Hobby's combined capacity cap is 1 TB; Pro lists unlimited capacity. A hard spending limit can suspend reads/uploads while stored data remains billable. — [Railway bucket billing](https://docs.railway.com/storage-buckets/billing)
- Available bucket regions are `sjc` California, `iad` Virginia, `ams` Amsterdam and `sin` Singapore; region is fixed at creation. Signing region may be `auto`, which differs from deployment-region codes. — [Railway CLI bucket reference](https://docs.railway.com/cli/bucket)
- Railway documents direct presigned PUT/GET patterns and requires bucket CORS for browser uploads. Service-proxied audio adds service egress and memory responsibility. — [Railway bucket guide](https://docs.railway.com/guides/storage-buckets-guide), [upload/serving patterns](https://docs.railway.com/storage-buckets/uploading-serving)
- AWS's presigned-URL contract allows repeated use until expiry; PUT replaces an existing object at that key. This is a hazard to test on the S3-compatible provider, not a promise that a database lock revokes storage capabilities. — [AWS presigned URLs](https://docs.aws.amazon.com/AmazonS3/latest/userguide/using-presigned-url.html)

### Inferences
- Correct the plan's uncertain-at-rest statement: infrastructure encryption is documented, while configurable SSE API support is absent. Encryption at rest does not create application-owned keys or per-account cryptographic erasure.
- Never issue a client PUT grant for the final accepted key. Use a unique operation-scoped staging key; trusted workers validate complete bytes, actual duration and checksum, then publish to a server-only fresh final key. **Verifying staging then unconditional COPY is still a race** if a live grant overwrites staging between verification and copy. Safer baseline: spool the exact verified bounded file, upload those bytes to final, verify final, then atomically publish its reference. Alternatively conditional COPY plus final verification needs provider compatibility tests. COPY support alone does not establish safe promotion.
- Late writes after unsync/delete can recreate staging. Drain/expire capabilities, abort multipart uploads, record durable cleanup inventories and repeat scans before reporting cleanup complete. Missing lifecycle rules mean the worker owns incomplete multipart/orphan cleanup.
- Keep clients direct-to-bucket for ordinary transfers. Final worker upload can add egress, but avoids false immutable-content claims; measure it. Native playback stays in local `Songs`; web still downloads one accepted song before playback.
- Do not assume bucket durability is a recovery copy. Maintain independent inventory/checksum reconciliation and a second copy if users rely on cloud as their only recovery source. Delete app-addressable per-account backup copies too; shared provider snapshots/physical media need a separately documented retention boundary.

### Gaps
- Probe exact CORS management, PUT/GET/HEAD/preflight/exposed headers, presigned expiry, conditional COPY/GET, checksum enforcement, multipart resume/list/abort, deletion and late-write behavior. “S3-compatible” is insufficient evidence for every optional AWS header/condition.
- Official docs do not establish object-level physical-erasure timing, provider internal replication/snapshot retention, per-account cryptographic erasure, scoped credential policy details, or a numerical durability SLA. Ask provider before promising stronger deletion guarantees.

## How should PostgreSQL operations and deployment remain simple and recoverable?

### Takeaway
Use Vercel for the static TypeScript client and Railway for persistent Go API/worker plus PostgreSQL. Keep a small deployment, configure backup/restore deliberately, and add pooling/HA only where measured requirements justify their extra services.

### Cited Findings
- Railway database templates are explicitly **unmanaged**: the operator owns backup/disaster recovery, tuning, security and monitoring/maintenance. Its PostgreSQL template uses a Railway SSL-enabled image based on official Postgres and defaults to private connectivity. Optional HA uses Patroni/etcd/HAProxy; it is not the default single node. — [Railway databases](https://docs.railway.com/databases), [PostgreSQL](https://docs.railway.com/databases/postgresql)
- Railway volume backup schedules retain daily/weekly/monthly copies for **6/27/89 days**. Restore keeps the previous volume and its newer backups unmounted; backups restore only in the same project/environment. — [Railway volume backups](https://docs.railway.com/volumes/backups)
- Optional PostgreSQL PITR uses pgBackRest/WAL and weekly full plus daily differential backups, retaining four full backups (roughly four weeks). Restores create a new service; async WAL archival can lose its restore window during sustained storage outage. — [Railway PITR](https://docs.railway.com/volumes/point-in-time-recovery)
- Railway supports optional PgBouncer; the standard template has direct connections only. Transaction pooling conflicts with session-scoped LISTEN/advisory-lock features, so such work and migrations need dedicated/unpooled connections. — [Railway PgBouncer guide](https://docs.railway.com/guides/connection-pooling-pgbouncer)
- Vercel supports Vite; Vercel Functions have a **4.5 MB request/response payload limit**. — [Vite on Vercel](https://vercel.com/docs/frameworks/frontend/vite), [Function limits](https://vercel.com/docs/functions/limitations)
- Current Railway edge specs allow HTTP requests up to **15 minutes with ongoing data**, otherwise close after **5 minutes without data**; request-body uploads must finish in five minutes. WebSockets are exempt from these duration/inactivity limits. These table facts were verified from official page source because the extractor omitted the table. — [Railway networking specs](https://docs.railway.com/networking/public-networking/specs-and-limits)
- SameSite cookie controls are defense in depth, not a replacement for CSRF protection; credentialed CORS must be restricted to trusted origins. — [OWASP CSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html)

### Inferences
- Serve Vite assets/SPA routes on Vercel; use direct Railway API and bucket grants. Do not proxy 128 MiB audio or run long-lived workers through Vercel Functions. Secrets stay server-side; browser build variables are public.
- Start with one modular Go API, one worker, one PostgreSQL database and optional bucket. Bound local driver pools across replicas. Add PgBouncer when aggregate connection counts warrant it; keep SSE connections from reserving one database connection each. HA improves uptime but costs additional databases/consensus/router services; it is not a backup substitute. An external managed PostgreSQL provider is justified by an operational support/RPO/RTO requirement, not by changing relational semantics; otherwise current Railway placement is simpler.
- Use durable cursor events with heartbeat, intentional reconnect/jitter and current-manifest reconciliation; Railway's SSE duration limit means a forever-open stream is not promised. Each replica can fan out committed events without losing changes on restart; polling/event wake-ups must not scan every catalog for every stream. OS push delivery remains a hint.
- Use production `app.example.com` on Vercel and `api.example.com` on Railway: same-site HTTPS, still cross-origin. Exact CORS credentials/origin policy, CSRF and cookie tests remain necessary. Default `vercel.app`/`railway.app` pairing is cross-site; do not make sign-in rely on third-party cookies. Give previews a stable staging domain rather than permit every arbitrary preview origin.
- Configure automated snapshots/PITR and restoration drills, controlled one-run migrations/readiness, monitoring and object inventories. Shared snapshots retain deleted account data; the independent minimal deletion ledger must survive restores and be reapplied before opening traffic. Retire old restored volumes/backups deliberately. Define receipt retention beyond device absence and restore windows; storing the only deletion ledger inside the same restored snapshot can resurrect accepted deletions.

### Gaps
- RPO/RTO, backup retention/independent ledger location, provider-internal retention and deletion wording need decisions. No provider SLA or monthly workload total was assumed. Runtime tests remain necessary for SSE reconnect, bucket promotion and full restore followed by deletion replay.
