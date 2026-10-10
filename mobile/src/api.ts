// Client for the Canivo Go server. Types mirror internal/api and internal/store.

export type State = 'calibrating' | 'calm' | 'alert';

export type Baseline = {
  rate_mean: number;
  rate_sd: number;
  tension_mean: number;
  tension_sd: number;
  points: number;
};

export type Dog = {
  id: number;
  name: string;
  created_at: string;
  baseline?: Baseline;
};

export type Status = {
  dog_id: number;
  state: State;
  rate_bpm: number;
  tension: number;
  score: number;
  calibration_progress: number;
  baseline?: Baseline;
  alert_id?: number;
  last_seen?: string;
};

export type AlertRecord = {
  id: number;
  dog_id: number;
  started_at: string;
  ended_at?: string;
  score: number;
  rate_bpm: number;
  tension: number;
};

// GPS reading from the collar.
export type Fix = { at: string; lat: number; lon: number; accuracy_m: number };

// Safe zone: a circle around home.
export type Zone = { lat: number; lon: number; radius_m: number };

export type DogLocation = {
  dog_id: number;
  zone: Zone | null;
  last?: Fix;
  distance_m?: number; // from home
  away: boolean;
  away_since?: string;
  trail?: Fix[]; // oldest first
};

export class ApiError extends Error {}

export function createClient(serverUrl: string, apiKey = '') {
  // Accept "canivo.onrender.com" as well as the full "https://..." URL.
  const withScheme = /^https?:\/\//i.test(serverUrl) ? serverUrl : `https://${serverUrl}`;
  const base = withScheme.replace(/\/+$/, '');

  async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 8000);
    let res: Response;
    try {
      res = await fetch(base + path, {
        method,
        headers: {
          ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
          ...(apiKey ? { Authorization: `Bearer ${apiKey}` } : {}),
        },
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: controller.signal,
      });
    } catch {
      throw new ApiError(
        `Can't reach the server at ${base}. If it's on Render's free plan it may be waking up; try again in 30 seconds.`,
      );
    } finally {
      clearTimeout(timer);
    }
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      throw new ApiError(data?.error ?? `Server error ${res.status}`);
    }
    return res.status === 204 ? (undefined as T) : res.json();
  }

  return {
    listDogs: () => request<Dog[]>('GET', '/api/dogs'),
    createDog: (name: string) => request<Dog>('POST', '/api/dogs', { name }),
    getDog: (id: number) => request<Dog>('GET', `/api/dogs/${id}`),
    status: (id: number) => request<Status>('GET', `/api/dogs/${id}/status`),
    alerts: (id: number, limit = 20) => request<AlertRecord[]>('GET', `/api/dogs/${id}/alerts?limit=${limit}`),
    recalibrate: (id: number) => request<Status>('POST', `/api/dogs/${id}/calibrate`),
    location: (id: number, minutes = 30) => request<DogLocation>('GET', `/api/dogs/${id}/location?minutes=${minutes}`),
    setZone: (id: number, zone: Zone) => request<DogLocation>('PUT', `/api/dogs/${id}/zone`, zone),
    deleteZone: (id: number) => request<DogLocation>('DELETE', `/api/dogs/${id}/zone`),
    addPushToken: (id: number, token: string) => request<void>('POST', `/api/dogs/${id}/push-tokens`, { token }),
    removePushToken: (token: string) =>
      request<void>('DELETE', `/api/push-tokens/${encodeURIComponent(token)}`),
    webPushKey: () => request<{ public_key: string }>('GET', '/api/web-push/key'),
    addWebPush: (id: number, subscription: unknown) => request<void>('POST', `/api/dogs/${id}/web-push`, subscription),
    removeWebPush: (endpoint: string) => request<void>('POST', '/api/web-push/unsubscribe', { endpoint }),
  };
}

export type Client = ReturnType<typeof createClient>;
