import { useState } from 'react';
import { Image, StyleSheet, Text, View } from 'react-native';
import Svg, { Circle, Polyline } from 'react-native-svg';
import type { Fix, Zone } from './api';
import { colors, fonts, tones } from './theme';
import { Icon } from './ui';

// A small static map: the safe zone, home, the dog and its recent trail,
// zoomed to fit. Built from raster tiles so it works the same on web and
// phones without a map SDK.

const TILE = 256;
const tileUrl = (z: number, x: number, y: number) => `https://tile.openstreetmap.org/${z}/${x}/${y}.png`;
const M_PER_DEG = 111195;

type Props = { zone: Zone | null; dog?: Fix; trail: Fix[]; away: boolean; height?: number };

// Web Mercator: lat/lon → world pixels at zoom z.
function project(lat: number, lon: number, z: number): [number, number] {
  const size = TILE * 2 ** z;
  const sin = Math.sin((lat * Math.PI) / 180);
  return [((lon + 180) / 360) * size, (0.5 - Math.log((1 + sin) / (1 - sin)) / (4 * Math.PI)) * size];
}

export function DogMap({ zone, dog, trail, away, height = 220 }: Props) {
  const [width, setWidth] = useState(0);

  // Everything that should be on screen.
  const pts: [number, number][] = trail.map((f) => [f.lat, f.lon]);
  if (dog) pts.push([dog.lat, dog.lon]);
  if (zone) {
    const dLat = zone.radius_m / M_PER_DEG;
    const dLon = zone.radius_m / (M_PER_DEG * Math.cos((zone.lat * Math.PI) / 180));
    pts.push([zone.lat + dLat, zone.lon], [zone.lat - dLat, zone.lon], [zone.lat, zone.lon + dLon], [zone.lat, zone.lon - dLon]);
  }

  let view: { z: number; left: number; top: number } | null = null;
  if (width > 0 && pts.length > 0) {
    const pad = 36;
    for (let z = 19; z >= 2; z--) {
      const xy = pts.map(([la, lo]) => project(la, lo, z));
      const xs = xy.map((p) => p[0]);
      const ys = xy.map((p) => p[1]);
      const [x0, x1, y0, y1] = [Math.min(...xs), Math.max(...xs), Math.min(...ys), Math.max(...ys)];
      if ((x1 - x0 <= width - 2 * pad && y1 - y0 <= height - 2 * pad) || z === 2) {
        view = { z, left: (x0 + x1) / 2 - width / 2, top: (y0 + y1) / 2 - height / 2 };
        break;
      }
    }
  }

  const screen = (lat: number, lon: number): [number, number] => {
    const [x, y] = project(lat, lon, view!.z);
    return [x - view!.left, y - view!.top];
  };

  const tiles: { key: string; uri: string; x: number; y: number }[] = [];
  if (view) {
    const n = 2 ** view.z;
    for (let tx = Math.floor(view.left / TILE); tx <= Math.floor((view.left + width) / TILE); tx++) {
      for (let ty = Math.floor(view.top / TILE); ty <= Math.floor((view.top + height) / TILE); ty++) {
        if (ty < 0 || ty >= n) continue;
        const wx = ((tx % n) + n) % n;
        tiles.push({ key: `${view.z}/${tx}/${ty}`, uri: tileUrl(view.z, wx, ty), x: tx * TILE - view.left, y: ty * TILE - view.top });
      }
    }
  }

  const dogColor = away ? tones.warning.fg : colors.primary;

  return (
    <View style={[styles.map, { height }]} onLayout={(e) => setWidth(e.nativeEvent.layout.width)}>
      {tiles.map((t) => (
        <Image key={t.key} source={{ uri: t.uri }} style={[styles.tile, { left: t.x, top: t.y }]} />
      ))}
      {view && (
        <Svg width={width} height={height} style={StyleSheet.absoluteFill}>
          {zone && (
            <Circle
              cx={screen(zone.lat, zone.lon)[0]}
              cy={screen(zone.lat, zone.lon)[1]}
              r={zone.radius_m / ((156543.03 * Math.cos((zone.lat * Math.PI) / 180)) / 2 ** view.z)}
              fill={colors.primary}
              fillOpacity={0.12}
              stroke={colors.primary}
              strokeWidth={2}
              strokeDasharray="6 5"
            />
          )}
          {trail.length > 1 && (
            <Polyline
              points={trail.map((f) => screen(f.lat, f.lon).join(',')).join(' ')}
              fill="none"
              stroke={dogColor}
              strokeOpacity={0.7}
              strokeWidth={3}
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          )}
        </Svg>
      )}
      {view && zone && <Marker at={screen(zone.lat, zone.lon)} icon="home" color={colors.text} bg={colors.accent} size={28} />}
      {view && dog && <Marker at={screen(dog.lat, dog.lon)} icon="paw" color="#fff" bg={dogColor} size={34} />}
      {!view && width > 0 && (
        <View style={[StyleSheet.absoluteFill, styles.empty]}>
          <Icon name="map-marker-question-outline" size={28} color={colors.faint} />
        </View>
      )}
      <Text style={styles.credit}>© OpenStreetMap</Text>
    </View>
  );
}

function Marker({ at, icon, color, bg, size }: { at: [number, number]; icon: 'home' | 'paw'; color: string; bg: string; size: number }) {
  return (
    <View
      style={[
        styles.marker,
        { left: at[0] - size / 2, top: at[1] - size / 2, width: size, height: size, borderRadius: size / 2, backgroundColor: bg },
      ]}
    >
      <Icon name={icon} size={size * 0.55} color={color} />
    </View>
  );
}

const styles = StyleSheet.create({
  map: { borderRadius: 16, overflow: 'hidden', backgroundColor: '#EEF0EA', position: 'relative' },
  tile: { position: 'absolute', width: TILE, height: TILE },
  marker: {
    position: 'absolute',
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: 3,
    borderColor: '#fff',
    shadowColor: '#000',
    shadowOpacity: 0.2,
    shadowRadius: 4,
    shadowOffset: { width: 0, height: 2 },
    elevation: 3,
  },
  empty: { alignItems: 'center', justifyContent: 'center' },
  credit: {
    position: 'absolute',
    right: 6,
    bottom: 4,
    fontSize: 10,
    fontFamily: fonts.semibold,
    color: colors.muted,
    backgroundColor: 'rgba(255,255,255,0.75)',
    paddingHorizontal: 4,
    borderRadius: 4,
  },
});
