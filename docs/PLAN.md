# Government-style Customer Queue Service — Architecture & Scope Plan

## Context

This is a greenfield project. The goal is a queue-management system modeled on
government-office ticketing: a customer scans a QR code, registers via a
mobile web page (ФИО / ИИН / phone), gets a queue number, and is notified by
SMS as their turn approaches. An administrator uses an Android app (Qt/QML,
iOS later) to advance the queue ("Next"), and to "Start"/"Stop" the day's
service. This targets Kazakhstan (ФИО/ИИН terminology, KZ personal-data law,
KZ-hosted infra, bilingual Kazakh+Russian messaging).

Confirmed decisions:
- **Mobile app**: Qt/QML (C++ backend + QML UI), targeting Android now, iOS later from the same codebase. Open-source, LGPL — no commercial license needed since Qt is dynamically linked and the app's own source is published.
- **Backend**: Go, using Gin.
- **Queue model**: v1 ships a single queue, but the data model/API don't hardcode "one queue" — a `queues` table/`queue_id` foreign key is present from day one so multi-queue (per department/window) is additive later, not a rewrite.
- **SMS**: generic `SMSSender` interface; one concrete adapter for a placeholder Kazakhstan-market gateway (e.g. Mobizon/SMSC.kz-style HTTP API), plus a console/log adapter for dev. Messages are bilingual Kazakh + Russian.
- **Hosting**: Kazakhstan-based cloud VPS, to satisfy data-localization expectations for citizen PII.

## Component Overview

1. **Backend web server** (Go) — REST + WebSocket API, Postgres-backed, serves the customer registration web page, handles SMS dispatch, exposes admin actions (Next/Start/Stop) and live stats.
2. **Customer registration web page** — server-rendered HTML, one-time confirmation view (no live polling — see Decisions Log). Includes a required personal-data consent checkbox.
3. **Android admin app** (Qt/QML, C++) — login, live dashboard (current number, total enqueued, avg serving time), Next/Start/Stop buttons.
4. **QR code utility** — small Go library + CLI, also exposed as an admin API endpoint so QR codes can be (re)generated/printed per queue without shell access.

## Data Model (sketch)

- `queues` — id, name, is_running (bool), current_serving_number. Seeded with exactly one row for v1.
- `admins` — id, username, password_hash (bcrypt). **v1: a single seeded admin account** (via migration/env var) — no admin-management UI/API yet.
- `reservations` (the "reserve" table from the spec):
  - id (uuid/bigserial)
  - queue_id (FK → queues)
  - full_name (ФИО)
  - national_id (ИИН, char(12), validated numeric)
  - phone (E.164)
  - status (`enqueued` | `served`) — **no `cancelled` status in v1**; customers cannot self-remove, only admin "Next" transitions to `served`.
  - queue_number (int, mutable, assigned immediately at registration — see Renumbering below)
  - last_notified_ahead_count (nullable int — used by Next's throttled-notification rule; seeded at registration time from the customer's initial position)
  - consent_accepted (bool, must be `true` to insert) — backs the required personal-data consent checkbox on the registration form
  - created_at, served_at
  - **Partial unique index** on `(queue_id, national_id) WHERE status = 'enqueued'` — enforces "decline duplicate if already enqueued" atomically at the DB level.
  - **No retention/expiry job in v1** — records are kept indefinitely; revisit once a real retention policy is defined.
- `sms_outbox` — reservation_id, message, status (pending/sent/failed), attempts, created_at. Durable log drained by a worker pool; survives crashes/restarts.

**Renumbering ("Start")**: batch-update all `enqueued` rows for the queue, ordered by `created_at` (FIFO — this naturally spans days too, since timestamps are monotonic; no special-casing needed for carryover from a previous day), to `queue_number = 1..N`, then full SMS fan-out with each customer's new position. Registration assigns a provisional number immediately at insert time; Start just re-normalizes gaps left by served customers.

## Key API Endpoints (indicative)

- `POST /api/register` — public. Validates ФИО/ИИН/phone + `consent_accepted == true`, checks duplicate-enqueued via the DB constraint, assigns queue_number, returns the customer's position for the one-time confirmation page (no separate status-polling endpoint needed in v1).
- `POST /api/admin/login` — returns JWT.
- `POST /api/admin/next` — advances `current_serving_number`, marks that reservation `served`, triggers throttled SMS notification (next 10 always, others only on ≥10% position change — see Notification Throttling). If the queue is empty, returns a `queue_empty` response (no-op; `current_serving_number` unchanged) that the app surfaces as a message.
- `POST /api/admin/start` — sets `is_running = true`, renumbers as above, full SMS fan-out to all enqueued (bypasses throttling).
- `POST /api/admin/stop` — sets `is_running = false`, sends "closed for today, queue preserved" SMS to all `enqueued` (bypasses throttling).
- `GET /api/admin/stats` (+ WebSocket channel) — current number, total enqueued, average serving time (= rolling average of intervals between consecutive `Next` timestamps — reflects admin throughput, not customer wait time). WebSocket handshake must carry the JWT explicitly (query param or `Sec-WebSocket-Protocol` header) since it isn't attached automatically the way plain REST Authorization headers are.
- `GET /api/admin/qrcode?queue_id=1` — returns QR PNG encoding the registration URL for that queue.

**Concurrency control for Next/Start**: `next`/`start`/`stop` all read-modify-write the same `queues` row — wrap each in a transaction that does `SELECT ... FOR UPDATE` on that row first, so two admin devices (or a retry) hitting `Next` at once serialize instead of racing to double-advance or double-send SMS. This is DB-level and correct regardless of whether multiple admin devices are ever actually used.

## Technology & Library List

### Backend (Go)
- **Web framework**: Gin (`github.com/gin-gonic/gin`)
- **DB**: PostgreSQL — transactional guarantees for number assignment/duplicate checks
- **DB access**: `sqlx` (`github.com/jmoiron/sqlx`) + hand-written SQL, or `sqlc` — avoids ORM magic for logic that needs precise transaction control
- **Migrations**: `golang-migrate/migrate`
- **WebSocket**: `gorilla/websocket`
- **Auth**: `golang-jwt/jwt` + `golang.org/x/crypto/bcrypt`
- **Validation**: `go-playground/validator/v10`; `github.com/nyaruka/phonenumbers` for phone parsing/validation
- **QR generation**: `github.com/skip2/go-qrcode`
- **CORS**: `gin-contrib/cors`
- **Rate limiting**: only on `/api/admin/login` (brute-force password guessing) via `golang.org/x/time/rate`. `/api/register` needs no IP rate limiting — the `(queue_id, national_id)` partial unique index already blocks the abuse case that matters.
- **Logging**: stdlib `log/slog`
- **Config**: env vars via a small config struct (`github.com/joho/godotenv` for local dev)
- **Testing**: stdlib `net/http/httptest` + `github.com/stretchr/testify`
- **SMS**: custom `SMSSender` interface; concrete HTTP adapter for the chosen KZ gateway; console adapter for dev
- **Background SMS fan-out**: durable `sms_outbox` table written in the same DB transaction as the Next/Start/Stop state change, drained by an in-process worker pool

### Customer web page
- Server-rendered via Go's stdlib `html/template` + minimal vanilla CSS/JS — a full SPA framework is unjustified for a 3-field form + one-time confirmation view. Confirmation page shows position once; further updates arrive via SMS, not polling.

### Android/Qt admin app
- **Qt 6** (LTS) with QML for UI, C++ for logic
- **Networking**: Qt Network (`QNetworkAccessManager`, JSON via `QJsonDocument`) for REST; `QtWebSockets` for the live stats channel
- **State exposure to QML**: a C++ `QueueController` singleton/context object with `Q_PROPERTY` bindings (currentNumber, totalEnqueued, avgServingTime, isRunning) and `Q_INVOKABLE` methods (`next()`, `start()`, `stop()`)
- **Local storage**: `QSettings` for the cached auth token only — server remains source of truth
- **Android packaging**: Qt for Android via Qt Creator / `androiddeployqt`, requires Android SDK + NDK; ensure the build dynamically links Qt (the default) to keep LGPL compliance straightforward

### QR utility
- Go library function (reusing `go-qrcode`) called both from a `cmd/qrgen` CLI and from the `/api/admin/qrcode` endpoint — one implementation, two entry points

### Infra / deployment
- `docker-compose` for local dev (Go backend + Postgres)
- Reverse proxy (nginx or Caddy) for TLS termination
- Production: Kazakhstan-based cloud VPS provider, for data-localization

## Notification Throttling (Next fan-out)

On every `Next`, instead of SMS-ing all enqueued customers:
- `reservations.last_notified_ahead_count` tracks each customer's ahead-count as of their last SMS.
- Recompute `ahead_count` for every remaining enqueued reservation (`count(enqueued WHERE queue_number < mine)`).
- **Always notify** customers with `ahead_count < 10` (the next 10 in line) — every Next, no threshold.
- **Everyone else**: notify only if `ahead_count` moved by ≥10% relative to `last_notified_ahead_count` (e.g. `abs(new - last) >= max(1, ceil(0.10 * last))`), then update `last_notified_ahead_count`.
- `ahead_count == 0` is the "your turn" message.
- `Start` and `Stop` bypass this throttle — both are one-off, full-fan-out events by nature, and reset `last_notified_ahead_count` to the fresh value for everyone touched.
- The customer's first-ever position is shown on the one-time web confirmation page, not via SMS — `last_notified_ahead_count` is seeded from that value at registration time.

## SMS Content & Language

- Messages are **bilingual: Kazakh + Russian** in a single SMS (two lines, one per language).
- **Cost/technical note**: Cyrillic (and Kazakh-specific characters) require UCS-2 SMS encoding — 70 characters per segment, vs. 160 for plain GSM-7. A bilingual two-line message will likely span 2–3 SMS segments per notification; factor this into gateway cost estimates once real volume is known (see Notification Throttling above for how volume itself is kept down).
- Exact message wording is still TBD (a fast-follow copywriting task), but the bilingual two-language structure is now fixed.
- Gateway sender-ID registration (if the chosen KZ provider requires an alphanumeric sender ID) is an operational setup task once a concrete provider is picked — doesn't block the `SMSSender` interface design.

## Personal Data Compliance

- Registration requires an explicit **consent checkbox** (bilingual KK/RU copy) before submission; backed by `reservations.consent_accepted`.
- Backend + database are hosted on a **Kazakhstan-based VPS provider** to align with data-localization expectations for citizen PII (ФИО/ИИН/phone).
- No data-retention/expiry policy in v1 — reservations are kept indefinitely; revisit if/when a formal retention requirement is defined.

## Decisions Log

All open questions from the initial risk review are now resolved:

1. Registration assigns queue_number immediately; Start only re-normalizes gaps.
2. Cross-day carryover uses strict FIFO by `created_at` — no special-casing needed.
3. "Average serving time" = interval between consecutive Next clicks (throughput, not customer wait time).
4. Empty-queue Next = no-op + friendly message (`queue_empty` response).
5. No customer self-cancellation in v1 — admin-only via "served"; no `cancelled` status.
6. Concurrent admin devices are handled regardless via `SELECT ... FOR UPDATE` row locking — no extra design needed either way.
7. SMS/UI language: bilingual Kazakh + Russian (see SMS Content & Language).
8. Personal-data compliance: KZ-hosted + consent checkbox (see Personal Data Compliance).
9. Data retention: kept indefinitely for now.
10. Admin accounts: single seeded account for v1, no management UI.
11. Deployment: Kazakhstan-based cloud VPS.
12. Public status page: one-time confirmation only, no live polling.
13. Qt licensing: open-source/LGPL, no commercial license needed.
14. SMS fan-out volume: throttled (next 10 always + ≥10% threshold for the rest), not "notify everyone every time."
15. Renumbering audit trail: not needed — `queue_number` stays simply mutable.
16. DB choice: PostgreSQL.
17. Rate limiting: register endpoint relies on the ИИН unique-index constraint, not IP throttling; login endpoint keeps brute-force rate limiting.

## Verification

- Backend: unit tests for the registration/duplicate-check/renumbering logic (especially the concurrent-Next and duplicate-ИИН edge cases) using `httptest` against a real test Postgres (via `docker-compose`); manual `curl`/Postman pass through register → start → next → stop.
- Customer web page: manually scan a generated QR code on a phone, complete registration (including the consent checkbox), confirm duplicate ИИН is rejected while still enqueued.
- Admin app: run against the local backend, exercise Start/Next/Stop and confirm the dashboard numbers and throttled SMS fan-out (using the console SMS adapter to observe fired messages in dev) match expectations.
