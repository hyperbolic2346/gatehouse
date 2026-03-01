import { writable } from 'svelte/store';
import type { GateStatus } from '../api';

export const gates = writable<GateStatus[]>([]);
export const gatesLoading = writable(false);

export const gatesStore = {
	set(statuses: GateStatus[]) {
		gates.set(statuses);
	}
};
