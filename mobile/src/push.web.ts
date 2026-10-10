/// <reference lib="dom" />
// Browser push (Web Push + service worker in public/sw.js). On iPhone this
// only works from the Home Screen app, and alerts then reach a paired Apple
// Watch too.
import { Client } from './api';
import { off, PushState } from './pushState';

const isIOS = () => /iPhone|iPad|iPod/.test(navigator.userAgent);
const isStandalone = () =>
  window.matchMedia('(display-mode: standalone)').matches || (navigator as { standalone?: boolean }).standalone === true;

export async function enablePush(client: Client, dogId: number, userTapped: boolean): Promise<PushState> {
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
    if (isIOS() && !isStandalone()) {
      return off('To get alerts on your iPhone and Apple Watch: tap Share → Add to Home Screen, then open Canivo from your Home Screen.');
    }
    return off('This browser doesn’t support push notifications.');
  }
  if (Notification.permission === 'denied') {
    return off('Notifications are blocked for this site. Allow them in your browser or iPhone Settings → Notifications → Canivo.');
  }
  if (Notification.permission === 'default') {
    if (!userTapped) return { status: 'needs-tap' };
    // Must be the first await after the tap, or iOS ignores the request.
    if ((await Notification.requestPermission()) !== 'granted') {
      return off('Notifications weren’t allowed.');
    }
  }

  try {
    const { public_key } = await client.webPushKey();
    const reg = await withTimeout(navigator.serviceWorker.ready, 10000, 'Service worker didn’t start. Reload the page.');
    const key = base64UrlToBytes(public_key);

    let sub = await reg.pushManager.getSubscription();
    if (sub && !sameKey(sub.options.applicationServerKey, key)) {
      await sub.unsubscribe(); // server's keys changed
      sub = null;
    }
    if (!sub) {
      sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
    }
    await client.addWebPush(dogId, sub.toJSON());
    const endpoint = sub.endpoint;
    return { status: 'on', unregister: () => client.removeWebPush(endpoint) };
  } catch (e) {
    return off(`Couldn’t turn on alerts: ${(e as Error).message}`);
  }
}

function base64UrlToBytes(s: string): Uint8Array<ArrayBuffer> {
  const b64 = (s + '='.repeat((4 - (s.length % 4)) % 4)).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(b64);
  const out = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

function sameKey(a: ArrayBuffer | null, b: Uint8Array): boolean {
  if (!a) return false;
  const x = new Uint8Array(a);
  return x.length === b.length && x.every((v, i) => v === b[i]);
}

function withTimeout<T>(p: Promise<T>, ms: number, msg: string): Promise<T> {
  return Promise.race([p, new Promise<T>((_, reject) => setTimeout(() => reject(new Error(msg)), ms))]);
}
