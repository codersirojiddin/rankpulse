# RankPulse

A lightweight, self-hostable SEO keyword rank tracker — built as a $0-tier
alternative to $100/mo tools like Ahrefs, for indie hackers and blog owners.

**Stack:** Go (Chi) · PostgreSQL (Supabase) · Serper.dev · Paddle Billing · Vanilla JS + Tailwind (CDN)

---

## 1. Project Layout

```
rankpulse/
├── cmd/
│   ├── api/              → HTTP server entrypoint (REST API + Paddle webhook)
│   └── worker/           → Background cron worker (daily SERP refresh)
├── internal/
│   ├── auth/              → Supabase JWT verification middleware
│   ├── config/            → Environment variable loading
│   ├── db/                → pgx connection pool setup
│   ├── handlers/          → HTTP handlers (projects, keywords, webhook, router)
│   ├── models/             → Shared domain structs
│   ├── serp/               → Serper.dev client + rank-matching logic
│   └── worker/             → Cron scheduler + concurrent worker pool
├── migrations/
│   └── 0001_init.sql      → Full Supabase schema, RLS policies, triggers
├── web/
│   ├── index.html         → Public landing page (domain.com) — pricing, marketing
│   ├── auth/index.html    → Sign in / sign up (domain.com/auth)
│   ├── dashboard/index.html → Protected app (domain.com/dashboard)
│   └── _redirects         → Clean-URL routing for Cloudflare Pages
├── Dockerfile.api
├── Dockerfile.worker
├── render.yaml            → One-click Render Blueprint (API + worker)
├── Makefile
├── .env.example
└── go.mod
```

---

## 2. Setup (10 minutes)

### Step 1 — Supabase project

1. Create a project at [supabase.com](https://supabase.com) (free tier).
2. Go to **SQL Editor** → paste the contents of `migrations/0001_init.sql` → **Run**.
   This creates all tables, indexes, RLS policies, and an auth trigger that
   auto-populates `public.users` whenever someone signs up via Supabase Auth.
3. Go to **Project Settings → API** and copy:
   - `Project URL` → used in `web/index.html`
   - `anon public` key → used in `web/index.html`
   - `JWT Secret` → used as `SUPABASE_JWT_SECRET` in `.env`
4. Go to **Project Settings → Database → Connection string → Connection pooling**
   and copy the **pooled** URI (port `6543`) → used as `DATABASE_URL`.

### Step 2 — Serper.dev

Sign up at [serper.dev](https://serper.dev) (2,500 free credits/month) and
copy your API key → `SERPER_API_KEY`.

### Step 3 — Paddle Billing

1. Create a [Paddle](https://www.paddle.com) account (sandbox is fine to start).
2. Create a Product + Price for your monthly/annual plans.
3. Go to **Developer Tools → Notifications**, create a webhook pointed at:
   `https://your-api-domain.onrender.com/webhooks/paddle`
   Subscribe to: `subscription.created`, `subscription.updated`, `subscription.canceled`.
4. Copy the webhook's **signing secret** → `PADDLE_WEBHOOK_SECRET`.
5. Go to **Developer Tools → Authentication** and copy your **client-side
   token** (safe to expose publicly, unlike your API key) → used as
   `PADDLE_CLIENT_TOKEN` in `web/auth/index.html`.
6. Create your Product + two Prices (monthly / annual) if you haven't
   already, and copy each **Price ID** (`pri_...`) → used in
   `PADDLE_PRICES` in `web/auth/index.html`.
7. Checkout passes the logged-in user's Supabase `id` as
   `custom_data: { user_id: "<uuid>" }` automatically — this is how the
   webhook handler links a Paddle subscription back to your `users` row.
   No extra setup needed here, it's already wired in `web/auth/index.html`.

### Step 4 — Fill in `.env`

```bash
cp .env.example .env
# then edit .env with the values collected above
```

### Step 5 — Fill in the frontend config

Three pages, each with a small config block at the top of its `<script>`
tag — fill in the **same** `SUPABASE_URL` / `SUPABASE_ANON_KEY` /
`API_BASE_URL` in all three:

- `web/index.html` (landing page)
- `web/auth/index.html` (sign in/up — also needs `PADDLE_CLIENT_TOKEN` and
  `PADDLE_PRICES.monthly` / `PADDLE_PRICES.annual`, i.e. your two `pri_...` IDs)
- `web/dashboard/index.html` (the app)

```js
const SUPABASE_URL = "https://YOUR-PROJECT.supabase.co";
const SUPABASE_ANON_KEY = "YOUR-SUPABASE-ANON-KEY";
const API_BASE_URL = "https://your-api.onrender.com";
```

---

## 3. Run locally

Requires Go 1.22+.

```bash
go mod tidy      # downloads dependencies, generates go.sum
make run-api      # starts the REST API on :8080
make run-worker   # starts the cron worker (separate terminal)
```

Open `web/index.html` directly in a browser (or serve it with any static
file server) to use the dashboard against your local API.

To manually trigger a SERP refresh without waiting for the 03:00 UTC cron,
temporarily call `sched.RunBatch(ctx)` from a short-lived script, or adjust
the cron expression in `internal/worker/scheduler.go` during testing.

---

## 4. Deploy (all free-tier)

| Component | Where | How |
|---|---|---|
| Database | Supabase (free tier) | Already set up in Step 1 |
| API | Render (free Web Service) | Connect this repo, Render auto-detects `render.yaml`, or manually point it at `Dockerfile.api` |
| Worker | Render (free Background Worker) | Same repo, `Dockerfile.worker` |
| Frontend | Cloudflare Pages / Vercel | Deploy the `web/` folder as a static site — routing described below is automatic |

Using the included **Render Blueprint** (`render.yaml`):

```bash
# Push this repo to GitHub, then in the Render dashboard:
# New → Blueprint → select this repo → Render reads render.yaml automatically.
```

You'll be prompted to fill in the env vars marked `sync: false` (all your
secrets) directly in the Render dashboard — they're intentionally excluded
from the blueprint file so nothing sensitive ever touches git.

---

## 5. Frontend routing behavior

Three static pages, each guarding itself on load — no server-side routing
needed, which keeps this deployable as plain static files on Cloudflare
Pages or Vercel:

| Route | File | Behavior |
|---|---|---|
| `domain.com` | `web/index.html` | Public landing page. If a session already exists, redirects to `/dashboard` before rendering any marketing content. Otherwise shows pricing/hero. |
| `domain.com/auth` | `web/auth/index.html` | Sign in / sign up form. If already logged in, redirects onward immediately (to checkout if `?plan=` is set, otherwise `/dashboard`). |
| `domain.com/dashboard` | `web/dashboard/index.html` | The protected app. If there's no session, redirects to `/auth`. Also re-checks on any Supabase auth-state change (e.g. token expiry, sign-out in another tab). |

**Checkout flow:** clicking a pricing button on the landing page links to
`/auth?plan=monthly` (or `annual`). After the user signs in or signs up,
`auth/index.html` opens the Paddle Checkout overlay for that price,
attaching `customData: { user_id }` so the webhook can match the resulting
subscription back to the right Supabase user — then sends them to
`/dashboard`, where their subscription status updates asynchronously once
Paddle's webhook fires.

---

## 6. API Reference

All routes except `/healthz` and `/webhooks/paddle` require:
`Authorization: Bearer <supabase-access-token>`

| Method | Path | Description |
|---|---|---|
| GET | `/healthz` | Liveness check |
| POST | `/webhooks/paddle` | Paddle billing webhook (HMAC-verified) |
| GET | `/projects/` | List the authenticated user's projects |
| POST | `/projects/` | Create a project `{ domain, target_country }` |
| GET | `/projects/{id}` | Get a single project |
| DELETE | `/projects/{id}` | Delete a project (cascades to keywords) |
| GET | `/projects/{id}/keywords/` | List keywords in a project |
| POST | `/projects/{id}/keywords/` | Add a keyword (max 20/project) `{ keyword_text }` |
| DELETE | `/projects/{id}/keywords/{kwId}` | Remove a keyword |
| GET | `/projects/{id}/keywords/{kwId}/history` | 90-day rank history for charting |

---

## 7. How rank checking works

`internal/worker/scheduler.go` runs a daily cron job (03:00 UTC by default)
that:

1. Pulls keywords not checked in the last 24 hours, in pages of 200.
2. Fans each page out across **10 concurrent goroutines**.
3. Each goroutine waits on a shared **token-bucket rate limiter** (5 req/sec
   by default) before calling Serper.dev — tune `requestsPerSecond` in
   `scheduler.go` to match your Serper plan.
4. Parses the JSON response, finds the first organic result whose domain
   matches the project's domain, and records that position.
5. Writes the new position to `keywords` (shifting the old one into
   `previous_position`) and appends a row to `rank_history` inside a single
   transaction, so the two tables never drift out of sync.

---

## 8. Notes on the free-tier limit

The 20-keywords-per-project cap is enforced server-side in
`internal/handlers/keywords.go` (`Create`), returning `402 Payment Required`
once reached. To introduce paid tiers with higher limits later, add a
`plan_keyword_limit` column (or lookup table) keyed off `users.plan_type`
and swap out the hardcoded `models.MaxKeywordsPerProject` check.

---

## 9. Security notes

- Row-Level Security is enabled on every table; the API additionally
  double-checks project ownership in Go before any read/write, so even a
  bug in RLS policy logic can't leak cross-user data through the API layer.
- The worker connects using the Supabase **service_role** key equivalent
  (i.e. the direct `DATABASE_URL`, not a user JWT), which intentionally
  bypasses RLS — it needs to read/write across all users' keywords to run
  the daily batch.
- The Paddle webhook handler verifies the `Paddle-Signature` HMAC before
  processing any payload — never trust an unverified webhook body.
