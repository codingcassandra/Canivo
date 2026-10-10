export type PushState =
  | { status: 'on'; unregister: () => Promise<void> }
  | { status: 'off'; reason: string }
  | { status: 'needs-tap' }; // browser: permission can only be asked after a tap

export const off = (reason: string): PushState => ({ status: 'off', reason });
