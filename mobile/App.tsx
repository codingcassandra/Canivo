import {
  Nunito_400Regular,
  Nunito_600SemiBold,
  Nunito_700Bold,
  Nunito_900Black,
  useFonts,
} from '@expo-google-fonts/nunito';
import { StatusBar } from 'expo-status-bar';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { SafeAreaProvider, SafeAreaView } from 'react-native-safe-area-context';
import { createClient } from './src/api';
import { DogPicker } from './src/DogPicker';
import { DogScreen } from './src/DogScreen';
import { LoginScreen } from './src/LoginScreen';
import { loadLastServer, loadSession, saveSession, Session } from './src/settings';
import { colors } from './src/theme';

// Pre-filled on the sign-in screen. Override with EXPO_PUBLIC_SERVER_URL (e.g. in mobile/.env.local).
const DEFAULT_SERVER = process.env.EXPO_PUBLIC_SERVER_URL ?? 'https://canivo.onrender.com';

export default function App() {
  const [fontsLoaded, fontError] = useFonts({ Nunito_400Regular, Nunito_600SemiBold, Nunito_700Bold, Nunito_900Black });
  const [loaded, setLoaded] = useState(false);
  const [session, setSession] = useState<Session | null>(null);
  const [server, setServer] = useState(DEFAULT_SERVER);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([loadSession(), loadLastServer()]).then(([s, last]) => {
      setSession(s);
      if (last) setServer(last);
      setLoaded(true);
    });
  }, []);

  const update = useCallback((s: Session | null) => {
    if (s) setServer(s.serverUrl);
    setSession(s);
    saveSession(s);
  }, []);

  // The server rejected our token (expired, or signed out elsewhere).
  const expired = useCallback(() => {
    setNotice('You’ve been signed out. Please sign in again.');
    update(null);
  }, [update]);

  const client = useMemo(
    () => (session ? createClient(session.serverUrl, session.token, expired) : null),
    [session?.serverUrl, session?.token, expired], // eslint-disable-line react-hooks/exhaustive-deps
  );

  const signOut = useCallback(async () => {
    await client?.logout().catch(() => {});
    setNotice(null);
    update(null);
  }, [client, update]);

  let screen;
  if (!loaded || !(fontsLoaded || fontError)) {
    screen = (
      <View style={styles.center}>
        <ActivityIndicator color={colors.primary} />
      </View>
    );
  } else if (!session || !client) {
    screen = (
      <LoginScreen
        initialServer={server}
        notice={notice}
        onSignedIn={(s) => {
          setNotice(null);
          update(s);
        }}
      />
    );
  } else if (session.dogId === null) {
    screen = (
      <DogPicker
        client={client}
        email={session.email}
        onPick={(dogId) => update({ ...session, dogId })}
        onSignOut={signOut}
      />
    );
  } else {
    screen = (
      <DogScreen
        key={`${session.serverUrl}#${session.dogId}`}
        client={client}
        dogId={session.dogId}
        onChangeDog={() => update({ ...session, dogId: null })}
        onSignOut={signOut}
      />
    );
  }

  return (
    <SafeAreaProvider>
      <SafeAreaView style={styles.root} edges={['top', 'left', 'right']}>
        <StatusBar style="dark" />
        {screen}
      </SafeAreaView>
    </SafeAreaProvider>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: colors.bg },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center' },
});
