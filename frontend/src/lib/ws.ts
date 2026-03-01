import { eventsStore } from './stores/events';
import { gatesStore } from './stores/gates';

export interface WSMessage {
	type: 'new_event' | 'event_update' | 'gate_status' | 'day_rollover';
	data: unknown;
}

let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let reconnectDelay = 1000;

function getWsUrl(): string {
	const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
	return `${proto}//${window.location.host}/api/ws`;
}

function handleMessage(msg: WSMessage) {
	switch (msg.type) {
		case 'new_event':
			eventsStore.addEvent(msg.data as import('./api').Event);
			break;
		case 'event_update':
			eventsStore.updateEvent(msg.data as import('./api').Event);
			break;
		case 'gate_status':
			gatesStore.set(msg.data as import('./api').GateStatus[]);
			break;
		case 'day_rollover':
			eventsStore.handleDayRollover((msg.data as { date: string }).date);
			break;
	}
}

export function connect() {
	if (ws?.readyState === WebSocket.OPEN) return;

	ws = new WebSocket(getWsUrl());

	ws.onopen = () => {
		reconnectDelay = 1000;
	};

	ws.onmessage = (ev) => {
		try {
			const msg: WSMessage = JSON.parse(ev.data);
			handleMessage(msg);
		} catch {
			// ignore malformed messages
		}
	};

	ws.onclose = () => {
		scheduleReconnect();
	};

	ws.onerror = () => {
		ws?.close();
	};
}

function scheduleReconnect() {
	if (reconnectTimer) return;
	reconnectTimer = setTimeout(() => {
		reconnectTimer = null;
		reconnectDelay = Math.min(reconnectDelay * 2, 30000);
		connect();
	}, reconnectDelay);
}

export function disconnect() {
	if (reconnectTimer) {
		clearTimeout(reconnectTimer);
		reconnectTimer = null;
	}
	ws?.close();
	ws = null;
}
