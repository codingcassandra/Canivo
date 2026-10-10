import { Linking, Platform } from 'react-native';

// Find My only exists on Apple devices.
export const hasFindMy = Platform.OS === 'ios';

// Opens Apple's Find My app on the Items tab, where AirTags are listed.
export function openFindMy(): Promise<void> {
  return Linking.openURL('findmy://items');
}
