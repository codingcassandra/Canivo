import { useEffect, useRef } from 'react';
import { Animated, Easing, Platform, StyleSheet, Text, View } from 'react-native';
import { colors, fonts, Tone } from './theme';

const SIZE = 210;
const useNativeDriver = Platform.OS !== 'web';

type Props = {
  tone: Tone;
  rateBpm: number; // 0 = unknown: falls back to a slow idle pulse
  value: string;
  unit?: string;
  caption: string;
};

// A glowing ring that expands on each inhale at the dog's real breathing rate.
export function BreathingRing({ tone, rateBpm, value, unit, caption }: Props) {
  const breath = useRef(new Animated.Value(0)).current;
  // Round so small rate wobbles between polls don't restart the animation.
  const periodMs = rateBpm > 3 ? Math.round(60000 / Math.round(rateBpm)) : 5000;

  useEffect(() => {
    const inhale = periodMs * 0.4;
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(breath, { toValue: 1, duration: inhale, easing: Easing.out(Easing.sin), useNativeDriver }),
        Animated.timing(breath, { toValue: 0, duration: periodMs - inhale, easing: Easing.inOut(Easing.sin), useNativeDriver }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [breath, periodMs]);

  const halo = (from: number, to: number, opacity: [number, number]) => ({
    transform: [{ scale: breath.interpolate({ inputRange: [0, 1], outputRange: [from, to] }) }],
    opacity: breath.interpolate({ inputRange: [0, 1], outputRange: opacity }),
  });

  return (
    <View style={styles.wrap} accessibilityLabel={`${value} ${unit ?? ''}, ${caption}`}>
      <Animated.View style={[styles.circle, { backgroundColor: tone.glow }, halo(1.0, 1.22, [0.25, 0.08])]} />
      <Animated.View style={[styles.circle, { backgroundColor: tone.glow }, halo(0.94, 1.08, [0.45, 0.25])]} />
      <Animated.View
        style={[
          styles.circle,
          styles.core,
          { borderColor: tone.fg },
          { transform: [{ scale: breath.interpolate({ inputRange: [0, 1], outputRange: [0.92, 0.97] }) }] },
        ]}
      >
        <Text style={[styles.value, { color: colors.text }]}>
          {value}
          {unit ? <Text style={styles.unit}> {unit}</Text> : null}
        </Text>
        <View style={[styles.badge, { backgroundColor: tone.soft }]}>
          <Text style={[styles.badgeText, { color: tone.fg }]}>{caption}</Text>
        </View>
      </Animated.View>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { width: SIZE * 1.3, height: SIZE * 1.3, alignItems: 'center', justifyContent: 'center', alignSelf: 'center' },
  circle: { position: 'absolute', width: SIZE, height: SIZE, borderRadius: SIZE / 2 },
  core: { backgroundColor: colors.card, borderWidth: 6, alignItems: 'center', justifyContent: 'center' },
  value: { fontFamily: fonts.black, fontSize: 56, letterSpacing: -1, fontVariant: ['tabular-nums'] },
  unit: { fontFamily: fonts.bold, fontSize: 18, color: colors.muted, letterSpacing: 0 },
  badge: { marginTop: 4, borderRadius: 999, paddingVertical: 4, paddingHorizontal: 12 },
  badgeText: { fontFamily: fonts.bold, fontSize: 14 },
});
