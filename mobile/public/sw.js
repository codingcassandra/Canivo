// Canivo service worker: shows alert notifications sent by the server.
// On iPhone these also appear (and vibrate) on a paired Apple Watch.

self.addEventListener('push', (event) => {
  let msg = { title: 'Canivo', body: 'Your dog needs attention.' };
  try {
    msg = event.data.json();
  } catch {}
  event.waitUntil(
    self.registration.showNotification(msg.title, {
      body: msg.body,
      data: msg.data,
      icon: '/icon-192.png',
      // Each alert gets its own notification, so every one buzzes (iPhone
      // can silently replace one with the same tag). Location updates replace
      // each other.
      tag: msg.data?.type === 'alert' ? 'canivo-alert-' + msg.data.alert_id : 'canivo-location',
      renotify: true, // a replaced location update still buzzes
      vibrate: [300, 100, 300], // Android; iPhone/Watch use their own haptic
    }),
  );
});

// Tapping the notification opens (or focuses) the app.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((wins) => {
      for (const w of wins) {
        if ('focus' in w) return w.focus();
      }
      return self.clients.openWindow('/');
    }),
  );
});
