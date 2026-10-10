# Canivo

Canivo watches a dog's breathing through an EMG sensor and pushes a
notification to the owner's phone when the dog goes on high alert, so they can
redirect the dog before it reacts.

```
EMG sensor → Arduino Nano ──USB serial──▶ bridge ──HTTP──▶ server ──Expo push──▶ owner's phone
             (firmware/)                 (cmd/bridge)     (cmd/server)
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

## Configuration (server env vars)

| Var | Default | |
|---|---|---|
| `PORT` | `8090` | |
| `DB_PATH` | `canivo.db` | SQLite file |
| `NOTIFIER` | `expo` | `log` to print instead of sending |
| `EXPO_ACCESS_TOKEN` | – | only if Expo push security is on |
| `PUSH_COOLDOWN` | `2m` | min time between pushes per dog |

## API

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

Push payload `data`: `{"type":"alert","dog_id":1,"alert_id":7}`.

## Layout

```
cmd/server      HTTP server
cmd/bridge      serial → HTTP forwarder (+ simulator)
internal/breath breath detection from EMG envelope (+ synthetic signal)
internal/monitor baseline, arousal score, calm/alert state machine
internal/store  SQLite: dogs, baselines, alerts, push tokens
internal/notify Expo push
internal/api    HTTP handlers
firmware/       Arduino Nano sketch
```

## Not done yet

- **Auth** – any client can read/write any dog. Add owner accounts and a per-device ingest key before deploying publicly.
- **Wireless** – the Nano R3 has no radio. Options: HC-05/HM-10 Bluetooth module → phone, or swap to an ESP32 / Nano 33 IoT / Nano ESP32 that posts directly.
- **Tuning on a real dog** – thresholds come from synthetic data. Exercise, heat-panting and barking also change breathing; expect to tune `monitor.DefaultConfig()`.
- Live state is in memory, so the server runs as a single instance.
