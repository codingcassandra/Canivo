import { useState } from 'react';
import { ActivityIndicator, Linking, Platform, Pressable, StyleSheet, Text, View } from 'react-native';
import type { Client, DogLocation, Fix } from './api';
import { confirm } from './confirm';
import { DogMap } from './DogMap';
import { getHere } from './here';
import { colors, fonts, radius, tones } from './theme';
import { Button, Icon, IconName, T } from './ui';
import { ago, fmtDistance, zoneSizes } from './units';

type Props = {
  client: Client;
  dogId: number;
  name: string;
  loc: DogLocation | null;
  onChange: (loc: DogLocation) => void;
};

// Safe zone around home + where the dog is now.
export function LocationCard({ client, dogId, name, loc, onChange }: Props) {
  const zone = loc?.zone ?? null;
  const [editing, setEditing] = useState(false);
  const [radiusM, setRadiusM] = useState(zone?.radius_m ?? zoneSizes[1]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const setHomeHere = async () => {
    setBusy(true);
    setError(null);
    try {
      const here = await getHere();
      onChange(await client.setZone(dogId, { lat: here.lat, lon: here.lon, radius_m: radiusM }));
      setEditing(false);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const removeZone = () =>
    confirm(`Turn off the safe zone?`, `You won’t be alerted if ${name} leaves home.`, 'Turn off', async () => {
      try {
        onChange(await client.deleteZone(dogId));
        setEditing(false);
      } catch (e) {
        setError((e as Error).message);
      }
    });

  if (loc === null) {
    return <ActivityIndicator color={colors.primary} />;
  }

  if (!zone || editing) {
    return (
      <View>
        <T style={{ color: colors.muted }}>
          {zone
            ? 'Stand at the center of home and tap below to move the safe zone.'
            : `Draw a safe zone around home and we’ll alert you the moment ${name} leaves it. Stand at home and tap below.`}
        </T>
        <T variant="label" style={{ marginTop: 16, marginBottom: 8 }}>
          ZONE SIZE
        </T>
        <View style={styles.chips}>
          {zoneSizes.map((r) => (
            <Pressable key={r} onPress={() => setRadiusM(r)} style={[styles.chip, Math.abs(r - radiusM) < 1 && styles.chipOn]}>
              <Text style={[styles.chipText, Math.abs(r - radiusM) < 1 && { color: '#fff' }]}>{fmtDistance(r)}</Text>
            </Pressable>
          ))}
        </View>
        <T variant="caption" style={{ marginTop: 6 }}>
          How far {name} can roam from where you’re standing.
        </T>
        {error && <T style={{ color: colors.error, marginTop: 10 }}>{error}</T>}
        <Button title={busy ? 'Finding you…' : 'Set home here'} icon="home-map-marker" onPress={setHomeHere} disabled={busy} />
        {zone && (
          <View style={styles.row}>
            <View style={{ flex: 1 }}>
              <Button title="Cancel" variant="ghost" onPress={() => setEditing(false)} />
            </View>
            <View style={{ flex: 1 }}>
              <Button title="Turn off" icon="map-marker-off" variant="ghost" onPress={removeZone} />
            </View>
          </View>
        )}
      </View>
    );
  }

  const last = loc.last;
  let icon: IconName = 'satellite-variant';
  let title = 'Waiting for GPS';
  let sub = `Once ${name}’s collar sends a location, it shows up here.`;
  let tint = tones.offline;
  if (last && loc.away) {
    icon = 'run-fast';
    title = `${name} is away`;
    sub = `${fmtDistance(loc.distance_m ?? 0)} from home · updated ${ago(last.at)}`;
    tint = tones.warning;
  } else if (last) {
    icon = 'home-heart';
    title = `${name} is home`;
    sub = `Inside the ${fmtDistance(zone.radius_m)} safe zone · updated ${ago(last.at)}`;
    tint = tones.calm;
  }

  return (
    <View>
      <View style={styles.statusRow}>
        <View style={[styles.statusIcon, { backgroundColor: tint.soft }]}>
          <Icon name={icon} size={20} color={tint.fg} />
        </View>
        <View style={{ flex: 1 }}>
          <T style={{ fontFamily: fonts.bold, color: loc.away ? tones.warning.fg : colors.text }}>{title}</T>
          <T variant="caption">{sub}</T>
        </View>
      </View>
      <DogMap zone={zone} dog={last} trail={loc.trail ?? []} away={loc.away} />
      {last && <Button title={`Directions to ${name}`} icon="directions" variant={loc.away ? 'primary' : 'soft'} onPress={() => openDirections(last)} />}
      <Button title="Change safe zone" icon="map-marker-radius" variant="ghost" onPress={() => setEditing(true)} />
    </View>
  );
}

// Walking directions in Apple Maps on iPhone, Google Maps elsewhere.
export function openDirections(f: Fix) {
  const ios =
    Platform.OS === 'ios' || (Platform.OS === 'web' && /iPhone|iPad|Macintosh/.test(globalThis.navigator?.userAgent ?? ''));
  const url = ios
    ? `https://maps.apple.com/?daddr=${f.lat},${f.lon}&dirflg=w`
    : `https://www.google.com/maps/dir/?api=1&destination=${f.lat},${f.lon}&travelmode=walking`;
  Linking.openURL(url);
}

const styles = StyleSheet.create({
  chips: { flexDirection: 'row', gap: 8, flexWrap: 'wrap' },
  chip: {
    paddingVertical: 8,
    paddingHorizontal: 14,
    borderRadius: radius.pill,
    backgroundColor: colors.primarySoft,
  },
  chipOn: { backgroundColor: colors.primary },
  chipText: { fontFamily: fonts.bold, fontSize: 15, color: colors.primary },
  row: { flexDirection: 'row', gap: 8 },
  statusRow: { flexDirection: 'row', alignItems: 'center', gap: 12, marginBottom: 12 },
  statusIcon: { width: 38, height: 38, borderRadius: 19, alignItems: 'center', justifyContent: 'center' },
});
