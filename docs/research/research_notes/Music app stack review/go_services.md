# Go API, workers, and synchronization services

## Does the Go HTTP framework need to change?

### Takeaway
Keep Gin. Its routing, middleware, request validation and event responses fit the API. The missing work is application transaction, transfer and device protocols; Chi is an optional standard-handler preference, rather than a necessary fix.

### Cited Findings
- Gin provides middleware, route groups, JSON binding/validation and response rendering; its official example implements authenticated SSE using `Stream` and `SSEvent`. The example is not a durable delivery or production authorization design. — [Gin](https://github.com/gin-gonic/gin); [official SSE example](https://github.com/gin-gonic/examples/blob/master/server-sent-event/main.go)
- Chi explicitly uses standard `net/http` handlers/middleware; Go's router supports methods and wildcard paths. Echo also provides an SSE example. — [Chi](https://github.com/go-chi/chi); [Go routing](https://go.dev/doc/go1.22); [Echo SSE](https://echo.labstack.com/next/cookbook/sse/)
- `oapi-codegen` supports Gin server generation and strict request/response interfaces, with separate OpenAPI request-validation middleware. — [generator](https://github.com/oapi-codegen/oapi-codegen); [Gin middleware](https://github.com/oapi-codegen/gin-middleware)

### Inferences
- Proposed minimum: Gin handlers call framework-independent account/catalog/media/sync services; OpenAPI and `oapi-codegen` reduce contract duplication. Ownership, revision checks and deletion gates remain domain/database rules. Keep JSON request limits small; audio bypasses the API. — [Gin](https://github.com/gin-gonic/gin); [current contract](../../../implementation-plan.md)

### Gaps
- No benchmark establishes a relevant Gin/Chi/Echo resource advantage for this app. Measure database queries, connected devices and worker resources before replacing the router.

## Which database access and worker tools simplify correctness?

### Takeaway
Propose `pgx`/`pgxpool`, `sqlc`, Goose SQL migrations and River's PostgreSQL-backed Go queue. This makes the generic “Go workers” choice concrete without adding Redis immediately.

### Cited Findings
- Pgx provides transactions, pooling and PostgreSQL notifications. Sqlc generates typed Go query interfaces and `WithTx` transaction-bound queries. Goose manages incremental SQL/Go migrations. — [pgx](https://pkg.go.dev/github.com/jackc/pgx/v5); [sqlc](https://docs.sqlc.dev/en/latest/); [transactions](https://docs.sqlc.dev/en/latest/howto/transactions.html); [Goose](https://pressly.github.io/goose/)
- River allows jobs to be enqueued in the same PostgreSQL transaction as catalog changes, and supports transactional database effects/job completion. Asynq is instead Redis-backed with retries/crash recovery. — [River enqueue](https://riverqueue.com/docs/transactional-enqueueing); [River completion](https://riverqueue.com/docs/transactional-job-completion); [Asynq](https://github.com/hibiken/asynq)

### Inferences
- SQL-first access exposes tenant foreign keys, revision predicates, leases and atomic replacement directly. An ORM can implement them but does not remove them; adopting GORM for basic CRUD offers little simplification here. Commit accepted catalog changes, request receipts, change-log rows and River jobs together. Keep a domain outbox/change log for device replay alongside the job queue. — [sqlc transactions](https://docs.sqlc.dev/en/latest/howto/transactions.html); [River enqueue](https://riverqueue.com/docs/transactional-enqueueing)
- Keep pools bounded and transactions short; transfers must not occupy a database transaction/pool connection throughout. Use one controlled application/River migration step rather than every replica migrating independently. — [PostgreSQL locking](https://www.postgresql.org/docs/current/explicit-locking.html); [Goose](https://pressly.github.io/goose/)

### Gaps
- Worker concurrency, retention and resource budgets require measured workloads. A queue choice does not establish a throughput guarantee.

## Does a queue enforce one operation per user and track?

### Takeaway
Not by itself. Preserve the accepted single active operation across upload, replacement, download and unsync through one database coordinator shared by the API and all workers.

### Cited Findings
- River uniqueness always includes job kind and can include arguments/state. Unique jobs still execute at least once. Global/partitioned concurrency limits are a River Pro feature. — [uniqueness](https://riverqueue.com/docs/unique-jobs); [Pro concurrency](https://riverqueue.com/docs/pro/concurrency-limits)
- PostgreSQL row locks serialize conflicting writers until transaction end; advisory locks require consistent application participation. Long-running transactions are discouraged. — [locking](https://www.postgresql.org/docs/current/explicit-locking.html)

### Inferences
- Proposed coordinator: a short transaction acquires an operation-slot row keyed by `(user_id, track_id)`, recording owner, operation ID, lease deadline, source revision and fencing generation. Distinct requests wait separately; only the current owner renews/completes. Reject stale completions even when their transfer succeeds. Never keep a database lock open for a 128 MiB transfer. — [locking](https://www.postgresql.org/docs/current/explicit-locking.html)
- Idempotency identifies `(account, request_id)` with payload validation. Repeating that request returns its receipt; a newly confirmed source creates fresh content even for equal bytes. Do not use checksum equality or track-only job uniqueness to collapse intentional replacements. Queue retries repeat one operation, not a new submission. — [River uniqueness](https://riverqueue.com/docs/unique-jobs); [accepted identity](../../../implementation-plan.md#track-identity-throughout-its-lifecycle)
- Serializing same-song downloads across restoring devices creates head-of-line waiting; different tracks still run concurrently. Measure this consequence while preserving the accepted rule. — [serialization requirement](../../../implementation-plan.md#serialized-sync-and-device-notifications)

### Gaps
- Lease/grant timing, queued revision policy and recovery remain checklist item 5. No framework selects these semantics automatically.

## How should events reach connected and sleeping devices?

### Takeaway
Start with authenticated Railway SSE, durable PostgreSQL replay cursors and push wake-up hints. PostgreSQL notifications wake dispatchers; in-memory channels are not history.

### Cited Findings
- PostgreSQL `NOTIFY` reaches listening sessions after commit, with a normally sub-8,000-byte payload; tables can hold referenced durable records. Redis Pub/Sub instead documents at-most-once delivery: disconnected subscribers lose messages. — [NOTIFY](https://www.postgresql.org/docs/current/sql-notify.html); [Redis delivery](https://redis.io/docs/latest/develop/pubsub/)
- Firebase Admin SDK supports Go sending. FCM routes Apple messages through APNs and describes silent handlers; background notification delivery is not guaranteed. — [Go sender](https://firebase.google.com/docs/cloud-messaging/send/admin-sdk); [Apple delivery](https://firebase.google.com/docs/cloud-messaging/ios/receive-messages)
- Railway public HTTP requests last at most 15 minutes with continuing traffic, or close after five minutes without transferred data. WebSockets are exempt from those limits. — [Railway network limits](https://docs.railway.com/networking/public-networking/specs-and-limits)

### Inferences
- One dedicated database listener per API replica wakes an account-filtered fanout dispatcher: avoid one PostgreSQL connection per device/SSE stream. Bounded buffers, heartbeats and replay repair dropped/coalesced hints. Expired replay history needs an authorized full snapshot. SSE suits server-to-device changes while writes use HTTP. — [NOTIFY](https://www.postgresql.org/docs/current/sql-notify.html); [Gin SSE](https://github.com/gin-gonic/examples/blob/master/server-sent-event/main.go)
- SSE heartbeats prevent inactivity closure, but do not defeat Railway's 15-minute ceiling. Require authenticated reconnect with jitter/backoff and durable cursor replay. WebSockets are an alternative if frequent reconnect overhead proves significant; revocation, reconnect and replay still need explicit application handling. — [Railway network limits](https://docs.railway.com/networking/public-networking/specs-and-limits)
- Firebase Admin Go can be a proposed single sender abstraction for Android and iOS via APNs; a direct APNs adapter is optional. Neither keeps a suspended app running. Push carries a change hint and devices fetch accepted revisions. Playback pins are client state, not a reason to hold the server slot throughout playback. — [FCM Apple behavior](https://firebase.google.com/docs/cloud-messaging/ios/receive-messages); [accepted playback](../../../implementation-plan.md#apply-changes-after-current-playback-finishes)

### Gaps
- Open-stream expiry/revocation, proxy timeout and connection scaling need testing. Redis may later improve fanout, but cannot replace cursor history.

## Are authentication and irreversible deletion supported?

### Takeaway
Go has appropriate cryptographic primitives and session libraries; Gin is not a complete identity system. Account/session tables and deletion state-machine logic remain necessary.

### Cited Findings
- OWASP describes server-side opaque sessions, token verifiers, protected cookies and server-enforced expiry. Go provides cryptographic randomness and Argon2id. SCS supports absolute lifetime, optional idle timeout, hashed-token storage and persistent stores. — [OWASP sessions](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html); [randomness](https://pkg.go.dev/crypto/rand); [Argon2id](https://pkg.go.dev/golang.org/x/crypto/argon2); [SCS](https://pkg.go.dev/github.com/alexedwards/scs/v2)
- Recovery requires random expiring single-use tokens and session invalidation. River defaults to a maximum of 25 attempts before failed work is discarded. — [OWASP recovery](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html); [River retries](https://riverqueue.com/docs/job-retries)

### Inferences
- Keep 30-day absolute sessions in account/device-indexed PostgreSQL rows; web cookies/native bearer credentials share a revocation gate. SCS is an optional adapter candidate: inspect custom storage, native exchange and bulk revocation. Self-contained 30-day JWTs do not replace immediate revocation. Benchmark/rate-limit password hashing separately from media processing. — [SCS](https://pkg.go.dev/github.com/alexedwards/scs/v2); [OWASP sessions](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- Email verification/reset uses a provider adapter and transactional jobs; encrypt transient raw link material in the application rather than assuming free River encrypts it. Keep deletion ledger/cleanup receipts independent of user cascades. A checkpointed worker plus recovery sweep/alerts must re-enqueue incomplete deletion after queue retry cutoff; only verified completion clears it. — [River retries](https://riverqueue.com/docs/job-retries); [account lifecycle](../../../implementation-plan.md#irreversible-account-deletion-and-device-cleanup)

### Gaps
- Email provider/auth-store adapter remain unselected. No library guarantees instantaneous offline-device purge or erasure from historical backups.

## How should Go verify and publish cloud audio?

### Takeaway
Use AWS SDK for Go v2 behind the object-store adapter and bounded ffprobe workers. The outstanding concern is immutable publication, not S3 access from Go.

### Cited Findings
- SDK v2 supports custom S3 `BaseEndpoint` and presigned operations. Ffprobe inspects containers/streams and produces machine-readable format/stream information. — [endpoints](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-endpoints.html); [SDK examples](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/go_s3_code_examples.html); [ffprobe](https://ffmpeg.org/ffprobe.html)
- Amazon S3 presigned URLs are reusable until expiry; PUT replaces an existing object at the key. A started download can continue after expiry. These Amazon facts do not prove every Railway condition/policy capability. — [presigned semantics](https://docs.aws.amazon.com/AmazonS3/latest/userguide/using-presigned-url.html)

### Inferences
- Spool staging bytes to bounded temporary files, count bytes, calculate integrity hashes and inspect positive finite audio duration before enforcing 128 MiB AND 1,800 seconds. Avoid whole-file Go heap buffers/in-process multimedia bindings initially; bound subprocess time/resources and clean temporary files. Local-only unsynced bytes cannot be independently probed by the server. — [ffprobe](https://ffmpeg.org/ffprobe.html); [accepted limits](../../../implementation-plan.md#approved-song-limits)
- Proposed guard: upload grants target unique staging-attempt keys only. Publish to a separate final key never PUT-granted to clients, from the exact verified temporary file or conditional copy plus final-byte verification. Otherwise late reusable PUTs can overwrite a verified generation, including between probing and copying. Check operation/account fences before publication and clean late writes on removal. — [overwrite semantics](https://docs.aws.amazon.com/AmazonS3/latest/userguide/using-presigned-url.html)

### Gaps
- Railway condition/checksum/copy semantics need storage-spike verification. PostgreSQL cannot atomically commit an object-store deletion: resumable cleanup inventories and reconciliation remain necessary.
