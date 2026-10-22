export interface User {
	id: number;
	username: string;
	role: string;
	wilson_gate: boolean;
	brigman_gate: boolean;
	/** Cameras this user is permitted to view (admin-controlled). */
	cameras: string[];
	/** Cameras the user has hidden from their own view (subset of cameras). */
	hidden_cameras: string[];
	/** Name shown to others; empty means use username. */
	display_name: string;
	/** Hold priority among non-admins: a higher number outranks a lower one. */
	hold_priority: number;
	/** Still on a password imported from the old gate site (upgraded on next login). */
	legacy_password: boolean;
}

export function displayName(u: Pick<User, 'username' | 'display_name'>): string {
	return u.display_name || u.username;
}

export interface Event {
	id: string;
	camera: string;
	label: string;
	top_score: number;
	start_time: number;
	end_time: number | null;
	has_clip: boolean;
	has_snapshot: boolean;
}

/** Events per page from GET /api/events (the server's Frigate page size). */
export const EVENTS_PAGE_SIZE = 50;

export interface GateStatus {
	name: string;
	id: number;
	hold_status: string;
	held: boolean;
	/** Who in Gatehouse holds the gate; absent for holds placed elsewhere. */
	held_by?: string;
	/** When the current hold began, if known (ISO timestamp). */
	held_since?: string;
}

export interface Activity {
	id: number;
	at: string;
	user_id?: number;
	/** Display name at the time; empty for status reports from the gate controller. */
	actor: string;
	gate_id?: number;
	action: string;
	detail?: string;
}

export interface ScheduledHold {
	id: number;
	gate_id: number;
	user_id: number;
	owner: string;
	start: string;
	end: string;
	note: string;
	status: 'pending' | 'active' | 'done' | 'cancelled' | 'skipped' | 'missed';
	detail?: string;
	can_cancel: boolean;
}

export interface ImportResult {
	imported: User[];
	skipped: { username: string; reason: string }[];
}

let unauthorizedCallback: (() => void) | null = null;

export function setOnUnauthorized(cb: () => void) {
	unauthorizedCallback = cb;
}

class ApiClient {
	private async request<T>(path: string, options?: RequestInit): Promise<T> {
		const res = await fetch(path, {
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json' },
			...options
		});
		if (res.status === 401) {
			unauthorizedCallback?.();
			throw new Error('Unauthorized');
		}
		if (!res.ok) {
			const text = await res.text();
			let message = text || res.statusText;
			try {
				message = JSON.parse(text).error ?? message;
			} catch {
				// not JSON; use the raw text
			}
			throw new Error(message);
		}
		if (res.status === 204) return undefined as T;
		return res.json();
	}

	async login(username: string, password: string): Promise<User> {
		return this.request('/api/login', {
			method: 'POST',
			body: JSON.stringify({ username, password })
		});
	}

	async logout(): Promise<void> {
		return this.request('/api/logout', { method: 'POST' });
	}

	async me(): Promise<User> {
		return this.request('/api/me');
	}

	/** Update which of the user's permitted cameras are hidden from their view. */
	async setCameraVisibility(hidden: string[]): Promise<User> {
		return this.request('/api/me/cameras', {
			method: 'PUT',
			body: JSON.stringify({ hidden })
		});
	}

	/** Admin: the full catalog of cameras discovered from Frigate. */
	async getCameras(): Promise<string[]> {
		return this.request('/api/cameras');
	}

	/** One page of a day's events, newest first. Pass the oldest start_time
	 * already shown as `before` to get the next page. */
	async getEvents(date?: string, camera?: string, before?: number): Promise<Event[]> {
		const params = new URLSearchParams();
		if (date) params.set('date', date);
		if (camera) params.set('camera', camera);
		if (before) params.set('before', String(before));
		return this.request(`/api/events?${params}`);
	}

	async deleteEvent(id: string): Promise<void> {
		return this.request(`/api/events/${id}`, { method: 'DELETE' });
	}

	thumbnailUrl(id: string): string {
		return `/api/events/${id}/thumbnail.jpg`;
	}

	clipUrl(id: string): string {
		return `/api/events/${id}/clip.mp4`;
	}

	async getGates(): Promise<GateStatus[]> {
		return this.request('/api/gates');
	}

	async gateAction(id: number, action: 'open' | 'hold' | 'release'): Promise<GateStatus[]> {
		return this.request(`/api/gates/${id}/${action}`, { method: 'POST' });
	}

	async getUsers(): Promise<User[]> {
		return this.request('/api/users');
	}

	async createUser(data: {
		username: string;
		password: string;
		role: string;
		wilson_gate: boolean;
		brigman_gate: boolean;
		cameras: string[];
		display_name: string;
		hold_priority: number;
	}): Promise<User> {
		return this.request('/api/users', {
			method: 'POST',
			body: JSON.stringify(data)
		});
	}

	async updateUser(
		id: number,
		data: {
			role?: string;
			password?: string;
			wilson_gate?: boolean;
			brigman_gate?: boolean;
			cameras?: string[];
			display_name?: string;
			hold_priority?: number;
		}
	): Promise<User> {
		return this.request(`/api/users/${id}`, {
			method: 'PUT',
			body: JSON.stringify(data)
		});
	}

	async deleteUser(id: number): Promise<void> {
		return this.request(`/api/users/${id}`, { method: 'DELETE' });
	}

	/** Upcoming and in-progress scheduled holds on gates the user can operate. */
	async getSchedules(): Promise<ScheduledHold[]> {
		return this.request('/api/schedules');
	}

	async scheduleHold(data: { gate_id: number; start: Date; end: Date; note: string }): Promise<ScheduledHold> {
		return this.request('/api/schedules', {
			method: 'POST',
			body: JSON.stringify({
				gate_id: data.gate_id,
				start: data.start.toISOString(),
				end: data.end.toISOString(),
				note: data.note
			})
		});
	}

	async cancelSchedule(id: number): Promise<void> {
		return this.request(`/api/schedules/${id}`, { method: 'DELETE' });
	}

	/** Admin: import users from a `mysql -B` dump of the old gate site's users table. */
	async importUsers(tsv: string): Promise<ImportResult> {
		return this.request('/api/users/import', {
			method: 'POST',
			body: JSON.stringify({ tsv })
		});
	}

	/** Admin: audit log, newest first. Pass the smallest id seen as `before` to page back. */
	async getActivity(opts: { before?: number; gate?: number; limit?: number } = {}): Promise<Activity[]> {
		const params = new URLSearchParams();
		if (opts.before) params.set('before', String(opts.before));
		if (opts.gate !== undefined) params.set('gate', String(opts.gate));
		if (opts.limit) params.set('limit', String(opts.limit));
		return this.request(`/api/activity?${params}`);
	}

	async webrtcOffer(camera: string, offer: RTCSessionDescriptionInit): Promise<RTCSessionDescriptionInit> {
		const res = await fetch(`/api/webrtc/offer?camera=${camera}`, {
			method: 'POST',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/sdp' },
			body: offer.sdp
		});
		if (!res.ok) throw new Error('WebRTC offer failed');
		const sdp = await res.text();
		return { type: 'answer', sdp };
	}
}

export const api = new ApiClient();
