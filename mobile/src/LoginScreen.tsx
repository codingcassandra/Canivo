import { useState } from 'react';
import { ActivityIndicator, KeyboardAvoidingView, Platform, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { createClient, normalizeServer } from './api';
import { Session } from './settings';
import { colors, fonts, radius, tones } from './theme';
import { Button, Card, Field, Icon, T } from './ui';

type Props = {
  initialServer: string;
  notice?: string | null; // e.g. "your session expired"
  onSignedIn: (s: Session) => void;
};

// Sign in or create an account.
export function LoginScreen({ initialServer, notice, onSignedIn }: Props) {
  const [mode, setMode] = useState<'signin' | 'signup'>('signin');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [remember, setRemember] = useState(true);
  const [server, setServer] = useState(initialServer);
  const [showServer, setShowServer] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const signup = mode === 'signup';
  const canSubmit = !busy && email.trim() !== '' && password !== '' && server.trim() !== '';

  const submit = async () => {
    if (!canSubmit) return;
    setBusy(true);
    setError(null);
    try {
      const client = createClient(server.trim());
      const res = signup
        ? await client.register(email.trim(), password, remember)
        : await client.login(email.trim(), password, remember);
      onSignedIn({ serverUrl: normalizeServer(server.trim()), token: res.token, email: res.user.email, remember, dogId: null });
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
    }
  };

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

          {notice && !error && (
            <View style={[styles.banner, { backgroundColor: colors.accentSoft }]}>
              <Icon name="information" color={colors.accentDeep} />
              <T style={{ flex: 1 }}>{notice}</T>
            </View>
          )}

          <Card>
            <View style={styles.tabs}>
              {(['signin', 'signup'] as const).map((m) => (
                <Pressable
                  key={m}
                  onPress={() => {
                    setMode(m);
                    setError(null);
                  }}
                  style={[styles.tab, mode === m && styles.tabOn]}
                >
                  <Text style={[styles.tabText, mode === m && { color: colors.text }]}>
                    {m === 'signin' ? 'Sign in' : 'Create account'}
                  </Text>
                </Pressable>
              ))}
            </View>

            <Field
              label="Email"
              icon="email-outline"
              value={email}
              onChangeText={setEmail}
              autoCapitalize="none"
              autoCorrect={false}
              keyboardType="email-address"
              autoComplete="email"
              textContentType="username"
              placeholder="you@example.com"
            />
            <Field
                label="Password"
                icon="lock-outline"
                value={password}
                onChangeText={setPassword}
                secureTextEntry={!showPassword}
                autoCapitalize="none"
                autoCorrect={false}
                autoComplete={signup ? 'new-password' : 'current-password'}
                textContentType={signup ? 'newPassword' : 'password'}
                placeholder={signup ? 'At least 8 characters' : 'Your password'}
                onSubmitEditing={submit}
                returnKeyType="go"
                right={
                  <Pressable
                    onPress={() => setShowPassword((v) => !v)}
                    hitSlop={10}
                    accessibilityLabel={showPassword ? 'Hide password' : 'Show password'}
                  >
                    <Icon name={showPassword ? 'eye-off-outline' : 'eye-outline'} color={colors.muted} />
                  </Pressable>
                }
              />

            <Pressable
              onPress={() => setRemember((v) => !v)}
              style={styles.remember}
              accessibilityRole="checkbox"
              accessibilityState={{ checked: remember }}
            >
              <Icon
                name={remember ? 'checkbox-marked' : 'checkbox-blank-outline'}
                size={24}
                color={remember ? colors.primary : colors.faint}
              />
              <View style={{ flex: 1 }}>
                <T style={{ fontFamily: fonts.semibold }}>Remember me</T>
                <T variant="caption">
                  {remember ? 'Stay signed in on this device.' : 'You’ll be signed out when you close the app.'}
                </T>
              </View>
            </Pressable>

            {error && (
              <View style={[styles.banner, { backgroundColor: tones.warning.soft, marginTop: 14 }]}>
                <Icon name="alert-circle" color={colors.error} />
                <T style={{ flex: 1, color: colors.error }}>{error}</T>
              </View>
            )}

            <Button
              title={busy ? (signup ? 'Creating account…' : 'Signing in…') : signup ? 'Create account' : 'Sign in'}
              icon={signup ? 'account-plus' : 'login'}
              onPress={submit}
              disabled={!canSubmit}
            />
            {busy && <ActivityIndicator style={{ marginTop: 12 }} color={colors.primary} />}
          </Card>

          {showServer ? (
            <Card style={{ marginTop: 14 }}>
              <Field
                label="Server"
                icon="server"
                value={server}
                onChangeText={setServer}
                autoCapitalize="none"
                autoCorrect={false}
                keyboardType="url"
                placeholder="https://canivo.onrender.com"
              />
            </Card>
          ) : (
            <Pressable onPress={() => setShowServer(true)} style={styles.serverLink}>
              <T variant="caption">Server: {server.replace(/^https?:\/\//, '')} · Change</T>
            </Pressable>
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
  banner: { flexDirection: 'row', gap: 10, alignItems: 'center', borderRadius: radius.sm, padding: 12, marginBottom: 14 },
  tabs: { flexDirection: 'row', backgroundColor: colors.bg, borderRadius: radius.pill, padding: 4 },
  tab: { flex: 1, paddingVertical: 10, borderRadius: radius.pill, alignItems: 'center' },
  tabOn: { backgroundColor: colors.card, shadowColor: '#2A3A66', shadowOpacity: 0.1, shadowRadius: 6, elevation: 1 },
  tabText: { fontFamily: fonts.bold, fontSize: 15, color: colors.muted },
  remember: { flexDirection: 'row', alignItems: 'center', gap: 12, marginTop: 18 },
  serverLink: { alignSelf: 'center', marginTop: 16, padding: 8 },
});
