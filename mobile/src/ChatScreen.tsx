import { useEffect, useRef, useState } from 'react';
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
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { ChatMessage, Client } from './api';
import { confirm } from './confirm';
import { colors, fonts, radius, shadow, tones } from './theme';
import { BackHeader, Icon, T } from './ui';

type Props = {
  client: Client;
  dogId: number;
  name: string;
  onBack: () => void;
  onEditProfile: () => void;
};

// Shown before the first question, as one-tap starters.
const starters = (name: string) => [
  `What are healthy snacks for ${name}?`,
  `How much should ${name} eat each day?`,
  `Games to tire ${name} out indoors`,
  `How can I help ${name} with stress?`,
];

export function ChatScreen({ client, dogId, name, onBack, onEditProfile }: Props) {
  const insets = useSafeAreaInsets();
  const [messages, setMessages] = useState<ChatMessage[] | null>(null);
  const [enabled, setEnabled] = useState(true);
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const scroll = useRef<ScrollView>(null);

  useEffect(() => {
    client.chatHistory(dogId).then(
      (h) => {
        setMessages(h.messages);
        setEnabled(h.enabled);
      },
      (e) => {
        setMessages([]);
        setError(e.message);
      },
    );
  }, [client, dogId]);

  const send = async (question: string) => {
    question = question.trim();
    if (!question || sending) return;
    // Show the question right away; the server returns it saved with the answer.
    const pending: ChatMessage = { id: -1, role: 'user', text: question, created_at: new Date().toISOString() };
    setMessages((m) => [...(m ?? []), pending]);
    setText('');
    setError(null);
    setSending(true);
    try {
      const res = await client.chat(dogId, question);
      setMessages((m) => [...(m ?? []).filter((x) => x !== pending), ...res.messages]);
    } catch (e) {
      setMessages((m) => (m ?? []).filter((x) => x !== pending));
      setText(question); // so they can try again
      setError((e as Error).message);
    } finally {
      setSending(false);
    }
  };

  const clear = () =>
    confirm('Clear this chat?', `This deletes the conversation about ${name} on all your devices.`, 'Clear', () =>
      client.clearChat(dogId).then(
        () => setMessages([]),
        (e) => setError(e.message),
      ),
    );

  const empty = messages !== null && messages.length === 0;

  return (
    <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <View style={styles.column}>
        <BackHeader
          title={`Ask about ${name}`}
          onBack={onBack}
          right={
            messages?.length ? (
              <Pressable onPress={clear} hitSlop={10} accessibilityLabel="Clear chat">
                <Icon name="delete-outline" size={22} color={colors.muted} />
              </Pressable>
            ) : null
          }
        />
      </View>

      <ScrollView
        ref={scroll}
        style={{ flex: 1 }}
        contentContainerStyle={[styles.column, styles.list]}
        keyboardShouldPersistTaps="handled"
        onContentSizeChange={() => scroll.current?.scrollToEnd({ animated: true })}
      >
        {messages === null && <ActivityIndicator color={colors.primary} style={{ marginTop: 40 }} />}

        {empty && (
          <View style={styles.intro}>
            <View style={styles.introIcon}>
              <Icon name="chat-question" size={28} color={colors.primary} />
            </View>
            <T variant="heading" style={{ textAlign: 'center' }}>
              Ask anything about {name}
            </T>
            <T style={{ color: colors.muted, textAlign: 'center', marginTop: 4 }}>
              Food, training, behavior, health. Answers are tailored to {name}’s profile.
            </T>
            <Pressable onPress={onEditProfile}>
              <Text style={styles.link}>Update {name}’s profile</Text>
            </Pressable>
            {enabled && (
              <View style={styles.starters}>
                {starters(name).map((s) => (
                  <Pressable
                    key={s}
                    onPress={() => send(s)}
                    style={({ pressed }) => [styles.starter, pressed && { opacity: 0.7 }]}
                  >
                    <Text style={styles.starterText}>{s}</Text>
                  </Pressable>
                ))}
              </View>
            )}
          </View>
        )}

        {messages?.map((m) => (
          <View key={m.id === -1 ? 'pending' : m.id} style={[styles.bubble, m.role === 'user' ? styles.mine : styles.theirs]}>
            <Text selectable style={[styles.bubbleText, m.role === 'user' && { color: '#fff' }]}>
              {m.text}
            </Text>
          </View>
        ))}

        {sending && (
          <View style={[styles.bubble, styles.theirs, styles.typing]}>
            <ActivityIndicator size="small" color={colors.muted} />
            <T variant="caption">Thinking…</T>
          </View>
        )}

        {error && (
          <View style={styles.error}>
            <Icon name="alert-circle" color={colors.error} />
            <T style={{ flex: 1, color: colors.error }}>{error}</T>
          </View>
        )}
      </ScrollView>

      <View style={[styles.composerWrap, { paddingBottom: Math.max(insets.bottom, 12) }]}>
        <View style={styles.column}>
          {enabled ? (
            <>
              <View style={styles.composer}>
                <TextInput
                  value={text}
                  onChangeText={setText}
                  placeholder={`Ask about ${name}…`}
                  placeholderTextColor={colors.faint}
                  multiline
                  maxLength={2000}
                  style={styles.input}
                  onKeyPress={(e) => {
                    // On the web, Enter sends and Shift+Enter adds a line.
                    const ev = e.nativeEvent as { key: string; shiftKey?: boolean };
                    if (Platform.OS === 'web' && ev.key === 'Enter' && !ev.shiftKey) {
                      (e as unknown as { preventDefault: () => void }).preventDefault();
                      send(text);
                    }
                  }}
                />
                <Pressable
                  onPress={() => send(text)}
                  disabled={!text.trim() || sending}
                  accessibilityLabel="Send"
                  style={({ pressed }) => [
                    styles.send,
                    (!text.trim() || sending) && { opacity: 0.4 },
                    pressed && { backgroundColor: colors.primaryPressed },
                  ]}
                >
                  <Icon name="arrow-up" size={22} color="#fff" />
                </Pressable>
              </View>
              <T variant="caption" style={styles.disclaimer}>
                AI answers can be wrong. For anything health-related, check with your vet.
              </T>
            </>
          ) : (
            <T style={{ color: colors.muted, textAlign: 'center' }}>
              The assistant isn’t turned on for this server yet.
            </T>
          )}
        </View>
      </View>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  column: { width: '100%', maxWidth: 520, alignSelf: 'center', paddingHorizontal: 20 },
  list: { paddingTop: 8, paddingBottom: 16, flexGrow: 1 },
  intro: { alignItems: 'center', paddingTop: 24 },
  introIcon: {
    width: 56,
    height: 56,
    borderRadius: 28,
    backgroundColor: colors.primarySoft,
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: 12,
  },
  link: { fontFamily: fonts.bold, fontSize: 15, color: colors.primary, marginTop: 10 },
  starters: { width: '100%', gap: 8, marginTop: 20 },
  starter: {
    backgroundColor: colors.card,
    borderRadius: radius.sm + 4,
    borderWidth: 1.5,
    borderColor: colors.border,
    paddingVertical: 12,
    paddingHorizontal: 14,
  },
  starterText: { fontFamily: fonts.semibold, fontSize: 15, color: colors.text },
  bubble: { maxWidth: '85%', borderRadius: radius.md, paddingVertical: 10, paddingHorizontal: 14, marginTop: 10 },
  mine: { alignSelf: 'flex-end', backgroundColor: colors.primary, borderBottomRightRadius: 6 },
  theirs: { alignSelf: 'flex-start', backgroundColor: colors.card, borderBottomLeftRadius: 6, ...shadow },
  bubbleText: { fontFamily: fonts.regular, fontSize: 16, lineHeight: 23, color: colors.text },
  typing: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  error: {
    flexDirection: 'row',
    gap: 10,
    alignItems: 'center',
    backgroundColor: tones.alert.soft,
    borderRadius: radius.sm,
    padding: 12,
    marginTop: 12,
  },
  composerWrap: { borderTopWidth: 1, borderTopColor: colors.border, backgroundColor: colors.bg, paddingTop: 10 },
  composer: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: 8,
    backgroundColor: colors.card,
    borderRadius: radius.lg,
    borderWidth: 1.5,
    borderColor: colors.border,
    paddingLeft: 16,
    padding: 6,
  },
  input: {
    flex: 1,
    fontFamily: fonts.semibold,
    fontSize: 16,
    color: colors.text,
    maxHeight: 120,
    paddingTop: 9,
    paddingBottom: 9,
  },
  send: {
    width: 38,
    height: 38,
    borderRadius: 19,
    backgroundColor: colors.primary,
    alignItems: 'center',
    justifyContent: 'center',
  },
  disclaimer: { textAlign: 'center', marginTop: 8, fontSize: 12 },
});
