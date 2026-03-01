import { writable, get } from 'svelte/store';
import { api, type Event } from '../api';

export const events = writable<Event[]>([]);
export const selectedDate = writable<string>(todayString());
export const selectedCamera = writable<string>('');
export const eventsLoading = writable(false);

function todayString(): string {
	const d = new Date();
	return d.getFullYear().toString() +
		(d.getMonth() + 1).toString().padStart(2, '0') +
		d.getDate().toString().padStart(2, '0');
}

export async function fetchEvents() {
	eventsLoading.set(true);
	try {
		const date = get(selectedDate);
		const camera = get(selectedCamera);
		const result = await api.getEvents(date, camera || undefined);
		events.set(result || []);
	} catch {
		events.set([]);
	} finally {
		eventsLoading.set(false);
	}
}

export const eventsStore = {
	addEvent(event: Event) {
		const date = get(selectedDate);
		const eventDate = formatUnixDate(event.start_time);
		if (eventDate === date) {
			events.update((list) => [event, ...list]);
		}
	},

	updateEvent(event: Event) {
		events.update((list) =>
			list.map((e) => (e.id === event.id ? { ...e, ...event } : e))
		);
	},

	handleDayRollover(date: string) {
		selectedDate.set(date);
		fetchEvents();
	}
};

function formatUnixDate(ts: number): string {
	const d = new Date(ts * 1000);
	return d.getFullYear().toString() +
		(d.getMonth() + 1).toString().padStart(2, '0') +
		d.getDate().toString().padStart(2, '0');
}
