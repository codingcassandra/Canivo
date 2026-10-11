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
      tag: 'canivo-' + (msg.data?.type === 'alert' || msg.data?.type === 'danger' ? 'alert' : 'location'), // a new one replaces the previous of its kind (danger replaces the alert it escalates)...
      renotify: true, // ...but still buzzes again
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
