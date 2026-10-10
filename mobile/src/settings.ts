import AsyncStorage from '@react-native-async-storage/async-storage';

export type Settings = {
  serverUrl: string;
  dogId: number;
};

const KEY = 'canivo.settings';

export async function loadSettings(): Promise<Settings | null> {
  try {
    const raw = await AsyncStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as Settings) : null;
  } catch {
    return null;
  }
}

export async function saveSettings(s: Settings | null): Promise<void> {
  if (s) {
    await AsyncStorage.setItem(KEY, JSON.stringify(s));
  } else {
    await AsyncStorage.removeItem(KEY);
  }
}
