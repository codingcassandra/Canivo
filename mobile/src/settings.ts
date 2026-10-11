import AsyncStorage from '@react-native-async-storage/async-storage';

// A signed-in owner, plus the dog they're looking at (null = picking a dog).
export type Session = {
  serverUrl: string;
  token: string;
  email: string;
  remember: boolean;
  dogId: number | null;
};

const SESSION = 'canivo.session';
const LAST_SERVER = 'canivo.server';
const LEGACY = 'canivo.settings'; // before accounts: server URL + shared API key

// Only "remember me" sessions are saved; others live in memory and are gone
// when the app closes.
export async function loadSession(): Promise<Session | null> {
  try {
    const raw = await AsyncStorage.getItem(SESSION);
    return raw ? (JSON.parse(raw) as Session) : null;
  } catch {
    return null;
  }
}

export async function saveSession(s: Session | null): Promise<void> {
  try {
    if (s?.remember) {
      await AsyncStorage.setItem(SESSION, JSON.stringify(s));
    } else {
      await AsyncStorage.removeItem(SESSION);
    }
    if (s) await AsyncStorage.setItem(LAST_SERVER, s.serverUrl);
  } catch {}
}

// The server last signed in to, to pre-fill the sign-in screen.
export async function loadLastServer(): Promise<string | null> {
  try {
    const last = await AsyncStorage.getItem(LAST_SERVER);
    if (last) return last;
    const legacy = await AsyncStorage.getItem(LEGACY);
    if (legacy) {
      await AsyncStorage.removeItem(LEGACY); // drop the old saved API key
      return (JSON.parse(legacy) as { serverUrl?: string }).serverUrl ?? null;
    }
  } catch {}
  return null;
}
