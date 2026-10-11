import type { State } from './api';

// Warm & friendly in blue and yellow: friendly blue for actions, sunny yellow
// accents, on a soft butter background.
export const colors = {
  bg: '#FFFAEE',
  card: '#FFFFFF',
  text: '#1F2A44', // deep navy
  muted: '#6B7390',
  faint: '#B9BED0',
  border: '#ECE6D6',
  primary: '#2F6BDB', // blue
  primaryPressed: '#2457B8',
  primarySoft: '#E4ECFC',
  accent: '#F6C431', // sunny yellow
  accentDeep: '#B98A07', // yellow dark enough for text and thin lines
  accentSoft: '#FFF1C2',
  error: '#C8402F',
};

export type Tone = { fg: string; soft: string; glow: string; label: string };

// Colors per dog state. `glow` is the halo around the breathing ring.
export const tones: Record<State | 'offline' | 'warning', Tone> = {
  calm: { fg: '#2F6BDB', soft: '#E4ECFC', glow: '#9DBDF5', label: 'Calm' },
  calibrating: { fg: '#B98A07', soft: '#FFF1C2', glow: '#F8D96E', label: 'Learning' },
  // Orange for alert, red for danger: the same colors as the circles in the
  // push notification titles.
  alert: { fg: '#C75B0A', soft: '#FDE9D2', glow: '#F5B26B', label: 'High alert' },
  danger: { fg: '#A61B1B', soft: '#F9CFCB', glow: '#E0574D', label: 'Danger' },
  // Plain red for errors and "left home", which aren't a stress level.
  warning: { fg: '#D9483A', soft: '#FDE3DF', glow: '#F4A196', label: 'Warning' },
  offline: { fg: '#8E94A8', soft: '#EEEFF4', glow: '#D3D6E0', label: 'Offline' },
};

export const fonts = {
  regular: 'Nunito_400Regular',
  semibold: 'Nunito_600SemiBold',
  bold: 'Nunito_700Bold',
  black: 'Nunito_900Black',
};

export const radius = { sm: 12, md: 20, lg: 28, pill: 999 };

// Soft shadow for cards (iOS/web use shadow*, Android uses elevation).
export const shadow = {
  shadowColor: '#2A3A66',
  shadowOpacity: 0.08,
  shadowRadius: 16,
  shadowOffset: { width: 0, height: 6 },
  elevation: 2,
};
