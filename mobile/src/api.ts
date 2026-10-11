// Client for the Canivo Go server. Types mirror internal/api and internal/store.

export type State = 'calibrating' | 'calm' | 'alert' | 'danger';

export type Baseline = {
  rate_mean: number;
  rate_sd: number;
  tension_mean: number;
  tension_sd: number;
  points: number;
};

// What the owner tells us about their dog; personalizes chat answers and alert tips.
export type Profile = {
  breed?: string;
  age?: string;
  weight?: string;
  diet?: string;
  health?: string;
  triggers?: string;
  calming?: string;
  notes?: string;
};

export type Dog = {
  id: number;
  name: string;
  created_at: string;
  baseline?: Baseline;
  profile: Profile;
};

export type ChatMessage = { id: number; role: 'user' | 'assistant'; text: string; created_at: string };

export type Status = {
  dog_id: number;
  seq: number; // pass back to status(id, seq) to wait for the next change
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
  tip?: string; // calming tip sent with the notification
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

export type User = { id: number; email: string; created_at: string };

export type AuthResult = { token: string; expires_at: string; user: User };

export class ApiError extends Error {}

// Accept "canivo.onrender.com" as well as the full "https://..." URL.
export function normalizeServer(serverUrl: string) {
  const withScheme = /^https?:\/\//i.test(serverUrl) ? serverUrl : `https://${serverUrl}`;
  return withScheme.replace(/\/+$/, '');
}

// token is the session token from login/register. onSignedOut is called when
// the server says the session is no longer valid (expired or signed out).
export function createClient(serverUrl: string, token = '', onSignedOut?: () => void) {
  const base = normalizeServer(serverUrl);

  async function request<T>(method: string, path: string, body?: unknown, timeoutMs = 8000): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    let res: Response;
    try {
      res = await fetch(base + path, {
        method,
        headers: {
          ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
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
      if (res.status === 401 && token) onSignedOut?.();
      const data = await res.json().catch(() => null);
      throw new ApiError(data?.error ?? `Server error ${res.status}`);
    }
    return res.status === 204 ? (undefined as T) : res.json();
  }

  return {
    register: (email: string, password: string, remember: boolean) =>
      request<AuthResult>('POST', '/api/auth/register', { email, password, remember }),
    login: (email: string, password: string, remember: boolean) =>
      request<AuthResult>('POST', '/api/auth/login', { email, password, remember }),
    logout: () => request<void>('POST', '/api/auth/logout'),
    me: () => request<User>('GET', '/api/me'),
    listDogs: () => request<Dog[]>('GET', '/api/dogs'),
    createDog: (name: string) => request<Dog>('POST', '/api/dogs', { name }),
    getDog: (id: number) => request<Dog>('GET', `/api/dogs/${id}`),
    setProfile: (id: number, profile: Profile) => request<Dog>('PUT', `/api/dogs/${id}/profile`, profile),
    chatHistory: (id: number) =>
      request<{ enabled: boolean; messages: ChatMessage[] }>('GET', `/api/dogs/${id}/chat`),
    // Answers can take a while, so allow longer than other calls.
    chat: (id: number, text: string) =>
      request<{ messages: ChatMessage[] }>('POST', `/api/dogs/${id}/chat`, { text }, 50000),
    clearChat: (id: number) => request<void>('DELETE', `/api/dogs/${id}/chat`),
    // With `after` (the seq of the status you have), the server holds the
    // request until there's a newer status (or ~10 s pass), so changes show
    // up immediately without fast polling.
    status: (id: number, after?: number) =>
      after === undefined
        ? request<Status>('GET', `/api/dogs/${id}/status`)
        : request<Status>('GET', `/api/dogs/${id}/status?after=${after}&wait=10`, undefined, 20000),
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
