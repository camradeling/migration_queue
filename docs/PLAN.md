# Government-style Customer Queue Service — Architecture & Scope Plan

## Context

This is a greenfield project (empty directory). The goal is a queue-management
system modeled on government-office ticketing: a customer scans a QR code,
registers via a mobile web page (ФИО / ИИН / phone), gets a queue number, and
is notified by SMS as their turn approaches. An administrator uses an Android
app (Qt/QML, iOS later) to advance the queue ("Next"), and to "Start"/"Stop"
the day's service. The terminology (ФИО, ИИН) and the environment locale
indicate this targets Kazakhstan, which matters for SMS providers and
personal-data-hosting rules (see Open Questions).

Confirmed decisions from clarification with the user:
- **Mobile app**: Qt/QML (C++ backend + QML UI), targeting Android now, iOS later from the same codebase.
- **Backend**: Go, using Gin.
- **Queue model**: v1 ships a single queue, but the data model and API must not hardcode "one queue" — a `queues` table/`queue_id` foreign key is present from day one so multi-queue (per department/window) is a additive change later, not a rewrite.
- **SMS**: build against a generic `SMSSender` interface; ship one concrete adapter for a placeholder Kazakhstan-market gateway (e.g. Mobizon/SMSC.kz-style HTTP API) plus a console/log adapter for local dev.

## Component Overview

1. **Backend web server** (Go) — REST + WebSocket API, Postgres-backed, serves the customer registration web page, handles SMS dispatch, exposes admin actions (Next/Start/Stop) and live stats.
2. **Customer registration web page** — server-rendered HTML (no SPA needed for a 3-field form), reached via the QR code link.
3. **Android admin app** (Qt/QML, C++) — login, live dashboard (current number, total enqueued, avg serving time), Next/Start/Stop buttons.
4. **QR code utility** — small Go library + CLI, also exposed as an admin API endpoint so QR codes can be (re)generated/printed per queue without shell access.

## Data Model (sketch)

- `queues` — id, name, is_running (bool). Seeded with exactly one row for v1.
- `admins` — id, username, password_hash (bcrypt), (roles later if needed).
- `reservations` (the "reserve" table from the spec):
  - id (uuid/bigserial)
  - queue_id (FK → queues)
  - full_name (ФИО)
  - national_id (ИИН, char(12), validated numeric)
  - phone (E.164)
  - status (`enqueued` | `served`)
  - queue_number (int, mutable — see renumbering logic below)
  - last_notified_ahead_count (nullable int — used by Next's throttled-notification rule; seeded at registration time from the customer's initial position)
  - created_at, served_at
  - **Partial unique index** on `(queue_id, national_id) WHERE status = 'enqueued'` — enforces "decline duplicate if already enqueued" atomically at the DB level, avoiding a race between the app-level check and the insert.
- `queue_state` (or columns on `queues`) — `current_serving_number` per queue, used to compute "how many ahead of you" as `count(enqueued WHERE queue_number < mine)`.

**Renumbering ("Start")**: batch-update all `enqueued` rows for the queue, ordered by `created_at` (FIFO), to `queue_number = 1..N`, then fan out SMS with each customer's new position. **Registration still assigns a provisional number immediately** (spec: "every enqueued record receives a number") so the position is defined even before the first Start of the day; Start just re-normalizes gaps left by served customers. This assumption is flagged in Open Questions for confirmation.

## Key API Endpoints (indicative)

- `POST /api/register` — public, called from the web page. Validates ФИО/ИИН/phone, checks duplicate-enqueued via the DB constraint, assigns queue_number, returns the customer's position.
- `GET /api/status/:id` — public, lets the registration page poll "N people ahead of you".
- `POST /api/admin/login` — returns JWT.
- `POST /api/admin/next` — advances `current_serving_number`, marks that reservation `served`, triggers **throttled** SMS notification (next 10 always, others only on ≥10% position change — see Notification Throttling).
- `POST /api/admin/start` — sets `is_running = true`, renumbers as above, full SMS fan-out to all enqueued (bypasses throttling).
- `POST /api/admin/stop` — sets `is_running = false`, sends "closed for today, queue preserved" SMS to all `enqueued` (bypasses throttling).
- `GET /api/admin/stats` (+ WebSocket channel) — current number, total enqueued, average serving time. WebSocket handshake must carry the JWT explicitly (query param or `Sec-WebSocket-Protocol` header) since browsers/`gorilla/websocket` don't attach cookies/Authorization headers automatically the way plain REST calls do — an easy point to accidentally ship unauthenticated.
- `GET /api/admin/qrcode?queue_id=1` — returns QR PNG encoding the registration URL for that queue.

**Concurrency control for Next/Start**: `next`/`start`/`stop` all read-modify-write the same `queues` row (current_serving_number, is_running) — wrap each in a transaction that does `SELECT ... FOR UPDATE` on that row first, so two admin devices (or a retry) hitting `Next` at once serialize instead of racing to double-advance or double-send SMS.

## Technology & Library List

### Backend (Go)
- **Web framework**: Gin (`github.com/gin-gonic/gin`)
- **DB**: PostgreSQL — chosen for transactional guarantees around number assignment/duplicate checks
- **DB access**: `sqlx` (`github.com/jmoiron/sqlx`) + hand-written SQL, or `sqlc` for type-safe generated queries — avoids ORM magic for logic that needs precise transaction control
- **Migrations**: `golang-migrate/migrate`
- **WebSocket**: `gorilla/websocket`
- **Auth**: `golang-jwt/jwt` + `golang.org/x/crypto/bcrypt`
- **Validation**: `go-playground/validator/v10`; `github.com/nyaruka/phonenumbers` for phone parsing/validation
- **QR generation**: `github.com/skip2/go-qrcode`
- **CORS**: `gin-contrib/cors`
- **Rate limiting**: only on `/api/admin/login` (brute-force password guessing) via `golang.org/x/time/rate` per-username/IP middleware. The public `/api/register` endpoint does **not** need IP rate limiting — the `(queue_id, national_id)` partial unique index already blocks the abuse case that matters (one ИИН, multiple active enqueues); no other throttle is needed for v1.
- **Logging**: stdlib `log/slog`
- **Config**: env vars via a small config struct (`github.com/joho/godotenv` for local dev)
- **Testing**: stdlib `net/http/httptest` + `github.com/stretchr/testify`
- **SMS**: custom `SMSSender` interface; concrete HTTP adapter for the chosen KZ gateway; console adapter for dev
- **Background SMS fan-out**: durable **`sms_outbox` table** (reservation_id, message, status: pending/sent/failed, attempts, created_at) written in the same DB transaction as the Next/Start/Stop state change, drained by an in-process worker pool. A crash mid-fan-out loses nothing — the worker resumes from `pending` rows on restart, and failed sends are retried and auditable. A pure in-memory channel (no outbox) was the original idea but silently drops messages on crash/restart, which is unacceptable for a system whose entire UX contract is "you will be SMS'd" — see Risk Review.

### Customer web page
- Server-rendered via Go's stdlib `html/template` + minimal vanilla CSS/JS (a small `fetch()` poll for live position) — a full SPA framework is unjustified for a 3-field form + status view

### Android/Qt admin app
- **Qt 6** (LTS) with QML for UI, C++ for logic
- **Networking**: Qt Network (`QNetworkAccessManager`, JSON via `QJsonDocument`) for REST; `QtWebSockets` for the live stats channel
- **State exposure to QML**: a C++ `QueueController` singleton/context object with `Q_PROPERTY` bindings (currentNumber, totalEnqueued, avgServingTime, isRunning) and `Q_INVOKABLE` methods (`next()`, `start()`, `stop()`)
- **Local storage**: `QSettings` for the cached auth token only — server remains source of truth
- **Android packaging**: Qt for Android via Qt Creator / `androiddeployqt`, requires Android SDK + NDK

### QR utility
- Go library function (reusing `go-qrcode`) called both from a `cmd/qrgen` CLI and from the `/api/admin/qrcode` endpoint — one implementation, two entry points

### Infra / deployment
- `docker-compose` for local dev (Go backend + Postgres)
- Reverse proxy (nginx or Caddy) for TLS termination in front of the Go server

## Risk Review (devil's advocate pass) — resolved with user

1. **Qt licensing — resolved: open-source, LGPL.** Since the whole app will be open-source, LGPL compliance is straightforward: Qt libraries are dynamically linked by default, and publishing the app's own source (already the plan) satisfies the "user can re-link against a different Qt build" obligation. No commercial license needed. No further action beyond making sure the Android build actually dynamic-links Qt (the default) rather than a static Qt build.
2. **SMS fan-out volume — resolved: throttled notifications.** Instead of messaging every enqueued customer on every Next, see the new **Notification Throttling** section below.
3. **Durable `sms_outbox`** — confirmed, keeping as designed above.
4. **Original queue number audit trail — resolved: not needed.** Customers only care about current position and count-ahead, not their original number. `queue_number` stays mutable with no separate immutable column.
5. **Postgres vs SQLite — resolved: Postgres.**
6. **Rate limiting — resolved: drop IP-based limiting on `/api/register`.** The partial unique index on `(queue_id, national_id) WHERE status='enqueued'` already prevents the actual abuse case named in the spec (one customer, multiple active enqueues) — no IP tracking needed for that. `/api/register` otherwise only needs ИИН format validation (12 digits). Note this is a different concern from **`/api/admin/login`**, which still needs its own brute-force rate limiting (wrong-password guessing) since that threat has nothing to do with ИИН uniqueness — kept in the plan.

## Notification Throttling (Next fan-out)

On every `Next`, instead of SMS-ing all enqueued customers:
- Add `last_notified_ahead_count` (nullable int) to `reservations`, tracking each customer's ahead-count as of their last SMS.
- Recompute `ahead_count` for every remaining enqueued reservation (`count(enqueued WHERE queue_number < mine)`).
- **Always notify** the customers with `ahead_count < 10` (the next 10 in line) — every Next, no threshold.
- **For everyone else**, notify only if their `ahead_count` has moved by ≥10% relative to `last_notified_ahead_count` (e.g. `abs(new - last) >= max(1, ceil(0.10 * last))`), then update `last_notified_ahead_count` to the new value.
- `ahead_count == 0` is the "your turn" message, per spec.
- `Start` and `Stop` are **not** subject to this throttle — both are one-off, full-fan-out events by nature (Start = "here's your new number", Stop = closure notice), so they always message every enqueued customer and reset `last_notified_ahead_count` to the fresh value for everyone touched.
- The customer's first-ever position (right after registering) is shown directly on the web confirmation page, not via SMS — `last_notified_ahead_count` is seeded from that value at registration time, so the very first SMS they might get is already threshold-compared correctly.

## Open Questions / Unclear Points (need answers before or early in implementation)

1. **Renumbering semantics**: Confirm registration assigns a number immediately (even before any "Start"), and "Start" only re-normalizes gaps — vs. an alternative reading where numbers are assigned only at Start time.
2. **Cross-day carryover**: On the first "Start" of a new day, are customers still `enqueued` from a previous day's unclosed queue mixed in FIFO order with brand-new registrants, or given strict priority?
3. **"Average serving time" definition**: interval between consecutive "Next" clicks (service throughput) vs. average wait time (`served_at - created_at`)? Plan currently assumes the former; needs confirmation.
4. **Empty-queue "Next"**: What should happen (no-op + message?) if admin clicks "Next" with zero enqueued customers?
5. **Customer self-cancellation**: Is there any way for a customer to leave the queue themselves (link in SMS, web page action), or is removal admin-only (implicitly, only via "served")?
6. **Multiple simultaneous admin devices**: Even in v1's single-queue design, could two admin phones both hit "Next" for the same queue? Affects whether we need to design for concurrent-safe transactions now (recommended to do regardless, but confirms priority).
7. **SMS content & language**: exact wording, Kazakh/Russian/English (or multiple), and whether the chosen gateway requires a registered alphanumeric Sender ID.
8. **Personal data compliance**: Kazakhstan's personal-data-protection law has data-localization implications for storing ИИН/phone/ФИО (citizen PII) — need to confirm hosting location and whether an explicit consent checkbox is required on the registration page.
9. **Data retention**: how long served/expired reservations are kept before deletion/anonymization.
10. **Admin account management**: is a single hardcoded admin account sufficient for v1, or is an admin-management UI/API needed now?
11. **Deployment target**: where will the backend actually run (VPS/cloud provider vs. on-prem server at the office)? Affects TLS/domain setup and the data-localization question above.
12. **Public status page richness**: is a one-time "you are #N, M ahead of you" confirmation enough, or should the page live-poll position updates until served?

Resolved during Risk Review: Qt/LGPL licensing, SMS fan-out volume (now throttled), renumbering audit trail (not needed), Postgres choice, and register-endpoint rate limiting — see Risk Review section above.

## Verification

- Backend: unit tests for the registration/duplicate-check/renumbering logic (especially the concurrent-Next and duplicate-ИИН edge cases) using `httptest` against a real test Postgres (via `docker-compose`); manual `curl`/Postman pass through register → start → next → stop.
- Customer web page: manually scan a generated QR code on a phone, complete registration, confirm duplicate ИИН is rejected while still enqueued.
- Admin app: run against the local backend, exercise Start/Next/Stop and confirm the dashboard numbers and SMS fan-out (using the console SMS adapter to observe fired messages in dev) match expectations.
