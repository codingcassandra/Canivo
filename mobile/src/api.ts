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

export class ApiError extends Error {}

export function createClient(serverUrl: string) {
  const base = serverUrl.replace(/\/+$/, '');

  async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 8000);
    let res: Response;
    try {
      res = await fetch(base + path, {
        method,
        headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: controller.signal,
      });
    } catch {
      throw new ApiError(`Can't reach the server at ${base}`);
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
    addPushToken: (id: number, token: string) => request<void>('POST', `/api/dogs/${id}/push-tokens`, { token }),
    removePushToken: (token: string) =>
      request<void>('DELETE', `/api/push-tokens/${encodeURIComponent(token)}`),
  };
}

export type Client = ReturnType<typeof createClient>;
