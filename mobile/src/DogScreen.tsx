import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { AppState, Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { AlertRecord, Baseline, createClient, Dog, DogLocation, Status } from './api';
import { BreathChart, Point } from './BreathChart';
import { BreathingRing } from './BreathingRing';
import { confirm } from './confirm';
import { LocationCard, openDirections } from './LocationCard';
import { enablePush } from './push';
import { PushState } from './pushState';
import { Settings } from './settings';
import { colors, fonts, radius, Tone, tones } from './theme';
import { Button, Card, Icon, IconName, Pill, T } from './ui';
import { ago, fmtDistance } from './units';

const POLL_MS = 2000;
const OFFLINE_AFTER_MS = 10000;
const CHART_WINDOW_MS = 5 * 60 * 1000;
const LOCATION_POLL_MS = 5000;

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
  const [history, setHistory] = useState<Point[]>([]);
  const [alerts, setAlerts] = useState<AlertRecord[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [push, setPush] = useState<PushState | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [loc, setLoc] = useState<DogLocation | null>(null);
  const lastState = useRef<string | null>(null);
  const lastLocPoll = useRef(0);

  const pollLocation = useCallback(async () => {
    lastLocPoll.current = Date.now();
    setLoc(await client.location(dogId));
  }, [client, dogId]);

  const refreshAlerts = useCallback(async () => {
    setAlerts(await client.alerts(dogId));
  }, [client, dogId]);

  const poll = useCallback(async () => {
    if (Date.now() - lastLocPoll.current >= LOCATION_POLL_MS) pollLocation().catch(() => {});
    try {
      const s = await client.status(dogId);
      setStatus(s);
      setError(null);
      if (!isOffline(s) && s.rate_bpm > 0) {
        const now = Date.now();
        setHistory((h) => [...h.filter((p) => p.t > now - CHART_WINDOW_MS), { t: now, rate: s.rate_bpm, state: s.state }]);
      }
      if (s.state !== lastState.current) {
        lastState.current = s.state;
        refreshAlerts().catch(() => {});
      }
    } catch (e) {
      setError((e as Error).message);
    }
  }, [client, dogId, refreshAlerts, pollLocation]);

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
      () => {
        setHistory([]);
        client.recalibrate(dogId).then(setStatus, (e) => setError(e.message));
      },
    );

  const changeDog = async () => {
    // Stop getting this dog's alerts on this phone.
    if (push?.status === 'on') await push.unregister().catch(() => {});
    onChangeDog();
  };

  const onRefresh = async () => {
    setRefreshing(true);
    await Promise.all([poll(), pollLocation().catch(() => {}), refreshAlerts().catch(() => {})]);
    setRefreshing(false);
  };

  const offline = !status || isOffline(status);
  const tone = offline ? tones.offline : tones[status.state];
  const baseline = status?.baseline;

  return (
    <ScrollView
      contentContainerStyle={styles.container}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={colors.primary} />}
    >
      <View style={styles.column}>
        <View style={styles.header}>
          <View style={{ flex: 1 }}>
            <T variant="caption">{greeting()}</T>
            <T variant="title">{name}</T>
          </View>
          <Pill
            icon={offline ? 'wifi-off' : 'circle'}
            text={offline ? 'Offline' : 'Live'}
            color={offline ? tones.offline.fg : tones.calm.fg}
            bg={offline ? tones.offline.soft : tones.calm.soft}
          />
        </View>

        {error && (
          <View style={styles.errorBanner}>
            <Icon name="alert-circle" color={colors.error} />
            <T style={{ flex: 1, color: colors.error }}>{error}</T>
          </View>
        )}

        {loc?.away && loc.last && (
          <View style={styles.awayBanner}>
            <View style={styles.awayTop}>
              <View style={styles.awayIcon}>
                <Icon name="run-fast" size={24} color={tones.alert.fg} />
              </View>
              <View style={{ flex: 1 }}>
                <T variant="heading" style={{ color: '#fff' }}>
                  {name} left home
                </T>
                <T style={{ color: '#fff', opacity: 0.92 }}>
                  {fmtDistance(loc.distance_m ?? 0)} away · left {loc.away_since ? ago(loc.away_since) : 'just now'}
                </T>
              </View>
            </View>
            <Pressable onPress={() => openDirections(loc.last!)} style={({ pressed }) => [styles.awayButton, pressed && { opacity: 0.85 }]}>
              <Icon name="directions" color={tones.alert.fg} />
              <Text style={styles.awayButtonText}>Get directions</Text>
            </Pressable>
          </View>
        )}

        <BreathingRing
          tone={tone}
          rateBpm={offline ? 0 : (status?.rate_bpm ?? 0)}
          value={offline || !status?.rate_bpm ? '–' : String(Math.round(status.rate_bpm))}
          unit={offline || !status?.rate_bpm ? undefined : 'bpm'}
          caption={tone.label}
        />

        {status && <Message name={name} status={status} offline={offline} tone={tone} />}

        {baseline && !offline && status.state !== 'calibrating' && (
          <View style={styles.metrics}>
            <Metric
              icon="lungs"
              tint={tones.calm}
              label="Breathing"
              value={`${Math.round(status.rate_bpm)}`}
              sub={`usually ${Math.round(baseline.rate_mean)}`}
            />
            <Metric
              icon="arm-flex"
              tint={{ ...tones.calibrating, fg: colors.accentDeep, soft: colors.accentSoft }}
              label="Tension"
              value={signedPct(status.tension, baseline.tension_mean)}
              sub="vs normal"
            />
            <Metric
              icon="gauge"
              tint={status.score >= 2.5 ? tones.alert : { ...tones.alert, fg: colors.primary, soft: colors.primarySoft }}
              label="Arousal"
              value={status.score.toFixed(1)}
              sub="alert at 2.5"
              meter={Math.min(1, status.score / 2.5)}
            />
          </View>
        )}

        <Card style={styles.section}>
          <SectionTitle icon="heart-pulse" title="Last 5 minutes" />
          <BreathChart points={history} baseline={baseline?.rate_mean} windowMs={CHART_WINDOW_MS} />
        </Card>

        <Card style={styles.section}>
          <SectionTitle icon="map-marker-radius" title="Location" />
          <LocationCard client={client} dogId={dogId} name={name} loc={loc} onChange={setLoc} />
        </Card>

        <Card style={styles.section}>
          <SectionTitle icon="clock-outline" title="Recent alerts" />
          {alerts.length === 0 ? (
            <View style={styles.emptyRow}>
              <Icon name="check-circle" color={tones.calm.fg} />
              <T style={{ color: colors.muted, flex: 1 }}>No alerts yet. {name} has been calm.</T>
            </View>
          ) : (
            alerts
              .slice(0, 8)
              .map((a, i) => (
                <AlertRow key={a.id} alert={a} baseline={baseline} last={i === Math.min(alerts.length, 8) - 1} />
              ))
          )}
        </Card>

        <Card style={styles.section}>
          <SectionTitle icon={push?.status === 'on' ? 'bell-ring' : 'bell-off-outline'} title="Notifications" />
          {push?.status === 'needs-tap' ? (
            <>
              <T style={{ color: colors.muted }}>Get a buzz on your phone (and Apple Watch) when {name} goes on alert or leaves home.</T>
              <Button title="Turn on alerts" icon="bell-ring" onPress={() => turnOnPush(true)} />
            </>
          ) : (
            <T style={{ color: colors.muted }}>
              {push === null
                ? 'Setting up…'
                : push.status === 'on'
                  ? `On. You’ll get a notification when ${name} goes on alert or leaves home.`
                  : push.reason}
            </T>
          )}
        </Card>

        <View style={styles.footer}>
          <Button title="Recalibrate" icon="refresh" variant="soft" onPress={recalibrate} />
          <Button title="Change dog or server" icon="swap-horizontal" variant="ghost" onPress={changeDog} />
        </View>
      </View>
    </ScrollView>
  );
}

function isOffline(s: Status) {
  const lastSeen = s.last_seen ? Date.parse(s.last_seen) : 0;
  return !lastSeen || Date.now() - lastSeen > OFFLINE_AFTER_MS;
}

function greeting() {
  const h = new Date().getHours();
  return h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening';
}

function signedPct(value: number, mean: number) {
  const pct = mean > 0 ? Math.round((value / mean - 1) * 100) : 0;
  return `${pct > 0 ? '+' : ''}${pct}%`;
}

function Message({ name, status, offline, tone }: { name: string; status: Status; offline: boolean; tone: Tone }) {
  let icon: IconName = 'paw';
  let title: string;
  let body: string;

  if (offline) {
    icon = 'wifi-off';
    title = 'Waiting for the harness';
    body = status.last_seen
      ? `No readings since ${new Date(status.last_seen).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}. Check the harness and bridge.`
      : `Once ${name}'s harness is on and sending data, you'll see their breathing here.`;
  } else if (status.state === 'alert') {
    icon = 'alert-circle';
    title = `Redirect ${name} now`;
    body = `Call ${name}'s name, step between them and the trigger, or ask for a cue they know well.`;
  } else if (status.state === 'calibrating') {
    icon = 'paw';
    title = `Getting to know ${name}`;
    body = `Keep ${name} relaxed for a couple of minutes while we learn what calm looks like.`;
  } else {
    title = `${name} is relaxed`;
    body = status.baseline
      ? `Breathing is right around their usual ${Math.round(status.baseline.rate_mean)} breaths per minute.`
      : 'Breathing looks normal.';
  }

  return (
    <View style={[styles.message, { backgroundColor: tone.soft }]}>
      <View style={[styles.messageIcon, { backgroundColor: colors.card }]}>
        <Icon name={icon} size={22} color={tone.fg} />
      </View>
      <View style={{ flex: 1 }}>
        <T variant="heading" style={{ color: tone.fg }}>
          {title}
        </T>
        <T style={{ marginTop: 2 }}>{body}</T>
        {!offline && status.state === 'calibrating' && (
          <View style={styles.track}>
            <View style={[styles.fill, { width: `${status.calibration_progress * 100}%`, backgroundColor: tone.fg }]} />
          </View>
        )}
      </View>
    </View>
  );
}

function Metric({
  icon,
  tint,
  label,
  value,
  sub,
  meter,
}: {
  icon: IconName;
  tint: Tone;
  label: string;
  value: string;
  sub: string;
  meter?: number;
}) {
  return (
    <Card style={styles.metric}>
      <View style={[styles.metricIcon, { backgroundColor: tint.soft }]}>
        <Icon name={icon} size={18} color={tint.fg} />
      </View>
      <T variant="caption" style={{ marginTop: 10 }}>
        {label}
      </T>
      <T style={styles.metricValue}>{value}</T>
      {meter !== undefined ? (
        <View style={[styles.track, { marginTop: 6, height: 6 }]}>
          <View style={[styles.fill, { width: `${meter * 100}%`, height: 6, backgroundColor: tint.fg }]} />
        </View>
      ) : null}
      <T variant="caption" style={{ marginTop: 4, fontSize: 12 }}>
        {sub}
      </T>
    </Card>
  );
}

function SectionTitle({ icon, title }: { icon: IconName; title: string }) {
  return (
    <View style={styles.sectionTitle}>
      <View style={styles.sectionIcon}>
        <Icon name={icon} size={17} color={colors.primary} />
      </View>
      <T variant="heading">{title}</T>
    </View>
  );
}

function AlertRow({ alert, baseline, last }: { alert: AlertRecord; baseline?: Baseline; last: boolean }) {
  const start = new Date(alert.started_at);
  const ongoing = !alert.ended_at;
  let duration = 'ongoing';
  if (alert.ended_at) {
    const secs = Math.round((Date.parse(alert.ended_at) - start.getTime()) / 1000);
    duration = secs < 60 ? `${secs}s` : `${Math.floor(secs / 60)}m ${secs % 60}s`;
  }
  return (
    <View style={[styles.alertRow, !last && styles.alertDivider]}>
      <View style={[styles.alertDot, { backgroundColor: tones.alert.soft }]}>
        <Icon name="paw" size={16} color={tones.alert.fg} />
      </View>
      <View style={{ flex: 1 }}>
        <T style={{ fontFamily: fonts.bold }}>{dayLabel(start)}</T>
        <T variant="caption">
          {start.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })} · {trigger(alert, baseline)}
        </T>
      </View>
      <Pill
        text={duration}
        color={ongoing ? tones.alert.fg : colors.muted}
        bg={ongoing ? tones.alert.soft : colors.bg}
      />
    </View>
  );
}

// What set the alert off: whichever reading was furthest from the dog's normal.
function trigger(a: AlertRecord, b?: Baseline) {
  const bpm = `${Math.round(a.rate_bpm)} bpm`;
  if (!b) return bpm;
  const zRate = Math.abs(a.rate_bpm - b.rate_mean) / b.rate_sd;
  const zTension = (a.tension - b.tension_mean) / b.tension_sd;
  return zTension > zRate ? `tension ${signedPct(a.tension, b.tension_mean)}` : bpm;
}

function dayLabel(d: Date) {
  const today = new Date();
  const yesterday = new Date(today.getTime() - 86400000);
  if (d.toDateString() === today.toDateString()) return 'Today';
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday';
  return d.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' });
}

const styles = StyleSheet.create({
  container: { padding: 20, paddingTop: 28, paddingBottom: 48 },
  column: { width: '100%', maxWidth: 520, alignSelf: 'center' },
  header: { flexDirection: 'row', alignItems: 'center', marginBottom: 4 },
  errorBanner: {
    flexDirection: 'row',
    gap: 10,
    alignItems: 'center',
    backgroundColor: tones.alert.soft,
    borderRadius: radius.sm,
    padding: 12,
    marginTop: 12,
  },
  message: { flexDirection: 'row', gap: 14, borderRadius: radius.md, padding: 16, marginTop: 4 },
  messageIcon: { width: 42, height: 42, borderRadius: 21, alignItems: 'center', justifyContent: 'center' },
  track: { height: 8, borderRadius: 4, backgroundColor: 'rgba(255,255,255,0.8)', marginTop: 12, overflow: 'hidden' },
  fill: { height: 8, borderRadius: 4 },
  metrics: { flexDirection: 'row', gap: 10, marginTop: 14 },
  metric: { flex: 1, padding: 14 },
  metricIcon: { width: 34, height: 34, borderRadius: 17, alignItems: 'center', justifyContent: 'center' },
  metricValue: { fontFamily: fonts.black, fontSize: 26, color: colors.text, fontVariant: ['tabular-nums'] },
  section: { marginTop: 14 },
  sectionIcon: {
    width: 30,
    height: 30,
    borderRadius: 15,
    backgroundColor: colors.accentSoft,
    alignItems: 'center',
    justifyContent: 'center',
  },
  sectionTitle: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 12 },
  emptyRow: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  alertRow: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 10 },
  alertDivider: { borderBottomWidth: 1, borderBottomColor: colors.border },
  alertDot: { width: 34, height: 34, borderRadius: 17, alignItems: 'center', justifyContent: 'center' },
  footer: { marginTop: 10 },
  awayBanner: { backgroundColor: tones.alert.fg, borderRadius: radius.md, padding: 16, marginTop: 12 },
  awayTop: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  awayIcon: { width: 44, height: 44, borderRadius: 22, backgroundColor: '#fff', alignItems: 'center', justifyContent: 'center' },
  awayButton: {
    marginTop: 14,
    backgroundColor: '#fff',
    borderRadius: radius.pill,
    paddingVertical: 12,
    flexDirection: 'row',
    gap: 8,
    alignItems: 'center',
    justifyContent: 'center',
  },
  awayButtonText: { fontFamily: fonts.bold, fontSize: 17, color: tones.alert.fg },
});
