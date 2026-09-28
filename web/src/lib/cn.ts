import { clsx, type ClassValue } from 'clsx'

/** Compose class names (clsx). Later classes win only via Tailwind's own cascade. */
export function cn(...inputs: ClassValue[]): string {
  return clsx(inputs)
}
