import { StatusBar } from 'expo-status-bar';
import { useEffect, useState } from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { SafeAreaProvider, SafeAreaView } from 'react-native-safe-area-context';
import { DogScreen } from './src/DogScreen';
import { SetupScreen } from './src/SetupScreen';
import { loadSettings, saveSettings, Settings } from './src/settings';
import { colors } from './src/theme';

// Pre-filled on the setup screen. Override with EXPO_PUBLIC_SERVER_URL in mobile/.env.
const DEFAULT_SERVER = process.env.EXPO_PUBLIC_SERVER_URL ?? 'http://192.168.1.20:8090';
const DEFAULT_KEY = process.env.EXPO_PUBLIC_API_KEY ?? '';

export default function App() {
  const [loaded, setLoaded] = useState(false);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [last, setLast] = useState({ url: DEFAULT_SERVER, key: DEFAULT_KEY });

  useEffect(() => {
    loadSettings().then((s) => {
      setSettings(s);
      if (s) setLast({ url: s.serverUrl, key: s.apiKey ?? '' });
      setLoaded(true);
    });
  }, []);

  const update = (s: Settings | null) => {
    if (s) setLast({ url: s.serverUrl, key: s.apiKey });
    setSettings(s);
    saveSettings(s);
  };

  return (
    <SafeAreaProvider>
      <SafeAreaView style={styles.root} edges={['top', 'left', 'right']}>
        <StatusBar style="dark" />
        {!loaded ? (
          <View style={styles.center}>
            <ActivityIndicator />
          </View>
        ) : settings ? (
          <DogScreen key={`${settings.serverUrl}#${settings.dogId}`} settings={settings} onChangeDog={() => update(null)} />
        ) : (
          <SetupScreen initialUrl={last.url} initialKey={last.key} onDone={update} />
        )}
      </SafeAreaView>
    </SafeAreaProvider>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: colors.bg },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center' },
});
