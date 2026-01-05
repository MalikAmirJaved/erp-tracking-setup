// src/lib/utils.js
import { clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * cn - Class Name utility
 * Combines clsx (for conditional classes) with tailwind-merge (to smartly merge Tailwind classes)
 * Prevents conflicting Tailwind classes (e.g., text-sm + text-lg → keeps the last one)
 */
export function cn(...inputs) {
  return twMerge(clsx(inputs));
}