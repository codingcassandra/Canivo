import * as Location from 'expo-location';

export type Here = { lat: number; lon: number; accuracy: number };

// The phone's current position, asking for permission if needed.
export async function getHere(): Promise<Here> {
  const { status } = await Location.requestForegroundPermissionsAsync();
  if (status !== 'granted') {
    throw new Error('Location permission is off. Turn it on for Canivo in Settings to set your home.');
  }
  const pos = await Location.getCurrentPositionAsync({ accuracy: Location.Accuracy.High });
  return { lat: pos.coords.latitude, lon: pos.coords.longitude, accuracy: pos.coords.accuracy ?? 0 };
}
