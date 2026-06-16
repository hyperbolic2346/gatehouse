import { writable } from 'svelte/store';
import { api, setOnUnauthorized, type User } from '../api';

export const user = writable<User | null>(null);
export const loading = writable(true);

// When any API call gets a 401, clear the user store so the layout
// reactively redirects to /login (no hard page reload).
setOnUnauthorized(() => {
	user.set(null);
});

export async function checkAuth(): Promise<boolean> {
	try {
		const u = await api.me();
		user.set(u);
		return true;
	} catch {
		user.set(null);
		return false;
	} finally {
		loading.set(false);
	}
}

export async function login(username: string, password: string): Promise<void> {
	const u = await api.login(username, password);
	user.set(u);
}

export async function logout(): Promise<void> {
	await api.logout();
	user.set(null);
}

export function isAdmin(u: User | null): boolean {
	return u?.role === 'admin';
}

export function canOperateGate(u: User | null, gateName: string): boolean {
	if (!u) return false;
	if (gateName === 'Wilson') return u.wilson_gate;
	if (gateName === 'Brigman') return u.brigman_gate;
	return false;
}

/** Cameras the user can currently see: permitted minus the ones they've hidden. */
export function visibleCameras(u: User | null): string[] {
	if (!u) return [];
	const hidden = new Set(u.hidden_cameras ?? []);
	return (u.cameras ?? []).filter((c) => !hidden.has(c));
}

/** Whether the user is permitted to view the given camera (ignores hidden state). */
export function canViewCamera(u: User | null, camera: string): boolean {
	return !!u && (u.cameras ?? []).includes(camera);
}
