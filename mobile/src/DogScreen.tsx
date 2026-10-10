import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { AppState, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { AlertRecord, createClient, Dog, Status } from './api';
import { confirm } from './confirm';
import { enablePush } from './push';
import { PushState } from './pushState';
import { Settings } from './settings';
import { Button } from './SetupScreen';
import { colors } from './theme';

const POLL_MS = 2000;
const OFFLINE_AFTER_MS = 10000;

type Props = {
  settings: Settings;
  onChangeDog: () => void;
};

export function DogScreen({ settings, onChangeDog }: Props) {
  const client = useMemo(
    () => createClient(settings.serverUrl, settings.apiKey),
    [settings.serverUrl, settings.apiKey],
  );
  const { dogId } = settings;

  const [dog, setDog] = useState<Dog | null>(null);
  const [status, setStatus] = useState<Status | null>(null);
  const [alerts, setAlerts] = useState<AlertRecord[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [push, setPush] = useState<PushState | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const lastState = useRef<string | null>(null);

  const refreshAlerts = useCallback(async () => {
    setAlerts(await client.alerts(dogId));
  }, [client, dogId]);

  const poll = useCallback(async () => {
    try {
      const s = await client.status(dogId);
      setStatus(s);
      setError(null);
      if (s.state !== lastState.current) {
        lastState.current = s.state;
        refreshAlerts().catch(() => {});
      }
    } catch (e) {
      setError((e as Error).message);
    }
  }, [client, dogId, refreshAlerts]);

  // Load the dog, then poll status while the app is in the foreground.
  useEffect(() => {
    client.getDog(dogId).then(setDog, (e) => setError(e.message));
    refreshAlerts().catch(() => {});
    poll();
    let timer: ReturnType<typeof setInterval> | null = setInterval(poll, POLL_MS);
    const sub = AppState.addEventListener('change', (st) => {
      if (st === 'active' && !timer) {
        poll();
        timer = setInterval(poll, POLL_MS);
      } else if (st !== 'active' && timer) {
        clearInterval(timer);
        timer = null;
      }
    });
    return () => {
      if (timer) clearInterval(timer);
      sub.remove();
    };
  }, [client, dogId, poll, refreshAlerts]);

  // Register this phone/browser for push alerts about this dog.
  const turnOnPush = useCallback(
    (userTapped: boolean) => enablePush(client, dogId, userTapped).then(setPush),
    [client, dogId],
  );
  useEffect(() => {
    turnOnPush(false);
  }, [turnOnPush]);

  const name = dog?.name ?? 'Your dog';

  const recalibrate = () =>
    confirm(
      `Recalibrate ${name}?`,
      `Make sure ${name} is calm and relaxed. The next 2 minutes become the new normal.`,
      'Recalibrate',
      () => client.recalibrate(dogId).then(setStatus, (e) => setError(e.message)),
    );

  const changeDog = async () => {
    // Stop getting this dog's alerts on this phone.
    if (push?.status === 'on') await push.unregister().catch(() => {});
    onChangeDog();
  };

  const onRefresh = async () => {
    setRefreshing(true);
    await Promise.all([poll(), refreshAlerts().catch(() => {})]);
    setRefreshing(false);
  };

  return (
    <ScrollView
      contentContainerStyle={styles.container}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
    >
      <Text style={styles.title}>{name}</Text>

      {error && <Text style={styles.error}>{error}</Text>}
      {status && <StatusCard name={name} status={status} />}
      {status?.baseline && status.state !== 'calibrating' && <Metrics status={status} />}

      <Text style={styles.label}>Recent alerts</Text>
      {alerts.length === 0 ? (
        <Text style={styles.muted}>No alerts yet.</Text>
      ) : (
        alerts.map((a) => <AlertRow key={a.id} alert={a} />)
      )}

      <Text style={styles.label}>Notifications</Text>
      {push?.status === 'needs-tap' ? (
        <Button title="Turn on alerts" onPress={() => turnOnPush(true)} />
      ) : (
        <Text style={styles.muted}>
          {push === null
            ? 'Setting up…'
            : push.status === 'on'
              ? `On. You’ll get a notification when ${name} goes on alert.`
              : push.reason}
        </Text>
      )}

      <Button title="Recalibrate" variant="secondary" onPress={recalibrate} />
      <Button title="Change dog or server" variant="secondary" onPress={changeDog} />
    </ScrollView>
  );
}

function StatusCard({ name, status }: { name: string; status: Status }) {
  const lastSeen = status.last_seen ? Date.parse(status.last_seen) : 0;
  const offline = !lastSeen || Date.now() - lastSeen > OFFLINE_AFTER_MS;

  let fg = colors.offline;
  let bg = colors.offlineBg;
  let headline = 'Sensor offline';
  let detail = lastSeen
    ? `No data since ${new Date(lastSeen).toLocaleTimeString()}. Check the harness and bridge.`
    : 'Waiting for the first readings from the harness.';

  if (!offline) {
    if (status.state === 'alert') {
      fg = colors.alert;
      bg = colors.alertBg;
      headline = 'High alert';
      detail = `Redirect ${name} now. Call their name, step between them and the trigger, or ask for a known cue.`;
    } else if (status.state === 'calibrating') {
      fg = colors.calibrating;
      bg = colors.calibratingBg;
      headline = 'Learning what calm looks like';
      detail = `Keep ${name} relaxed. ${Math.round(status.calibration_progress * 100)}% done.`;
    } else {
      fg = colors.calm;
      bg = colors.calmBg;
      headline = 'Calm';
      detail = `${name}'s breathing is normal.`;
    }
  }

  return (
    <View style={[styles.card, { backgroundColor: bg, borderColor: fg }]}>
      <Text style={[styles.headline, { color: fg }]}>{headline}</Text>
      <Text style={styles.detail}>{detail}</Text>
      {!offline && status.state === 'calibrating' && (
        <View style={styles.progressTrack}>
          <View style={[styles.progressFill, { width: `${status.calibration_progress * 100}%`, backgroundColor: fg }]} />
        </View>
      )}
    </View>
  );
}

function Metrics({ status }: { status: Status }) {
  const b = status.baseline!;
  const tensionPct = b.tension_mean > 0 ? Math.round((status.tension / b.tension_mean - 1) * 100) : 0;
  return (
    <View style={styles.metrics}>
      <Metric value={`${Math.round(status.rate_bpm)}`} unit="bpm" label={`Breathing · usually ${Math.round(b.rate_mean)}`} />
      <Metric value={`${tensionPct > 0 ? '+' : ''}${tensionPct}%`} label="Muscle tension vs normal" />
      <Metric value={status.score.toFixed(1)} label="Arousal · alert at 2.5" />
    </View>
  );
}

function Metric({ value, unit, label }: { value: string; unit?: string; label: string }) {
  return (
    <View style={styles.metric}>
      <Text style={styles.metricValue}>
        {value}
        {unit && <Text style={styles.metricUnit}> {unit}</Text>}
      </Text>
      <Text style={styles.metricLabel}>{label}</Text>
    </View>
  );
}

function AlertRow({ alert }: { alert: AlertRecord }) {
  const start = new Date(alert.started_at);
  const when = `${start.toLocaleDateString()} ${start.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}`;
  let duration = 'ongoing';
  if (alert.ended_at) {
    const secs = Math.round((Date.parse(alert.ended_at) - start.getTime()) / 1000);
    duration = secs < 60 ? `${secs}s` : `${Math.floor(secs / 60)}m ${secs % 60}s`;
  }
  return (
    <View style={styles.alertRow}>
      <Text style={styles.alertWhen}>{when}</Text>
      <Text style={[styles.muted, !alert.ended_at && { color: colors.alert, fontWeight: '600' }]}>{duration}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { padding: 24, paddingTop: 48, paddingBottom: 48 },
  title: { fontSize: 34, fontWeight: '700', color: colors.text, marginBottom: 16 },
  error: { color: colors.alert, marginBottom: 12, fontSize: 15 },
  muted: { color: colors.muted, fontSize: 15 },
  label: { fontSize: 13, fontWeight: '600', color: colors.muted, textTransform: 'uppercase', marginTop: 28, marginBottom: 8 },
  card: { borderWidth: 2, borderRadius: 16, padding: 20 },
  headline: { fontSize: 26, fontWeight: '700' },
  detail: { fontSize: 16, color: colors.text, marginTop: 6, lineHeight: 22 },
  progressTrack: { height: 8, borderRadius: 4, backgroundColor: '#fff', marginTop: 14, overflow: 'hidden' },
  progressFill: { height: 8, borderRadius: 4 },
  metrics: { flexDirection: 'row', gap: 8, marginTop: 12 },
  metric: { flex: 1, backgroundColor: colors.card, borderColor: colors.border, borderWidth: 1, borderRadius: 12, padding: 12 },
  metricValue: { fontSize: 22, fontWeight: '700', color: colors.text, fontVariant: ['tabular-nums'] },
  metricUnit: { fontSize: 13, fontWeight: '500', color: colors.muted },
  metricLabel: { fontSize: 12, color: colors.muted, marginTop: 4 },
  alertRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingVertical: 12,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },
  alertWhen: { fontSize: 15, color: colors.text },
});
