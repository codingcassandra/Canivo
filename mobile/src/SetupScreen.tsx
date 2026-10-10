import { useState } from 'react';
import { ActivityIndicator, KeyboardAvoidingView, Platform, Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { createClient, Dog } from './api';
import { Settings } from './settings';
import { colors, fonts, radius, shadow, tones } from './theme';
import { Button, Card, Field, Icon, T } from './ui';

type Props = {
  initialUrl: string;
  initialKey: string;
  onDone: (s: Settings) => void;
};

// Connect to the server, then pick an existing dog or add a new one.
export function SetupScreen({ initialUrl, initialKey, onDone }: Props) {
  const [url, setUrl] = useState(initialUrl);
  const [apiKey, setApiKey] = useState(initialKey);
  const [dogs, setDogs] = useState<Dog[] | null>(null);
  const [name, setName] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function run(fn: () => Promise<void>) {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const connect = () =>
    run(async () => {
      setDogs(await createClient(url.trim(), apiKey.trim()).listDogs());
    });

  const addDog = () =>
    run(async () => {
      const dog = await createClient(url.trim(), apiKey.trim()).createDog(name.trim());
      onDone({ serverUrl: url.trim(), apiKey: apiKey.trim(), dogId: dog.id });
    });

  const pick = (d: Dog) => onDone({ serverUrl: url.trim(), apiKey: apiKey.trim(), dogId: d.id });

  return (
    <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <ScrollView contentContainerStyle={styles.container} keyboardShouldPersistTaps="handled">
        <View style={styles.column}>
          <View style={styles.hero}>
            <View style={styles.logo}>
              <Icon name="paw" size={40} color={colors.primary} />
            </View>
            <T variant="title" style={{ fontSize: 40, marginTop: 14 }}>
              Canivo
            </T>
            <T style={styles.tagline}>Know when your dog is on alert, before they react.</T>
          </View>

          <Card>
            <T variant="heading">Connect</T>
            <Field
              label="Server"
              icon="server"
              value={url}
              onChangeText={(t) => {
                setUrl(t);
                setDogs(null);
              }}
              autoCapitalize="none"
              autoCorrect={false}
              keyboardType="url"
              placeholder="https://canivo.onrender.com"
            />
            <Field
              label="API key"
              icon="key-variant"
              value={apiKey}
              onChangeText={(t) => {
                setApiKey(t);
                setDogs(null);
              }}
              autoCapitalize="none"
              autoCorrect={false}
              secureTextEntry
              placeholder="From Render → Environment"
            />
            <Button title="Connect" icon="paw" onPress={connect} disabled={busy || !url.trim()} />
          </Card>

          {busy && <ActivityIndicator style={{ marginTop: 20 }} color={colors.primary} />}
          {error && (
            <View style={styles.error}>
              <Icon name="alert-circle" color={colors.error} />
              <T style={{ flex: 1, color: colors.error }}>{error}</T>
            </View>
          )}

          {dogs && (
            <View style={{ marginTop: 24 }}>
              {dogs.length > 0 && (
                <T variant="label" style={{ marginBottom: 10 }}>
                  CHOOSE YOUR DOG
                </T>
              )}
              {dogs.map((d) => (
                <Pressable key={d.id} onPress={() => pick(d)} style={({ pressed }) => [styles.dogRow, pressed && { opacity: 0.7 }]}>
                  <View style={styles.dogAvatar}>
                    <Icon name="dog" size={24} color={colors.primary} />
                  </View>
                  <View style={{ flex: 1 }}>
                    <T style={{ fontFamily: fonts.bold, fontSize: 18 }}>{d.name}</T>
                    <T variant="caption" style={{ color: d.baseline ? tones.calm.fg : colors.muted }}>
                      {d.baseline ? 'Calibrated' : 'Not calibrated yet'}
                    </T>
                  </View>
                  <Icon name="chevron-right" color={colors.faint} />
                </Pressable>
              ))}

              <Card style={{ marginTop: dogs.length ? 14 : 0 }}>
                <T variant="heading">{dogs.length ? 'Or add a dog' : 'Add your dog'}</T>
                <Field label="Name" icon="dog" value={name} onChangeText={setName} placeholder="Rex" />
                <Button title="Add dog" icon="plus" onPress={addDog} disabled={busy || !name.trim()} />
              </Card>
            </View>
          )}
        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: { padding: 20, paddingTop: 36, paddingBottom: 48 },
  column: { width: '100%', maxWidth: 480, alignSelf: 'center' },
  hero: { alignItems: 'center', marginBottom: 24 },
  logo: {
    width: 84,
    height: 84,
    borderRadius: 42,
    backgroundColor: colors.accent,
    alignItems: 'center',
    justifyContent: 'center',
  },
  tagline: { color: colors.muted, textAlign: 'center', marginTop: 6, fontSize: 17, maxWidth: 300 },
  error: {
    flexDirection: 'row',
    gap: 10,
    alignItems: 'center',
    backgroundColor: tones.alert.soft,
    borderRadius: radius.sm,
    padding: 12,
    marginTop: 16,
  },
  dogRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 14,
    backgroundColor: colors.card,
    borderRadius: radius.md,
    padding: 14,
    marginBottom: 10,
    ...shadow,
  },
  dogAvatar: {
    width: 46,
    height: 46,
    borderRadius: 23,
    backgroundColor: colors.accentSoft,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
