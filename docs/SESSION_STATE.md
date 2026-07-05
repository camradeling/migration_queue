# Session State — resume point

Written before a session/terminal restart, so work can pick up cleanly.

## Where things stand

- **Plan**: finalized and committed — see `docs/PLAN.md`. All 17 open questions from the
  risk review are resolved (see its "Decisions Log" section). No design blockers remain.
- **Git**: repo is `camradeling/migration_queue` on GitHub, `master` branch, pushed and
  up to date as of this point.
- **Prerequisites**:
  - **Go 1.26.4**: installed at `/usr/local/go`. `go`/`gofmt` symlinked into
    `~/.local/bin` (already on `PATH` for non-interactive shells) — this was necessary
    because `/etc/profile.d/go.sh` (written by `scripts/install_prereqs.sh`) is only
    read by login shells, and `~/.bashrc` has an early `return` for non-interactive
    shells that skips the appended `PATH` export. **Confirmed working**: `go version`
    → `go1.26.4 linux/amd64`.
  - **Docker**: installed (`docker --version` → 29.6.1, `docker compose version` →
    v5.3.0). `usermod -aG docker $USER` was run, but the current login session's shell
    was started before that change, so `docker ps` still fails with
    `permission denied ... docker.sock` in this session. **This is why the session is
    being restarted** — a fresh login session will pick up the updated `/etc/group`
    membership. After restart, verify with `docker ps` (should list nothing, no error).
  - `scripts/install_prereqs.sh` is idempotent — safe to re-run if anything looks off
    after restart.

## Not yet started

No backend code exists yet. Next step (once `docker ps` works post-restart) is the
Go module scaffold per `docs/PLAN.md`:
- `go.mod` for the backend module
- Directory layout: `cmd/server`, `cmd/qrgen`, `internal/{config,db,api,queue,sms}`
- `migrations/` (golang-migrate SQL) for `queues`, `admins`, `reservations`, `sms_outbox`
- `docker-compose.yml` (Postgres + backend service) for containerized dev
- Wire up the endpoints and logic described in `docs/PLAN.md`'s "Key API Endpoints",
  "Notification Throttling", and "Concurrency control" sections

## To resume

Just say "continue" / "go" — this file plus `docs/PLAN.md` has everything needed to
pick back up without re-deriving context.
