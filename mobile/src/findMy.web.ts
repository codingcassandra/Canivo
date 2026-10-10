// Find My only exists on Apple devices (iPhone, iPad, Mac).
export const hasFindMy = /iPhone|iPad|Macintosh/.test(globalThis.navigator?.userAgent ?? '');

// Opens Apple's Find My app on the Items tab, where AirTags are listed.
// A custom URL scheme has to be opened in the same window, not a new tab.
export async function openFindMy(): Promise<void> {
  window.location.href = 'findmy://items';
}
