import { Alert } from 'react-native';

// Native confirm dialog. (Alert.alert does nothing on web; see confirm.web.ts.)
export function confirm(title: string, message: string, action: string, onConfirm: () => void) {
  Alert.alert(title, message, [
    { text: 'Cancel', style: 'cancel' },
    { text: action, onPress: onConfirm },
  ]);
}
