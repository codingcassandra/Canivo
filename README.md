# Canivo

Canivo watches a dog's breathing through an EMG sensor and pushes a
notification to the owner's phone when the dog goes on high alert, so they can
redirect the dog before it reacts.

```
EMG sensor → Arduino Nano ──USB serial──▶ bridge ──HTTP──▶ server ──Expo push──▶ owner's phone
             (firmware/)                 (cmd/bridge)     (cmd/server)     (mobile/)
```

## How detection works

1. **Envelope** – the Nano sends the EMG envelope (muscle activity level) at 50 Hz.
2. **Breaths** – each breath is a burst of respiratory-muscle activity. The
   server smooths the signal, subtracts a slow trend and counts rises above it
   (`internal/breath`). It reports breaths/minute and average muscle tension over
   a 15 s window.
3. **Baseline** – every dog is different, so the first 2 minutes of data (dog
   calm!) become that dog's baseline. Re-run any time with `POST /calibrate`.
4. **Arousal score** – `0.5·|rate z-score| + 0.5·(tension z-score, increases only)`.
   Rate counts in both directions because alert dogs often freeze and hold their
   breath. Score ≥ 2.5 for 5 s → **alert** + push. Score < 1.5 for 10 s → calm.
   Pushes are rate-limited to one per dog every 2 minutes (`internal/monitor`).

## Run it

```sh
# 1. server (port 8090; 8080 is commonly taken)
NOTIFIER=log go run ./cmd/server          # prints pushes instead of sending

# 2. register a dog
curl -XPOST localhost:8090/api/dogs -d '{"name":"Rex"}'

# 3a. no hardware: simulated dog (calm 3 min, then an alert episode every 90 s)
go run ./cmd/bridge -dog 1 -simulate

# 3b. real hardware: flash firmware/canivo_nano/canivo_nano.ino, then
go run ./cmd/bridge -list                 # find the port
go run ./cmd/bridge -dog 1 -port /dev/cu.usbserial-110
```

Close the Arduino IDE's Serial Monitor first—only one program can hold the port.

Tests: `go test ./...`

## Phone app (`mobile/`)

Expo / React Native. Shows live status, breathing vs. the dog's normal, alert
history, and receives the push alerts.

```sh
cd mobile
npm install
npx expo start            # scan the QR code with the Expo Go app
```

- Phone and computer must be on the same WiFi. The server URL is your
  computer's LAN IP (`ipconfig getifaddr en0`), pre-filled from
  `mobile/.env.local` (`EXPO_PUBLIC_SERVER_URL=http://<ip>:8090`). Type the API
  key on the setup screen. Don't set `EXPO_PUBLIC_API_KEY` for builds you share:
  `EXPO_PUBLIC_*` values are embedded in the app.
- **Push notifications** need a free Expo account, once: `npx eas-cli@latest login`
  then `npx eas-cli@latest init` inside `mobile/` (adds the project ID to `app.json`).
  On iPhone, Expo Go then receives pushes. On Android, Expo Go can't; use a
  development build (`npx eas-cli@latest build --profile development --platform android`).
- Run the server **without** `NOTIFIER=log` to send real pushes.

## Web app + Apple Watch (`mobile/`, deployed to Vercel)

The same app runs in a browser. Alerts arrive as web push notifications, and
on an iPhone those are mirrored to a paired Apple Watch, which taps your wrist.

- `npx expo start --web` to run it locally; `npx expo export -p web` builds `mobile/dist`.
- `public/sw.js` is the service worker that shows notifications;
  `public/manifest.json` makes it installable.
- **iPhone:** web push only works from the Home Screen: open the site in
  Safari → Share → **Add to Home Screen** → open it from there → **Turn on alerts**.
  The watch buzzes while the iPhone is locked (unlocked, the alert shows on the phone).

Deploy: Vercel → New Project → import this repo → **Root Directory `mobile`** →
env var `EXPO_PUBLIC_SERVER_URL=https://canivo.onrender.com` → Deploy
(`mobile/vercel.json` sets the build). Then on Render set `ALLOWED_ORIGINS` to
the Vercel URL and the `VAPID_*` vars (`go run ./cmd/vapidkeys`).

## Configuration (server env vars)

| Var | Default | |
|---|---|---|
| `PORT` | `8090` | |
| `API_KEY` | – | clients send `Authorization: Bearer <key>`; unset = no auth (local only) |
| `DATABASE_URL` | – | Postgres URL; unset = SQLite at `DB_PATH` |
| `DB_PATH` | `canivo.db` | SQLite file |
| `NOTIFIER` | `expo` | `log` to print instead of sending |
| `EXPO_ACCESS_TOKEN` | – | only if Expo push security is on |
| `PUSH_COOLDOWN` | `2m` | min time between pushes per dog |
| `VAPID_PUBLIC_KEY` / `VAPID_PRIVATE_KEY` | – | browser push; generate with `go run ./cmd/vapidkeys` |
| `VAPID_SUBJECT` | – | your email; required by Apple with VAPID keys |
| `ALLOWED_ORIGINS` | – | comma-separated web app URLs allowed to call the API |

## Deploy (Render + Supabase)

The server keeps live detection state in memory, so it needs an always-running
host (not serverless like Vercel).

1. **Database:** create a free project at supabase.com. Click **Connect** and
   copy the **Session pooler** connection string (Render can't reach Supabase's
   IPv6-only direct connection). Put in your DB password; URL-encode special
   characters (e.g. `@` → `%40`). The server creates its tables on startup.
2. **Server:** in Render, **New → Blueprint**, pick this repo. It reads
   `render.yaml`. Paste the connection string for `DATABASE_URL` when asked.
3. Copy the generated `API_KEY` from the service's **Environment** tab and check
   `https://<your-service>.onrender.com/health`.
4. **Bridge:** `go run ./cmd/bridge -server https://<your-service>.onrender.com -key <API_KEY> -dog 1 -simulate`
5. **App:** enter the Render URL and API key on the setup screen.

Free tier notes: Render sleeps after 15 min with no requests (a running bridge
keeps it awake; waking takes ~30 s). Supabase pauses a project after a week of
no database activity.

## API

All `/api/*` routes need `Authorization: Bearer <API_KEY>` when the server has one.

| Method | Path | Body / notes |
|---|---|---|
| GET | `/health` | |
| POST | `/api/dogs` | `{"name":"Rex"}` |
| GET | `/api/dogs`, `/api/dogs/{id}` | includes learned baseline |
| POST | `/api/dogs/{id}/samples` | `{"samples":[{"t":<millis>,"v":<value>}]}` (≤ 5000) → status |
| GET | `/api/dogs/{id}/status` | `state` (`calibrating`/`calm`/`alert`), `rate_bpm`, `tension`, `score`, `calibration_progress` |
| POST | `/api/dogs/{id}/calibrate` | forget baseline, relearn from the next 2 min |
| GET | `/api/dogs/{id}/alerts?limit=50` | alert history |
| POST | `/api/dogs/{id}/push-tokens` | `{"token":"ExponentPushToken[...]"}` from the Expo app |
| DELETE | `/api/push-tokens/{token}` | e.g. on logout |
| GET | `/api/web-push/key` | `{"public_key":...}` VAPID key for `pushManager.subscribe` |
| POST | `/api/dogs/{id}/web-push` | a browser `PushSubscription.toJSON()` |
| POST | `/api/web-push/unsubscribe` | `{"endpoint":...}` |

Push payload `data`: `{"type":"alert","dog_id":1,"alert_id":7}`.

## Layout

```
cmd/server      HTTP server
cmd/bridge      serial → HTTP forwarder (+ simulator)
cmd/vapidkeys   generates web push keys
internal/breath breath detection from EMG envelope (+ synthetic signal)
internal/monitor baseline, arousal score, calm/alert state machine
internal/store  SQLite: dogs, baselines, alerts, push tokens
internal/notify Expo push + Web Push
internal/api    HTTP handlers
firmware/       Arduino Nano sketch
mobile/         Expo app: iOS/Android + web (Vercel)
```

## Not done yet

- **Accounts** – one shared API key protects the server, but everyone with it sees every dog. Add owner accounts before real users.
- **Wireless** – the Nano R3 has no radio. Options: HC-05/HM-10 Bluetooth module → phone, or swap to an ESP32 / Nano 33 IoT / Nano ESP32 that posts directly.
- **Tuning on a real dog** – thresholds come from synthetic data. Exercise, heat-panting and barking also change breathing; expect to tune `monitor.DefaultConfig()`.
- Live state is in memory, so the server runs as a single instance.
