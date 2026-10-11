// Shared building blocks for the warm & friendly look.
import { MaterialCommunityIcons } from '@expo/vector-icons';
import type { ComponentProps, ReactNode } from 'react';
import { Pressable, StyleSheet, Text, TextInput, TextInputProps, TextProps, View, ViewStyle } from 'react-native';
import { colors, fonts, radius, shadow } from './theme';

export type IconName = ComponentProps<typeof MaterialCommunityIcons>['name'];

export function Icon({ name, size = 20, color = colors.text }: { name: IconName; size?: number; color?: string }) {
  return <MaterialCommunityIcons name={name} size={size} color={color} />;
}

type Variant = 'title' | 'heading' | 'body' | 'label' | 'caption';

export function T({ variant = 'body', style, ...rest }: TextProps & { variant?: Variant }) {
  return <Text {...rest} style={[text[variant], style]} />;
}

export function Card({ children, style }: { children: ReactNode; style?: ViewStyle | ViewStyle[] }) {
  return <View style={[styles.card, style]}>{children}</View>;
}

export function Button({
  title,
  onPress,
  disabled,
  icon,
  variant = 'primary',
}: {
  title: string;
  onPress: () => void;
  disabled?: boolean;
  icon?: IconName;
  variant?: 'primary' | 'soft' | 'ghost';
}) {
  const fg = variant === 'primary' ? '#fff' : colors.primary;
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled}
      style={({ pressed }) => [
        styles.button,
        variant === 'primary' && { backgroundColor: pressed ? colors.primaryPressed : colors.primary },
        variant === 'soft' && { backgroundColor: colors.primarySoft },
        variant === 'ghost' && { backgroundColor: 'transparent' },
        (pressed && variant !== 'primary') && { opacity: 0.7 },
        disabled && { opacity: 0.45 },
      ]}
    >
      {icon && <Icon name={icon} size={20} color={fg} />}
      <Text style={[styles.buttonText, { color: fg }]}>{title}</Text>
    </Pressable>
  );
}

export function Field({
  label,
  icon,
  right,
  ...input
}: TextInputProps & { label: string; icon: IconName; right?: ReactNode }) {
  return (
    <View style={{ marginTop: 18 }}>
      <T variant="label" style={{ marginBottom: 8 }}>
        {label}
      </T>
      <View style={[styles.field, input.multiline && styles.fieldMultiline]}>
        <Icon name={icon} size={20} color={colors.faint} />
        <TextInput
          placeholderTextColor={colors.faint}
          {...input}
          style={[styles.input, input.multiline && styles.inputMultiline]}
        />
        {right}
      </View>
    </View>
  );
}

// Top bar for screens opened from the dog screen.
export function BackHeader({ title, onBack, right }: { title: string; onBack: () => void; right?: ReactNode }) {
  return (
    <View style={styles.backHeader}>
      <Pressable onPress={onBack} hitSlop={10} accessibilityLabel="Back" style={styles.backButton}>
        <Icon name="chevron-left" size={28} color={colors.primary} />
      </Pressable>
      <T variant="heading" style={{ flex: 1 }} numberOfLines={1}>
        {title}
      </T>
      {right}
    </View>
  );
}

export function Pill({ icon, text: label, color, bg }: { icon?: IconName; text: string; color: string; bg: string }) {
  return (
    <View style={[styles.pill, { backgroundColor: bg }]}>
      {icon && <Icon name={icon} size={14} color={color} />}
      <Text style={[styles.pillText, { color }]}>{label}</Text>
    </View>
  );
}

const text = StyleSheet.create({
  title: { fontFamily: fonts.black, fontSize: 34, color: colors.text, letterSpacing: -0.5 },
  heading: { fontFamily: fonts.bold, fontSize: 19, color: colors.text },
  body: { fontFamily: fonts.regular, fontSize: 16, color: colors.text, lineHeight: 23 },
  label: { fontFamily: fonts.bold, fontSize: 13, color: colors.muted, letterSpacing: 0.3 },
  caption: { fontFamily: fonts.semibold, fontSize: 13, color: colors.muted },
});

const styles = StyleSheet.create({
  card: { backgroundColor: colors.card, borderRadius: radius.md, padding: 18, ...shadow },
  button: {
    flexDirection: 'row',
    gap: 8,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: radius.pill,
    paddingVertical: 15,
    paddingHorizontal: 22,
    marginTop: 12,
  },
  buttonText: { fontFamily: fonts.bold, fontSize: 16 },
  field: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    backgroundColor: colors.card,
    borderRadius: radius.sm + 4,
    borderWidth: 1.5,
    borderColor: colors.border,
    paddingHorizontal: 14,
  },
  // outlineStyle: the rounded field shows focus; hide the browser's square outline on web.
  input: { flex: 1, fontFamily: fonts.semibold, fontSize: 16, color: colors.text, paddingVertical: 14, outlineWidth: 0 },
  fieldMultiline: { alignItems: 'flex-start', paddingTop: 14 },
  inputMultiline: { minHeight: 72, paddingTop: 0, textAlignVertical: 'top' },
  backHeader: { flexDirection: 'row', alignItems: 'center', gap: 6, paddingTop: 12, paddingBottom: 12 },
  backButton: { marginLeft: -8 },
  pill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    alignSelf: 'flex-start',
    borderRadius: radius.pill,
    paddingVertical: 5,
    paddingHorizontal: 11,
  },
  pillText: { fontFamily: fonts.bold, fontSize: 13 },
});
