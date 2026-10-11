import { useCallback, useEffect, useRef, useState } from 'react';
import { AppState, Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { AlertRecord, Client, Dog, DogLocation, Status } from './api';
import { BreathingRing } from './BreathingRing';
import { ChatScreen } from './ChatScreen';
import { confirm } from './confirm';
import { hasFindMy, openFindMy } from './findMy';
import { LocationCard, openDirections } from './LocationCard';
import { enablePush } from './push';
import { ProfileScreen } from './ProfileScreen';
import { PushState } from './pushState';
import { colors, fonts, radius, Tone, tones } from './theme';
import { Button, Card, Icon, IconName, Pill, T } from './ui';
import { ago, fmtDistance } from './units';

const RETRY_MS = 2000;
const OFFLINE_AFTER_MS = 10000;
const LOCATION_POLL_MS = 5000;
// The calming tip is written after the alert starts; check back for it this long.
const TIP_WAIT_MS = 30000;

type Props = {
  client: Client;
  dogId: number;
  onChangeDog: () => void;
  onSignOut: () => void;
};

export function DogScreen({ client, dogId, onChangeDog, onSignOut }: Props) {
  const [dog, setDog] = useState<Dog | null>(null);
  const [status, setStatus] = useState<Status | null>(null);
  const [alerts, setAlerts] = useState<AlertRecord[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [push, setPush] = useState<PushState | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [loc, setLoc] = useState<DogLocation | null>(null);
  const [page, setPage] = useState<'home' | 'chat' | 'profile'>('home');
  const lastState = useRef<string | null>(null);

  const pollLocation = useCallback(async () => {
    setLoc(await client.location(dogId));
  }, [client, dogId]);

  const refreshAlerts = useCallback(async () => {
    setAlerts(await client.alerts(dogId));
  }, [client, dogId]);

  const applyStatus = useCallback(
    (s: Status) => {
      setStatus(s);
      setError(null);
      if (s.state !== lastState.current) {
        lastState.current = s.state;
        refreshAlerts().catch(() => {});
      }
    },
    [refreshAlerts],
  );

  // Load the dog, then follow its status live while the app is in the
  // foreground: each request waits on the server for the next change, so a
  // new reading shows up as soon as the server has it.
  useEffect(() => {
    client.getDog(dogId).then(setDog, (e) => setError(e.message));
    refreshAlerts().catch(() => {});

    let stopped = false;
    let running = false;
    let active = AppState.currentState !== 'background';
    let seq: number | undefined;

    const follow = async () => {
      if (running) return;
      running = true;
      while (!stopped && active) {
        try {
          const s = await client.status(dogId, seq);
          if (stopped) break;
          seq = s.seq;
          applyStatus(s);
        } catch (e) {
          if (stopped) break;
          setError((e as Error).message);
          seq = undefined;
          await new Promise((r) => setTimeout(r, RETRY_MS));
        }
      }
      running = false;
    };

    const locate = () => {
      if (active) pollLocation().catch(() => {});
    };
    follow();
    locate();
    const locTimer = setInterval(locate, LOCATION_POLL_MS);

    const sub = AppState.addEventListener('change', (st) => {
      active = st === 'active';
      if (active) {
        seq = undefined; // catch up right away
        follow();
        locate();
      }
    });
    return () => {
      stopped = true;
      clearInterval(locTimer);
      sub.remove();
    };
  }, [client, dogId, applyStatus, pollLocation, refreshAlerts]);

  // The open alert, and its calming tip once the server has written it.
  const alerting = status?.state === 'alert' || status?.state === 'danger';
  const openAlert = alerting ? alerts.find((a) => a.id === status.alert_id) : undefined;
  const waitingForTip = alerting && !!status.alert_id && !openAlert?.tip;
  const tipSince = openAlert?.started_at;
  useEffect(() => {
    if (!waitingForTip) return;
    if (tipSince && Date.now() - Date.parse(tipSince) > TIP_WAIT_MS) return; // no tip coming
    const t = setTimeout(() => refreshAlerts().catch(() => {}), 3000);
    return () => clearTimeout(t);
  }, [waitingForTip, tipSince, alerts, refreshAlerts]);

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
        client.recalibrate(dogId).then(setStatus, (e) => setError(e.message));
      },
    );

  // Leaving this dog: stop getting its alerts on this phone.
  const changeDog = async () => {
    if (push?.status === 'on') await push.unregister().catch(() => {});
    onChangeDog();
  };

  const signOut = () =>
    confirm('Sign out?', `You won’t get alerts about ${name} on this device until you sign in again.`, 'Sign out', async () => {
      if (push?.status === 'on') await push.unregister().catch(() => {});
      onSignOut();
    });

  const onRefresh = async () => {
    setRefreshing(true);
    await Promise.all([
      client.status(dogId).then(applyStatus, (e) => setError(e.message)),
      pollLocation().catch(() => {}),
      refreshAlerts().catch(() => {}),
    ]);
    setRefreshing(false);
  };

  if (page === 'chat') {
    return (
      <ChatScreen
        client={client}
        dogId={dogId}
        name={name}
        onBack={() => setPage('home')}
        onEditProfile={() => setPage('profile')}
      />
    );
  }
  if (page === 'profile' && dog) {
    return (
      <ProfileScreen
        client={client}
        dog={dog}
        onBack={() => setPage('home')}
        onSaved={(d) => {
          setDog(d);
          setPage('home');
        }}
      />
    );
  }

  const offline = !status || isOffline(status);
  const tone = offline ? tones.offline : tones[status.state];

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
          caption={tone.label}
        />

        {status && <Message name={name} status={status} offline={offline} tone={tone} tip={openAlert?.tip} />}

        <Pressable onPress={() => setPage('chat')} style={({ pressed }) => [styles.askCard, pressed && { opacity: 0.85 }]}>
          <View style={styles.askIcon}>
            <Icon name="chat-question" size={24} color={colors.primary} />
          </View>
          <View style={{ flex: 1 }}>
            <T variant="heading" style={{ color: '#fff' }}>
              Ask about {name}
            </T>
            <T style={{ color: '#fff', opacity: 0.92 }}>Food ideas, training, health questions</T>
          </View>
          <Icon name="chevron-right" size={26} color="#fff" />
        </Pressable>

        {/* GPS tracking only shows once a GPS collar has sent a location, so
            the app never suggests it can track a dog it can't. */}
        {loc?.last && (
          <Card style={styles.section}>
            <SectionTitle icon="map-marker-radius" title="Location" />
            <LocationCard client={client} dogId={dogId} name={name} loc={loc} onChange={setLoc} />
          </Card>
        )}

        <Card style={styles.section}>
          <SectionTitle icon="map-search-outline" title="AirTag" />
          <T style={{ color: colors.muted }}>
            If {name} wears an AirTag, you can see where it is in Apple’s Find My app. Canivo can’t see AirTag
            locations.
          </T>
          {hasFindMy ? (
            <Button
              title="Open Find My"
              icon="open-in-new"
              variant="soft"
              onPress={() => openFindMy().catch(() => setError('Couldn’t open Find My. Open it from your Home Screen.'))}
            />
          ) : (
            <T variant="caption" style={{ marginTop: 8 }}>
              Find My is only on iPhone, iPad and Mac.
            </T>
          )}
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
                <AlertRow key={a.id} alert={a} last={i === Math.min(alerts.length, 8) - 1} />
              ))
          )}
        </Card>

        {dog && (
          <Card style={styles.section}>
            <SectionTitle icon="dog" title={`About ${name}`} />
            <T style={{ color: colors.muted }}>{profileSummary(dog)}</T>
            <Button title="Edit profile" icon="pencil" variant="soft" onPress={() => setPage('profile')} />
          </Card>
        )}

        <Card style={styles.section}>
          <SectionTitle icon={push?.status === 'on' ? 'bell-ring' : 'bell-off-outline'} title="Notifications" />
          {push?.status === 'needs-tap' ? (
            <>
              <T style={{ color: colors.muted }}>Get a buzz on your phone (and Apple Watch) when {name} goes on alert.</T>
              <Button title="Turn on alerts" icon="bell-ring" onPress={() => turnOnPush(true)} />
            </>
          ) : (
            <T style={{ color: colors.muted }}>
              {push === null
                ? 'Setting up…'
                : push.status === 'on'
                  ? `On. You’ll get a notification when ${name} goes on alert.`
                  : push.reason}
            </T>
          )}
        </Card>

        <View style={styles.footer}>
          <Button title="Recalibrate" icon="refresh" variant="soft" onPress={recalibrate} />
          <Button title="Switch dog" icon="swap-horizontal" variant="ghost" onPress={changeDog} />
          <Button title="Sign out" icon="logout" variant="ghost" onPress={signOut} />
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

// One line about the dog's profile, or a nudge to fill it in.
function profileSummary(dog: Dog) {
  const p = dog.profile ?? {};
  const basics = [p.breed, p.age, p.weight].filter(Boolean).join(' · ');
  if (!Object.values(p).some(Boolean)) {
    return `Tell Canivo about ${dog.name}’s breed, diet and what calms them, so chat answers and alert tips fit ${dog.name}.`;
  }
  return (basics ? basics + '. ' : '') + (p.calming ? `Calms down with: ${p.calming}` : 'Add what helps them calm down for better alert tips.');
}

function Message({
  name,
  status,
  offline,
  tone,
  tip,
}: {
  name: string;
  status: Status;
  offline: boolean;
  tone: Tone;
  tip?: string;
}) {
  let icon: IconName = 'paw';
  let title: string;
  let body: string;

  if (offline) {
    icon = 'wifi-off';
    title = 'Waiting for the harness';
    body = status.last_seen
      ? `No readings since ${new Date(status.last_seen).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}. Check the harness and bridge.`
      : `Once ${name}'s harness is on and sending data, you'll see their breathing here.`;
  } else if (status.state === 'danger') {
    icon = 'alert-octagon';
    title = `${name} is in danger`;
    body = tip ?? `Go to ${name} now. Move them somewhere calm and quiet, and call a vet if they don't settle.`;
  } else if (status.state === 'alert') {
    icon = 'alert-circle';
    title = `Redirect ${name} now`;
    body = tip ?? `Call ${name}'s name, step between them and the trigger, or ask for a cue they know well.`;
  } else if (status.state === 'calibrating') {
    icon = 'paw';
    title = `Getting to know ${name}`;
    body = `Keep ${name} relaxed for a couple of minutes while we learn what calm looks like.`;
  } else {
    title = `${name} is relaxed`;
    body = 'Breathing looks normal.';
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

function AlertRow({ alert, last }: { alert: AlertRecord; last: boolean }) {
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
          {start.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}
        </T>
        {alert.tip && (
          <T variant="caption" style={styles.alertTip}>
            Tip: {alert.tip}
          </T>
        )}
      </View>
      <Pill
        text={duration}
        color={ongoing ? tones.alert.fg : colors.muted}
        bg={ongoing ? tones.alert.soft : colors.bg}
      />
    </View>
  );
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
  alertTip: { color: colors.text, fontFamily: fonts.regular, marginTop: 2 },
  askCard: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    backgroundColor: colors.primary,
    borderRadius: radius.md,
    padding: 16,
    marginTop: 14,
  },
  askIcon: { width: 44, height: 44, borderRadius: 22, backgroundColor: '#fff', alignItems: 'center', justifyContent: 'center' },
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
