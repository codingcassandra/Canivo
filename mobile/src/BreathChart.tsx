import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import Svg, { Defs, Line, LinearGradient, Path, Rect, Stop } from 'react-native-svg';
import type { State } from './api';
import { colors, tones } from './theme';
import { T } from './ui';

export type Point = { t: number; rate: number; state: State };

const HEIGHT = 130;
const PAD_TOP = 14;
const PAD_BOTTOM = 8;

type Props = { points: Point[]; baseline?: number; windowMs: number };

// Breathing rate over the last few minutes, with the dog's usual rate as a
// dashed line and alert periods shaded.
export function BreathChart({ points, baseline, windowMs }: Props) {
  const [width, setWidth] = useState(0);
  const now = Date.now();
  const start = now - windowMs;

  const rates = points.map((p) => p.rate);
  if (baseline) rates.push(baseline);
  const lo = Math.max(0, Math.min(...rates, Infinity) - 6);
  const hi = Math.max(...rates, -Infinity, lo + 20) + 6;

  const x = (t: number) => ((t - start) / windowMs) * width;
  const y = (r: number) => PAD_TOP + (1 - (r - lo) / (hi - lo)) * (HEIGHT - PAD_TOP - PAD_BOTTOM);

  const line = smoothPath(points.map((p) => [x(p.t), y(p.rate)]));
  const area =
    points.length > 1 ? `${line} L ${x(points[points.length - 1].t)} ${HEIGHT} L ${x(points[0].t)} ${HEIGHT} Z` : '';

  return (
    <View>
      <View style={styles.legend}>
        <LegendItem color={colors.primary} label="breaths / min" />
        {baseline ? <LegendItem color={colors.accentDeep} label={`usual ${Math.round(baseline)}`} dashed /> : null}
        {points.some((p) => p.state === 'alert') ? <LegendItem color={tones.alert.glow} label="alert" block /> : null}
      </View>
      <View onLayout={(e) => setWidth(e.nativeEvent.layout.width)}>
        {width > 0 && (
          <Svg width={width} height={HEIGHT}>
            <Defs>
              <LinearGradient id="fill" x1="0" y1="0" x2="0" y2="1">
                <Stop offset="0" stopColor={colors.primary} stopOpacity={0.22} />
                <Stop offset="1" stopColor={colors.primary} stopOpacity={0} />
              </LinearGradient>
            </Defs>

            {alertSpans(points).map(([a, b], i) => (
              <Rect key={i} x={x(a)} y={0} width={Math.max(2, x(b) - x(a))} height={HEIGHT} rx={8} fill={tones.alert.soft} />
            ))}

            {baseline ? (
              <Line x1={0} x2={width} y1={y(baseline)} y2={y(baseline)} stroke={colors.accentDeep} strokeWidth={1.5} strokeDasharray="5 6" />
            ) : null}

            {area ? <Path d={area} fill="url(#fill)" /> : null}
            {points.length > 1 ? (
              <Path d={line} stroke={colors.primary} strokeWidth={3} fill="none" strokeLinecap="round" strokeLinejoin="round" />
            ) : null}
          </Svg>
        )}
        {points.length < 2 && (
          <View style={[StyleSheet.absoluteFill, styles.empty]}>
            <T variant="caption">The chart fills in as readings arrive.</T>
          </View>
        )}
      </View>
      <View style={styles.axis}>
        <T variant="caption">{Math.round(windowMs / 60000)} min ago</T>
        <T variant="caption">now</T>
      </View>
    </View>
  );
}

function LegendItem({ color, label, dashed, block }: { color: string; label: string; dashed?: boolean; block?: boolean }) {
  return (
    <View style={styles.legendItem}>
      <View
        style={
          block
            ? { width: 12, height: 12, borderRadius: 3, backgroundColor: color }
            : { width: 16, height: 0, borderTopWidth: 3, borderColor: color, borderStyle: dashed ? 'dashed' : 'solid' }
        }
      />
      <T variant="caption" style={{ fontSize: 12 }}>
        {label}
      </T>
    </View>
  );
}

// Curve through the points using midpoint quadratic segments (smooth, never overshoots much).
function smoothPath(pts: number[][]): string {
  if (pts.length === 0) return '';
  let d = `M ${pts[0][0]} ${pts[0][1]}`;
  for (let i = 1; i < pts.length - 1; i++) {
    const mx = (pts[i][0] + pts[i + 1][0]) / 2;
    const my = (pts[i][1] + pts[i + 1][1]) / 2;
    d += ` Q ${pts[i][0]} ${pts[i][1]} ${mx} ${my}`;
  }
  const last = pts[pts.length - 1];
  return d + ` L ${last[0]} ${last[1]}`;
}

function alertSpans(points: Point[]): [number, number][] {
  const spans: [number, number][] = [];
  let begin: number | null = null;
  points.forEach((p, i) => {
    if (p.state === 'alert' && begin === null) begin = p.t;
    const ends = p.state !== 'alert' || i === points.length - 1;
    if (begin !== null && ends) {
      spans.push([begin, p.t]);
      begin = null;
    }
  });
  return spans;
}

const styles = StyleSheet.create({
  empty: { alignItems: 'center', justifyContent: 'center' },
  legend: { flexDirection: 'row', flexWrap: 'wrap', gap: 14, marginBottom: 8 },
  legendItem: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  axis: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 6 },
});
