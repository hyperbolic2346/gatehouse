export interface User {
	id: number;
	username: string;
	role: string;
	wilson_gate: boolean;
	brigman_gate: boolean;
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

export interface GateStatus {
	name: string;
	id: number;
	hold_status: string;
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
			throw new Error(text || res.statusText);
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

	async getEvents(date?: string, camera?: string): Promise<Event[]> {
		const params = new URLSearchParams();
		if (date) params.set('date', date);
		if (camera) params.set('camera', camera);
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
