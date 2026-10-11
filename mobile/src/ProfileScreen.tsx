import { useState } from 'react';
import { KeyboardAvoidingView, Platform, ScrollView, StyleSheet, View } from 'react-native';
import { Client, Dog, Profile } from './api';
import { colors, radius, tones } from './theme';
import { BackHeader, Button, Field, Icon, IconName, T } from './ui';

type Props = {
  client: Client;
  dog: Dog;
  onBack: () => void;
  onSaved: (dog: Dog) => void;
};

type Key = keyof Profile;

const fields = (name: string): { key: Key; label: string; icon: IconName; placeholder: string; long?: boolean }[] => [
  { key: 'breed', label: 'Breed', icon: 'dog', placeholder: 'e.g. Border collie mix' },
  { key: 'age', label: 'Age', icon: 'cake-variant-outline', placeholder: 'e.g. 3 years' },
  { key: 'weight', label: 'Weight', icon: 'scale-bathroom', placeholder: 'e.g. 18 kg or 40 lb' },
  { key: 'diet', label: 'Food & diet', icon: 'food-drumstick-outline', placeholder: 'Food, treats, allergies', long: true },
  { key: 'health', label: 'Health', icon: 'medical-bag', placeholder: 'Conditions, medication', long: true },
  {
    key: 'triggers',
    label: `What sets ${name} off`,
    icon: 'alert-outline',
    placeholder: 'e.g. Bikes, the doorbell, other dogs on leash',
    long: true,
  },
  {
    key: 'calming',
    label: `What helps ${name} calm down`,
    icon: 'heart-outline',
    placeholder: 'e.g. Sniffing games, “find it”, a quiet spot',
    long: true,
  },
  { key: 'notes', label: 'Anything else', icon: 'note-text-outline', placeholder: 'Personality, routine, cues they know', long: true },
];

export function ProfileScreen({ client, dog, onBack, onSaved }: Props) {
  const [profile, setProfile] = useState<Profile>(dog.profile ?? {});
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      onSaved(await client.setProfile(dog.id, profile));
    } catch (e) {
      setError((e as Error).message);
      setSaving(false);
    }
  };

  return (
    <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <ScrollView contentContainerStyle={styles.container} keyboardShouldPersistTaps="handled">
        <View style={styles.column}>
          <BackHeader title={`About ${dog.name}`} onBack={onBack} />
          <T style={{ color: colors.muted }}>
            The more Canivo knows about {dog.name}, the more personal the chat answers and the calming tips in alerts.
            Everything is optional.
          </T>

          {fields(dog.name).map((f) => (
            <Field
              key={f.key}
              label={f.label}
              icon={f.icon}
              placeholder={f.placeholder}
              value={profile[f.key] ?? ''}
              onChangeText={(v) => setProfile((p) => ({ ...p, [f.key]: v }))}
              multiline={f.long}
              maxLength={1000}
            />
          ))}

          {error && (
            <View style={styles.error}>
              <Icon name="alert-circle" color={colors.error} />
              <T style={{ flex: 1, color: colors.error }}>{error}</T>
            </View>
          )}
          <Button title={saving ? 'Saving…' : 'Save'} icon="check" onPress={save} disabled={saving} />
        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: { padding: 20, paddingTop: 0, paddingBottom: 48 },
  column: { width: '100%', maxWidth: 520, alignSelf: 'center' },
  error: {
    flexDirection: 'row',
    gap: 10,
    alignItems: 'center',
    backgroundColor: tones.alert.soft,
    borderRadius: radius.sm,
    padding: 12,
    marginTop: 16,
  },
});
