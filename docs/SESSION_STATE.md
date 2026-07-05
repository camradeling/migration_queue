# Session State — resume point

Written after finishing the initial Go backend scaffold, so work can pick up cleanly.

## Where things stand

- **Plan**: finalized — see `docs/PLAN.md`. No design blockers remain.
- **Prerequisites**: Go 1.26.4 and Docker are installed and working. Note: `docker`
  group membership for the `claudev` user needed a manual fix this session (the
  earlier `usermod -aG docker` from `scripts/install_prereqs.sh` hadn't taken); once
  `/etc/group` had `claudev` in the `docker` line, `sg docker -c "docker ..."` picked
  it up immediately without a further restart. Plain `docker ps` (no `sg` wrapper)
  may still fail in a shell whose login session predates that fix.
- **Backend scaffold: done and verified end-to-end.** `backend/` now has:
  - `go.mod` (module `github.com/camradeling/migration_queue/backend`), all
    dependencies from `docs/PLAN.md`'s library list fetched and tidy.
  - `cmd/server` — wires config → DB connect → migrate → admin seed → SMS outbox
    worker → Gin router → listen.
  - `cmd/qrgen` — CLI wrapper around `internal/qr` (same code path the
    `/api/admin/qrcode` endpoint uses).
  - `internal/{config,db,queue,sms,api,qr}` — config loading, sqlx/Postgres
    connection + golang-migrate runner + admin seeding, the register/next/
    start/stop queue logic (row-locked transactions, throttled-notification
    rule, FIFO renumbering), the `SMSSender` interface + console adapter +
    outbox-draining worker, Gin routes/handlers (JWT auth, per-IP login rate
    limit, WebSocket stats stream authenticated via `?token=`), and QR PNG
    generation.
  - `migrations/000001..000004` — `queues`, `admins`, `reservations` (with the
    partial unique index on `(queue_id, national_id) WHERE status='enqueued'`),
    `sms_outbox`.
  - `Dockerfile` (multi-stage, distroless runtime) + root `docker-compose.yml`
    (Postgres + backend).
  - Admin seeding: `ADMIN_USERNAME`/`ADMIN_PASSWORD` env vars, applied once at
    startup if `admins` table is empty (no admin-management UI, per plan).
- **Verified via `docker compose up -d --build`** (using `sg docker -c "..."`
  since group membership needs a fresh login to apply automatically):
  register → duplicate rejected (409) → admin login → stats → start (renumber +
  full fan-out, confirmed in backend logs via the console SMS adapter) → next
  (serves in order, throttled "your turn" notification fired correctly) → next
  on empty queue → `queue_empty` response → stop → QR PNG endpoint (200,
  image/png) → WebSocket `/ws/stats?token=...` handshake + live frame, all
  behaved as `docs/PLAN.md` specifies. Stack was torn down (`docker compose
  down`) after verification — nothing left running.
- **Git**: `backend/`, `docker-compose.yml`, and `scripts/` are untracked
  (new since last commit) — not yet committed; ask before committing.

## Not yet started

- Customer registration web page (server-rendered HTML + consent checkbox) —
  currently only the JSON API exists, no `html/template` frontend.
- Android admin app (Qt/QML) — nothing started.
- Real SMS gateway adapter (only the console/dev adapter exists so far).
- Unit tests (httptest-based) for the registration/duplicate-check/renumbering
  logic called for in `docs/PLAN.md`'s Verification section.
- Exact bilingual SMS copy (current strings in `internal/sms/messages.go` are
  explicitly marked as placeholders pending the copywriting fast-follow).

## To resume

Just say "continue" / "go" — this file plus `docs/PLAN.md` has everything needed
to pick back up. Next logical step is either the customer registration web page
or the httptest unit-test suite.
