// Distances in feet/miles for US-style locales, meters/km elsewhere.
const locale = (() => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().locale;
  } catch {
    return 'en-US';
  }
})();
const imperial = /-(US|LR|MM)\b/i.test(locale) || locale === 'en';

export function fmtDistance(m: number): string {
  if (imperial) {
    const ft = m * 3.28084;
    if (ft < 1000) return `${Math.max(10, Math.round(ft / 10) * 10)} ft`;
    const mi = ft / 5280;
    return `${+mi.toFixed(mi < 1 ? 2 : 1)} mi`;
  }
  if (m < 1000) return `${Math.max(5, Math.round(m / 5) * 5)} m`;
  return `${(m / 1000).toFixed(1)} km`;
}

// Safe zone sizes to offer, in meters, round in the local units.
export const zoneSizes = imperial ? [150, 300, 600, 1320].map((ft) => ft / 3.28084) : [50, 100, 200, 400];

export function ago(iso: string): string {
  const mins = Math.round((Date.now() - Date.parse(iso)) / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins} min ago`;
  const h = Math.floor(mins / 60);
  return `${h} h ${mins % 60} min ago`;
}
