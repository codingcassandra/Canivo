export type Here = { lat: number; lon: number; accuracy: number };

// The browser's current position. Safari asks for permission the first time.
export function getHere(): Promise<Here> {
  return new Promise((resolve, reject) => {
    if (!navigator.geolocation) {
      reject(new Error('This browser can’t share your location.'));
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (pos) => resolve({ lat: pos.coords.latitude, lon: pos.coords.longitude, accuracy: pos.coords.accuracy }),
      (err) =>
        reject(
          new Error(
            err.code === err.PERMISSION_DENIED
              ? 'Location is blocked. Allow it for this site (iPhone: Settings → Privacy → Location Services → Safari Websites).'
              : 'Couldn’t get your location. Try again outside or near a window.',
          ),
        ),
      { enableHighAccuracy: true, timeout: 20000, maximumAge: 0 },
    );
  });
}
