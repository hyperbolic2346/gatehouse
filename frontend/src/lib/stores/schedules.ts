import { writable } from 'svelte/store';
import { api, type ScheduledHold } from '../api';

export const schedules = writable<ScheduledHold[]>([]);

/** Reload upcoming scheduled holds; failures leave the current list in place. */
export async function refreshSchedules(): Promise<void> {
	try {
		schedules.set(await api.getSchedules());
	} catch {
		// keep what we have
	}
}
