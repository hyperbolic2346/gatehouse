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

	function stateColor(state: string): string {
		switch (state) {
			case 'OPEN':
				return 'bg-green-500';
			case 'CLOSED':
				return 'bg-red-500';
			case 'MOVING':
				return 'bg-yellow-500';
			default:
				return 'bg-gray-500';
		}
	}

	function holdColor(holdState: string): string {
		if (holdState === 'HELD BY US') return 'text-yellow-400';
		return 'text-gray-500';
	}
</script>

<div class="space-y-3">
	{#each gateList as gate}
		{#if canOperateGate(currentUser, gate.name)}
			<div class="flex items-center gap-3 rounded bg-gray-800 p-3">
				<div class="flex items-center gap-2">
					<span class="h-3 w-3 rounded-full {stateColor(gate.state)}"></span>
					<span class="w-20 font-medium text-white">{gate.name}</span>
				</div>
				<div class="flex flex-1 items-center gap-2">
					<span class="text-xs text-gray-400">{gate.state}</span>
					{#if gate.hold_state !== 'NOT HELD'}
						<span class="text-xs {holdColor(gate.hold_state)}">{gate.hold_state}</span>
					{/if}
				</div>
				{#if isMobile}
					<select
						onchange={(e) => {
							const action = (e.target as HTMLSelectElement).value as 'open' | 'hold' | 'release';
							if (action) performAction(gate.id, action);
							(e.target as HTMLSelectElement).value = '';
						}}
						class="rounded bg-gray-700 px-2 py-1 text-sm text-white"
						disabled={gate.state === 'MOVING' || actionInProgress !== null}
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
								disabled={gate.state === 'MOVING' || actionInProgress === `${gate.id}-${action}`}
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
