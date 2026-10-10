import { useState } from 'react';
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { createClient, Dog } from './api';
import { Settings } from './settings';
import { colors } from './theme';

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

  return (
    <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <ScrollView contentContainerStyle={styles.container} keyboardShouldPersistTaps="handled">
        <Text style={styles.title}>Canivo</Text>
        <Text style={styles.subtitle}>Know when your dog is on alert, before they react.</Text>

        <Text style={styles.label}>Server</Text>
        <TextInput
          style={styles.input}
          value={url}
          onChangeText={(t) => {
            setUrl(t);
            setDogs(null);
          }}
          autoCapitalize="none"
          autoCorrect={false}
          keyboardType="url"
          placeholder="http://192.168.1.20:8090"
        />
        <Text style={styles.hint}>
          Your deployed server, or your computer's local IP (not localhost: the phone is a different device).
        </Text>

        <Text style={styles.label}>API key</Text>
        <TextInput
          style={styles.input}
          value={apiKey}
          onChangeText={(t) => {
            setApiKey(t);
            setDogs(null);
          }}
          autoCapitalize="none"
          autoCorrect={false}
          secureTextEntry
          placeholder="Leave blank for a local server"
        />
        <Button title="Connect" onPress={connect} disabled={busy || !url.trim()} />

        {busy && <ActivityIndicator style={{ marginTop: 16 }} />}
        {error && <Text style={styles.error}>{error}</Text>}

        {dogs && (
          <>
            {dogs.length > 0 && <Text style={styles.label}>Choose your dog</Text>}
            {dogs.map((d) => (
              <Pressable
                key={d.id}
                style={({ pressed }) => [styles.dogRow, pressed && { opacity: 0.6 }]}
                onPress={() => onDone({ serverUrl: url.trim(), apiKey: apiKey.trim(), dogId: d.id })}
              >
                <Text style={styles.dogName}>{d.name}</Text>
                <Text style={styles.muted}>{d.baseline ? 'Calibrated' : 'Not calibrated'}</Text>
              </Pressable>
            ))}

            <Text style={styles.label}>{dogs.length ? 'Or add a dog' : 'Add your dog'}</Text>
            <TextInput style={styles.input} value={name} onChangeText={setName} placeholder="Dog's name" />
            <Button title="Add dog" onPress={addDog} disabled={busy || !name.trim()} />
          </>
        )}
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

export function Button({
  title,
  onPress,
  disabled,
  variant = 'primary',
}: {
  title: string;
  onPress: () => void;
  disabled?: boolean;
  variant?: 'primary' | 'secondary';
}) {
  const primary = variant === 'primary';
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled}
      style={({ pressed }) => [
        styles.button,
        primary ? { backgroundColor: colors.primary } : styles.buttonSecondary,
        (pressed || disabled) && { opacity: 0.5 },
      ]}
    >
      <Text style={[styles.buttonText, !primary && { color: colors.primary }]}>{title}</Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  container: { padding: 24, paddingTop: 48 },
  title: { fontSize: 34, fontWeight: '700', color: colors.text },
  subtitle: { fontSize: 16, color: colors.muted, marginTop: 4, marginBottom: 24 },
  label: { fontSize: 13, fontWeight: '600', color: colors.muted, textTransform: 'uppercase', marginTop: 24, marginBottom: 8 },
  input: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderWidth: 1,
    borderRadius: 10,
    padding: 14,
    fontSize: 16,
    color: colors.text,
  },
  hint: { fontSize: 13, color: colors.muted, marginTop: 6 },
  error: { color: colors.alert, marginTop: 16, fontSize: 15 },
  muted: { color: colors.muted },
  dogRow: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderWidth: 1,
    borderRadius: 10,
    padding: 16,
    marginBottom: 8,
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  dogName: { fontSize: 17, fontWeight: '600', color: colors.text },
  button: { borderRadius: 10, padding: 15, alignItems: 'center', marginTop: 12 },
  buttonSecondary: { borderWidth: 1, borderColor: colors.border, backgroundColor: colors.card },
  buttonText: { color: '#fff', fontSize: 16, fontWeight: '600' },
});
