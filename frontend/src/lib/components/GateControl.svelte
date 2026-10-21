<script lang="ts">
	import { api, type GateStatus } from '$lib/api';
	import { gates } from '$lib/stores/gates';
	import { user, canOperateGate } from '$lib/stores/auth';

	let gateList = $derived($gates);
	let currentUser = $derived($user);
	let actionInProgress = $state<string | null>(null);
	let isMobile = $state(false);

	function checkMobile() {
		isMobile = typeof window !== 'undefined' && window.innerWidth < 768;
	}

	$effect(() => {
		if (typeof window !== 'undefined') {
			checkMobile();
			window.addEventListener('resize', checkMobile);
			return () => window.removeEventListener('resize', checkMobile);
		}
	});

	async function performAction(gateId: number, action: 'open' | 'hold' | 'release') {
		const key = `${gateId}-${action}`;
		actionInProgress = key;
		try {
			const result = await api.gateAction(gateId, action);
			gates.set(result);
		} catch (err) {
			alert(`Gate action failed: ${err instanceof Error ? err.message : 'Unknown error'}`);
		} finally {
			actionInProgress = null;
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
</script>

<div class="space-y-3">
	{#each gateList as gate}
		{#if canOperateGate(currentUser, gate.name)}
			<div class="flex items-center gap-3 rounded bg-gray-800 p-3">
				<div class="flex items-center gap-2">
					<span class="w-20 font-medium text-white">{gate.name}</span>
				</div>
				<div class="flex flex-1 items-center gap-2">
					<span class="text-xs {holdColor(gate)}">{holdLabel(gate)}</span>
				</div>
				{#if isMobile}
					<select
						onchange={(e) => {
							const action = (e.target as HTMLSelectElement).value as 'open' | 'hold' | 'release';
							if (action) performAction(gate.id, action);
							(e.target as HTMLSelectElement).value = '';
						}}
						class="rounded bg-gray-700 px-2 py-1 text-sm text-white"
						disabled={actionInProgress !== null}
					>
						<option value="">Action...</option>
						<option value="open">Open</option>
						<option value="hold">Hold</option>
						<option value="release">Release</option>
					</select>
				{:else}
					<div class="flex gap-1">
						{#each ['open', 'hold', 'release'] as action}
							<button
								onclick={() => performAction(gate.id, action as 'open' | 'hold' | 'release')}
								disabled={actionInProgress === `${gate.id}-${action}`}
								class="rounded bg-gray-700 px-3 py-1 text-xs font-medium text-gray-200 capitalize hover:bg-gray-600 disabled:opacity-50"
							>
								{action}
							</button>
						{/each}
					</div>
				{/if}
			</div>
		{/if}
	{/each}
</div>
