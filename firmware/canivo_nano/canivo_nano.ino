// Canivo - Arduino Nano R3 EMG reader.
//
// Sends one "millis,value" line every 20 ms (50 Hz) over USB serial at
// 115200 baud. The value is the EMG *envelope*: how active the muscles are,
// which rises with every breath.
//
// Wiring: sensor signal -> A0, sensor GND -> GND, sensor power -> 5V (or 3.3V,
// per your sensor's datasheet).
//
// Set SENSOR_GIVES_ENVELOPE to match your sensor:
//   1 = the sensor already outputs an envelope (e.g. MyoWare "ENV"/"SIG" pin,
//       most Grove/Gravity EMG boards). We just sample it.
//   0 = the sensor outputs raw EMG (e.g. MyoWare "RAW" pin). We sample at
//       1 kHz, rectify, and smooth it into an envelope here.

#define SENSOR_GIVES_ENVELOPE 1

const int EMG_PIN = A0;
const unsigned long SEND_EVERY_MS = 20;

unsigned long lastSend = 0;

#if !SENSOR_GIVES_ENVELOPE
float dcLevel = 512;   // slow average = signal center
float envelope = 0;    // smoothed rectified signal
unsigned long lastSampleUs = 0;
#endif

void setup() {
  Serial.begin(115200);
  Serial.println("# canivo nano ready");
}

void loop() {
  unsigned long now = millis();

#if SENSOR_GIVES_ENVELOPE
  if (now - lastSend >= SEND_EVERY_MS) {
    lastSend = now;
    Serial.print(now);
    Serial.print(',');
    Serial.println(analogRead(EMG_PIN));
  }
#else
  unsigned long us = micros();
  if (us - lastSampleUs >= 1000) {  // 1 kHz
    lastSampleUs = us;
    int raw = analogRead(EMG_PIN);
    dcLevel += 0.001 * (raw - dcLevel);           // ~1 s time constant
    envelope += 0.02 * (fabs(raw - dcLevel) - envelope);  // ~50 ms
  }
  if (now - lastSend >= SEND_EVERY_MS) {
    lastSend = now;
    Serial.print(now);
    Serial.print(',');
    Serial.println(envelope, 1);
  }
#endif
}
