<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type GateStatus, type ScheduledHold } from '$lib/api';
	import { gates } from '$lib/stores/gates';
	import { schedules, refreshSchedules } from '$lib/stores/schedules';
	import { user, canOperateGate } from '$lib/stores/auth';
	import ScheduleHoldForm from './ScheduleHoldForm.svelte';

	let gateList = $derived($gates);
	let currentUser = $derived($user);
	let actionInProgress = $state<string | null>(null);
	let schedulingGate = $state<number | null>(null);
	let errors = $state<Record<number, string>>({});

	onMount(refreshSchedules);

	async function performAction(gateId: number, action: 'open' | 'hold' | 'release') {
		actionInProgress = `${gateId}-${action}`;
		errors[gateId] = '';
		try {
			gates.set(await api.gateAction(gateId, action));
		} catch (err) {
			errors[gateId] = err instanceof Error ? err.message : 'Something went wrong';
		} finally {
			actionInProgress = null;
		}
	}

	async function cancelSchedule(s: ScheduledHold) {
		if (!confirm(`Cancel ${s.owner}'s hold for ${formatWindow(s)}?`)) return;
		try {
			await api.cancelSchedule(s.id);
			await refreshSchedules();
		} catch (err) {
			errors[s.gate_id] = err instanceof Error ? err.message : 'Could not cancel';
		}
	}

	function holdColor(gate: GateStatus): string {
		if (gate.held) return 'text-yellow-400';
		if (gate.hold_status.toLowerCase() === 'unknown') return 'text-gray-600';
		return 'text-green-400';
	}

	// "Tuesday 4:12 PM", like the old gate site showed.
	function formatSince(iso: string): string {
		return new Date(iso).toLocaleString(undefined, { weekday: 'long', hour: 'numeric', minute: '2-digit' });
	}

	function holdLabel(gate: GateStatus): string {
		if (!gate.held) return gate.hold_status;
		const who = gate.held_by ? `Held by ${gate.held_by}` : 'Held outside Gatehouse';
		if (!gate.held_since) return gate.held_by ? who : gate.hold_status;
		return `${who} since ${formatSince(gate.held_since)}`;
	}

	function sameDay(a: Date, b: Date) {
		return a.toDateString() === b.toDateString();
	}

	// "Today 1:00 – 3:00 PM", "Thu, Oct 23 1:00 – 3:00 PM"
	function formatWindow(s: ScheduledHold): string {
		const start = new Date(s.start);
		const end = new Date(s.end);
		const now = new Date();
		const tomorrow = new Date(now.getTime() + 24 * 60 * 60 * 1000);
		const day = sameDay(start, now)
			? 'Today'
			: sameDay(start, tomorrow)
				? 'Tomorrow'
				: start.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' });
		const time = (d: Date) => d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
		const endPart = sameDay(start, end) ? time(end) : `${time(end)} next day`;
		return `${day} ${time(start)} – ${endPart}`;
	}

	const buttonClass =
		'rounded bg-gray-700 px-4 py-2 text-sm font-medium text-gray-100 hover:bg-gray-600 disabled:opacity-50';
</script>

<div class="space-y-3">
	{#each gateList as gate (gate.id)}
		{#if canOperateGate(currentUser, gate.name)}
			{@const holdAction = gate.held ? 'release' : 'hold'}
			{@const upcoming = $schedules.filter((s) => s.gate_id === gate.id)}
			<div class="space-y-2 rounded bg-gray-800 p-3">
				<div class="flex flex-wrap items-center gap-x-3 gap-y-2">
					<div class="min-w-0 flex-1">
						<div class="font-medium text-white">{gate.name}</div>
						<div class="text-xs {holdColor(gate)}">{holdLabel(gate)}</div>
					</div>
					<div class="flex gap-2">
						<button
							onclick={() => performAction(gate.id, 'open')}
							disabled={actionInProgress !== null}
							class={buttonClass}
						>
							Open
						</button>
						<button
							onclick={() => performAction(gate.id, holdAction)}
							disabled={actionInProgress !== null}
							class="{buttonClass} {gate.held ? 'bg-yellow-700 hover:bg-yellow-600' : ''}"
						>
							{gate.held ? 'Release' : 'Hold'}
						</button>
						<button
							onclick={() => (schedulingGate = schedulingGate === gate.id ? null : gate.id)}
							class={buttonClass}
							aria-expanded={schedulingGate === gate.id}
						>
							Schedule
						</button>
					</div>
				</div>

				{#if errors[gate.id]}
					<p class="text-sm text-red-300">{errors[gate.id]}</p>
				{/if}

				{#if schedulingGate === gate.id}
					<ScheduleHoldForm gateId={gate.id} gateName={gate.name} onDone={() => (schedulingGate = null)} />
				{/if}

				{#each upcoming as s (s.id)}
					<div class="flex items-center gap-2 text-xs text-gray-300">
						<span class={s.status === 'active' ? 'text-yellow-400' : 'text-blue-300'}>
							{s.status === 'active' ? 'Holding now' : 'Scheduled'}
						</span>
						<span class="min-w-0 flex-1">
							{formatWindow(s)} · {s.owner}{s.note ? ` · ${s.note}` : ''}
						</span>
						{#if s.can_cancel}
							<button onclick={() => cancelSchedule(s)} class="text-gray-400 underline hover:text-white">
								Cancel
							</button>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	{/each}
</div>
