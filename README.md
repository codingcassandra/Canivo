# Canivo

Canivo watches a dog's breathing through an EMG sensor and pushes a
notification to the owner's phone when the dog goes on high alert, so they can
redirect the dog before it reacts. It also watches a GPS collar and alerts the
owner the moment the dog leaves a safe zone around home.

```
EMG sensor → Arduino Nano ──USB serial──▶ bridge ──HTTP──▶ server ──Expo push──▶ owner's phone
             (firmware/)                 (cmd/bridge)     (cmd/server)     (mobile/)
```

## Safe zone (GPS)

The **Location** card only appears once a GPS collar has sent a location, so
the app never suggests it can track a dog without one. Then, under **Location**, stand at home, pick a size and tap **Set home
here**. The phone's location becomes the center of the safe zone. The collar
posts GPS fixes to the server. When the dog leaves the zone, the owner gets a
"Rex left home" push, and the app shows a red banner, a map with the dog's
trail, and walking directions. Another push follows when the dog is back.

Against GPS jitter, a fix only counts as outside if its whole accuracy circle is
outside the zone. It takes 2 fixes in a row to change state, and fixes less
accurate than 100 m are ignored. Fixes are kept for 24 hours.

No collar yet? Simulate one. It plays in the yard, runs off, then comes home:

```sh
go run ./cmd/gpssim -server https://canivo.onrender.com -dog 1
```

AirTags can't be used: Apple offers no way for other apps to read their
location. On Apple devices the app has an **Open Find My** button
(`findmy://items`) so an owner whose dog wears an AirTag can check it there.

## How detection works

1. **Envelope** – the Nano sends the EMG envelope (muscle activity level) at 50 Hz.
2. **Breaths** – each breath is a burst of respiratory-muscle activity. The
   server smooths the signal, subtracts a slow trend and counts rises above it
   (`internal/breath`). It reports breaths/minute from the last few breaths (within
   15 s) and average muscle tension over the last 6 s.
3. **Baseline** – every dog is different, so the first 2 minutes of data (dog
   calm!) become that dog's baseline. Re-run any time with `POST /calibrate`.
4. **Arousal score** – `0.5·|rate z-score| + 0.5·(tension z-score, increases only)`.
   Rate counts in both directions because alert dogs often freeze and hold their
   breath. Score ≥ 2.5 for 3 s → **alert** + push. Score < 1.5 for 10 s → calm.
   Pushes are rate-limited to one per dog every 2 minutes (`internal/monitor`).

**Speed.** A clear change in the dog alerts in about 6 s (a moderate one in
about 9 s), and the app shows it about 1 s later: the bridge sends every 200 ms
(`-send`), and the app's status request waits on the server for the next
change instead of polling. `TestAlertLatency` and `TestCalmDogNeverAlerts`
guard the speed vs. false-alarm trade-off; to go faster, lower `AlertSustain`
in `monitor.DefaultConfig()`.

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
  `mobile/.env.local` (`EXPO_PUBLIC_SERVER_URL=http://<ip>:8090`). Sign in or
  create an account; never put the `API_KEY` in the app.
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
| `API_KEY` | – | device key for the bridge/collar (`Authorization: Bearer <key>`), reaches every dog; unset = requests without a token get full access (local only) |
| `DATABASE_URL` | – | Postgres URL; unset = SQLite at `DB_PATH` |
| `DB_PATH` | `canivo.db` | SQLite file |
| `NOTIFIER` | `expo` | `log` to print instead of sending |
| `EXPO_ACCESS_TOKEN` | – | only if Expo push security is on |
| `PUSH_COOLDOWN` | `2m` | min time between pushes per dog |
| `VAPID_PUBLIC_KEY` / `VAPID_PRIVATE_KEY` | – | browser push; generate with `go run ./cmd/vapidkeys` |
| `VAPID_SUBJECT` | – | your email; required by Apple with VAPID keys |
| `GEMINI_API_KEY` | – | adds a Gemini-written calming tip to alert pushes ([get a key](https://aistudio.google.com/apikey)); unset = "Time to redirect …" |
| `GEMINI_MODEL` | `gemini-3.5-flash-lite` | |
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
5. **App:** create your account. Do this first: the first account created
   takes over any dogs made before accounts existed.

Free tier notes: Render sleeps after 15 min with no requests (a running bridge
keeps it awake; waking takes ~30 s). Supabase pauses a project after a week of
no database activity.

## Accounts

Owners sign in with email and password and see only their own dogs. Passwords
are stored as bcrypt hashes. A sign-in gives the app a random session token;
only its SHA-256 hash is stored.

- **Remember me** (on by default): the token is saved on the device and lasts
  90 days from the last time the app was used, so regular users never have to
  sign in again. Off: the token is only kept in memory (closing the app signs
  you out) and expires after 12 hours on the server.
- **Sign out** ends that device's session on the server and stops its push alerts.
- After 10 wrong passwords for an email, signing in to it is blocked for 15 minutes.
- **Devices** (bridge, GPS collar) still use the shared `API_KEY`, which can
  reach every dog. Keep it out of the app.

## API

`/api/*` routes need `Authorization: Bearer <token>`: a session token from
sign-in, or the `API_KEY` (devices). Only register and login are open.

| Method | Path | Body / notes |
|---|---|---|
| GET | `/health` | |
| POST | `/api/auth/register` | `{"email","password","remember"}` → `{"token","expires_at","user"}`; password 8–72 chars |
| POST | `/api/auth/login` | same body and response; 401 wrong email or password, 429 after too many tries |
| POST | `/api/auth/logout` | ends this token's session |
| GET | `/api/me` | the signed-in owner |
| POST | `/api/dogs` | `{"name":"Rex"}`, owned by the signed-in owner |
| GET | `/api/dogs`, `/api/dogs/{id}` | your dogs (all dogs with the API key); includes learned baseline. Another owner's dog is a 404 |
| POST | `/api/dogs/{id}/samples` | `{"samples":[{"t":<millis>,"v":<value>}]}` (≤ 5000) → status |
| GET | `/api/dogs/{id}/status` | `state` (`calibrating`/`calm`/`alert`), `rate_bpm`, `tension`, `score`, `calibration_progress`, `seq`. With `?after=<seq>&wait=10` it waits until there's a newer status (max 10 s) |
| POST | `/api/dogs/{id}/calibrate` | forget baseline, relearn from the next 2 min |
| GET | `/api/dogs/{id}/alerts?limit=50` | alert history |
| POST | `/api/dogs/{id}/push-tokens` | `{"token":"ExponentPushToken[...]"}` from the Expo app |
| DELETE | `/api/push-tokens/{token}` | e.g. on logout |
| GET | `/api/web-push/key` | `{"public_key":...}` VAPID key for `pushManager.subscribe` |
| POST | `/api/dogs/{id}/web-push` | a browser `PushSubscription.toJSON()` |
| POST | `/api/web-push/unsubscribe` | `{"endpoint":...}` |
| POST | `/api/dogs/{id}/locations` | `{"fixes":[{"t":<unix ms>,"lat":..,"lon":..,"accuracy_m":..}]}` (≤ 500; `t` optional = now) → location |
| GET | `/api/dogs/{id}/location?minutes=30` | `zone`, `last` fix, `distance_m`, `away`, `away_since`, `trail` |
| PUT | `/api/dogs/{id}/zone` | `{"lat":..,"lon":..,"radius_m":100}` (20–5000 m) |
| DELETE | `/api/dogs/{id}/zone` | turn the safe zone off |

Push payload `data`: `{"type":"alert","dog_id":1,"alert_id":7}`, or
`{"type":"away"|"home","dog_id":1}` for the safe zone.

## Layout

```
cmd/server      HTTP server
cmd/bridge      serial → HTTP forwarder (+ simulator)
cmd/vapidkeys   generates web push keys
cmd/gpssim      fake GPS collar for trying the safe zone
internal/breath breath detection from EMG envelope (+ synthetic signal)
internal/monitor baseline, arousal score, calm/alert state machine
internal/geo    distance + safe-zone fence (left / returned)
internal/store  SQLite/Postgres: dogs, baselines, alerts, zones, GPS fixes, push tokens
internal/notify Expo push + Web Push
internal/api    HTTP handlers
firmware/       Arduino Nano sketch
mobile/         Expo app: iOS/Android + web (Vercel)
```

## Not done yet

- **Password reset** – there's no email service yet, so a forgotten password can't be reset from the app.
- **Per-device keys** – every bridge/collar shares one `API_KEY` that reaches every dog. Fine for one household; real users each need their own device keys.
- **Wireless** – the Nano R3 has no radio. Options: HC-05/HM-10 Bluetooth module → phone, or swap to an ESP32 / Nano 33 IoT / Nano ESP32 that posts directly.
- **GPS collar hardware** – the server and app are ready, but no collar firmware yet. A dog that runs away is out of Wi-Fi range, so the collar needs cellular (e.g. an ESP32 board with a SIM7000G/SIM7080G LTE-M + GNSS modem and a data SIM). It just POSTs to `/api/dogs/{id}/locations` every few seconds.
- **Tuning on a real dog** – thresholds come from synthetic data. Exercise, heat-panting and barking also change breathing; expect to tune `monitor.DefaultConfig()`.
- Live state is in memory, so the server runs as a single instance.
