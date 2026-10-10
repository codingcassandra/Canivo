/// <reference lib="dom" />
export function confirm(title: string, message: string, _action: string, onConfirm: () => void) {
  if (window.confirm(`${title}\n\n${message}`)) onConfirm();
}
