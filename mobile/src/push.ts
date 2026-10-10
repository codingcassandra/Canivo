import Constants from 'expo-constants';
import * as Device from 'expo-device';
import * as Notifications from 'expo-notifications';
import { Platform } from 'react-native';
import { Client } from './api';
import { off, PushState } from './pushState';

// Show alerts as banners even while the app is open.
Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldPlaySound: true,
    shouldSetBadge: false,
    shouldShowBanner: true,
    shouldShowList: true,
  }),
});

// Asks for permission, gets this phone's Expo push token and registers it with
// the server for this dog. (The browser version is push.web.ts.)
export async function enablePush(client: Client, dogId: number, _userTapped: boolean): Promise<PushState> {
  const r = await expoToken();
  if ('error' in r) return off(r.error);
  try {
    await client.addPushToken(dogId, r.token);
  } catch (e) {
    return off((e as Error).message);
  }
  return { status: 'on', unregister: () => client.removePushToken(r.token) };
}

async function expoToken(): Promise<{ token: string } | { error: string }> {
  if (!Device.isDevice) {
    return { error: 'Push needs a real phone, not a simulator.' };
  }
  if (Platform.OS === 'android' && Constants.executionEnvironment === 'storeClient') {
    return { error: 'Expo Go on Android can’t receive push. Use a development build or an iPhone.' };
  }

  if (Platform.OS === 'android') {
    await Notifications.setNotificationChannelAsync('alerts', {
      name: 'Dog alerts',
      importance: Notifications.AndroidImportance.MAX,
      vibrationPattern: [0, 250, 250, 250],
    });
  }

  const { status: existing } = await Notifications.getPermissionsAsync();
  let status = existing;
  if (existing !== 'granted') {
    status = (await Notifications.requestPermissionsAsync()).status;
  }
  if (status !== 'granted') {
    return { error: 'Notifications are turned off for this app in Settings.' };
  }

  const projectId = Constants.expoConfig?.extra?.eas?.projectId ?? Constants.easConfig?.projectId;
  if (!projectId) {
    return { error: 'No EAS project ID. Run `npx eas-cli@latest init` in mobile/.' };
  }

  try {
    const { data } = await Notifications.getExpoPushTokenAsync({ projectId });
    return { token: data };
  } catch (e) {
    return { error: `Couldn’t get a push token: ${(e as Error).message}` };
  }
}
