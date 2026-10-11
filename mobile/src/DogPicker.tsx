import { useEffect, useState } from 'react';
import { ActivityIndicator, KeyboardAvoidingView, Platform, Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Client, Dog } from './api';
import { colors, fonts, radius, shadow, tones } from './theme';
import { Button, Card, Field, Icon, T } from './ui';

type Props = {
  client: Client;
  email: string;
  onPick: (dogId: number) => void;
  onSignOut: () => void;
};

// The signed-in owner's dogs: pick one, or add a new one.
export function DogPicker({ client, email, onPick, onSignOut }: Props) {
  const [dogs, setDogs] = useState<Dog[] | null>(null);
  const [name, setName] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    client.listDogs().then(setDogs, (e) => setError(e.message));
  }, [client]);

  const addDog = async () => {
    setBusy(true);
    setError(null);
    try {
      const dog = await client.createDog(name.trim());
      onPick(dog.id);
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
    }
  };

  return (
    <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <ScrollView contentContainerStyle={styles.container} keyboardShouldPersistTaps="handled">
        <View style={styles.column}>
          <View style={styles.header}>
            <View style={styles.logo}>
              <Icon name="paw" size={26} color={colors.primary} />
            </View>
            <View style={{ flex: 1 }}>
              <T variant="caption">Signed in as</T>
              <T style={{ fontFamily: fonts.bold }} numberOfLines={1}>
                {email}
              </T>
            </View>
          </View>

          <T variant="title" style={{ marginBottom: 16 }}>
            {dogs && dogs.length === 0 ? 'Add your dog' : 'Your dogs'}
          </T>

          {!dogs && !error && <ActivityIndicator style={{ marginTop: 20 }} color={colors.primary} />}
          {error && (
            <View style={styles.error}>
              <Icon name="alert-circle" color={colors.error} />
              <T style={{ flex: 1, color: colors.error }}>{error}</T>
            </View>
          )}

          {dogs?.map((d) => (
            <Pressable key={d.id} onPress={() => onPick(d.id)} style={({ pressed }) => [styles.dogRow, pressed && { opacity: 0.7 }]}>
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

          {dogs && (
            <Card style={{ marginTop: dogs.length ? 14 : 0 }}>
              <T variant="heading">{dogs.length ? 'Add another dog' : 'What’s your dog’s name?'}</T>
              <Field label="Name" icon="dog" value={name} onChangeText={setName} placeholder="Rex" onSubmitEditing={addDog} />
              <Button title="Add dog" icon="plus" onPress={addDog} disabled={busy || !name.trim()} />
            </Card>
          )}

          <View style={{ marginTop: 20 }}>
            <Button title="Sign out" icon="logout" variant="ghost" onPress={onSignOut} />
          </View>
        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: { padding: 20, paddingTop: 28, paddingBottom: 48 },
  column: { width: '100%', maxWidth: 480, alignSelf: 'center' },
  header: { flexDirection: 'row', alignItems: 'center', gap: 12, marginBottom: 20 },
  logo: {
    width: 48,
    height: 48,
    borderRadius: 24,
    backgroundColor: colors.accent,
    alignItems: 'center',
    justifyContent: 'center',
  },
  error: {
    flexDirection: 'row',
    gap: 10,
    alignItems: 'center',
    backgroundColor: tones.alert.soft,
    borderRadius: radius.sm,
    padding: 12,
    marginBottom: 14,
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
