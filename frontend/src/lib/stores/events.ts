import { writable, get } from 'svelte/store';
import { api, type Event } from '../api';
import { user, visibleCameras } from './auth';

export const events = writable<Event[]>([]);
export const selectedDate = writable<string>(toDateString(new Date()));
export const selectedCamera = writable<string>('');
export const eventsLoading = writable(false);
export const eventsError = writable<string | null>(null);

/** Local date as YYYYMMDD, the form the events API takes. */
export function toDateString(d: Date): string {
	return d.getFullYear().toString() +
		(d.getMonth() + 1).toString().padStart(2, '0') +
		d.getDate().toString().padStart(2, '0');
}

/** Parse YYYYMMDD as a local date. */
export function fromDateString(s: string): Date {
	return new Date(Number(s.slice(0, 4)), Number(s.slice(4, 6)) - 1, Number(s.slice(6, 8)));
}

export async function fetchEvents() {
	eventsLoading.set(true);
	eventsError.set(null);
	try {
		const date = get(selectedDate);
		const camera = get(selectedCamera);
		const result = await api.getEvents(date, camera || undefined);
		events.set(result || []);
	} catch (err) {
		events.set([]);
		eventsError.set(err instanceof Error ? err.message : 'Failed to load events');
	} finally {
		eventsLoading.set(false);
	}
}

export const eventsStore = {
	addEvent(event: Event) {
		// Ignore live events for cameras the user has hidden or cannot view.
		if (!visibleCameras(get(user)).includes(event.camera)) {
			return;
		}
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

	/** At midnight, follow along to the new day if the viewer was on today;
	 * leave someone browsing an older day where they are. */
	handleDayRollover() {
		const today = toDateString(new Date());
		const yesterday = new Date();
		yesterday.setDate(yesterday.getDate() - 1);
		if (get(selectedDate) === toDateString(yesterday)) {
			selectedDate.set(today);
			fetchEvents();
		}
	}
};

function formatUnixDate(ts: number): string {
	return toDateString(new Date(ts * 1000));
}
