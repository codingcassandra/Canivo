import type { State } from './api';

// Warm & friendly: cream paper, warm browns, soft pastel state colors.
export const colors = {
  bg: '#FBF5EC',
  card: '#FFFDF9',
  text: '#3B2F28',
  muted: '#8C7B6D',
  faint: '#C9BBAE',
  border: '#EFE4D6',
  primary: '#C8693F', // terracotta
  primaryPressed: '#B05A34',
  primarySoft: '#F7E1D4',
  error: '#C24A3A',
};

export type Tone = { fg: string; soft: string; glow: string; label: string };

// Colors per dog state. `glow` is the halo around the breathing ring.
export const tones: Record<State | 'offline', Tone> = {
  calm: { fg: '#5E9470', soft: '#E6F0E5', glow: '#A9CDB0', label: 'Calm' },
  calibrating: { fg: '#C98B2B', soft: '#FBEFD8', glow: '#F0CF8E', label: 'Learning' },
  alert: { fg: '#D9553F', soft: '#FCE4DC', glow: '#F2A48F', label: 'High alert' },
  offline: { fg: '#A39383', soft: '#F1EAE2', glow: '#DDD1C4', label: 'Offline' },
};

export const fonts = {
  regular: 'Nunito_400Regular',
  semibold: 'Nunito_600SemiBold',
  bold: 'Nunito_700Bold',
  black: 'Nunito_900Black',
};

export const radius = { sm: 12, md: 20, lg: 28, pill: 999 };

// Soft warm shadow for cards (iOS/web use shadow*, Android uses elevation).
export const shadow = {
  shadowColor: '#7A5236',
  shadowOpacity: 0.08,
  shadowRadius: 16,
  shadowOffset: { width: 0, height: 6 },
  elevation: 2,
};
