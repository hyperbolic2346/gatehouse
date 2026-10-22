<script lang="ts">
	import { api, type Activity } from '$lib/api';

	const pageSize = 100;
	const gateNames: Record<number, string> = { 0: 'Wilson', 1: 'Brigman' };

	let rows = $state<Activity[]>([]);
	let gateFilter = $state('');
	let loading = $state(false);
	let done = $state(false);
	let error = $state('');

	async function load(reset: boolean) {
		loading = true;
		error = '';
		try {
			const before = reset || rows.length === 0 ? undefined : rows[rows.length - 1].id;
			const gate = gateFilter === '' ? undefined : Number(gateFilter);
			const page = await api.getActivity({ before, gate, limit: pageSize });
			rows = reset ? page : [...rows, ...page];
			done = page.length < pageSize;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load activity';
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		void gateFilter;
		load(true);
	});

	function formatTime(iso: string): string {
		return new Date(iso).toLocaleString(undefined, {
			month: 'short',
			day: 'numeric',
			hour: 'numeric',
			minute: '2-digit',
			second: '2-digit'
		});
	}

	// Plain-English description of one row.
	function describe(a: Activity): string {
		const gate = a.gate_id !== undefined ? (gateNames[a.gate_id] ?? `gate ${a.gate_id}`) : '';
		switch (a.action) {
			case 'login':
				return 'Logged in';
			case 'login_failed':
				return 'Failed login';
			case 'open':
				return `Opened ${gate}`;
			case 'hold':
				return `Held ${gate} open`;
			case 'release':
				return `Released ${gate}`;
			case 'open_failed':
			case 'hold_failed':
			case 'release_failed':
				return `Tried to ${a.action.replace('_failed', '')} ${gate}, but the command failed`;
			case 'denied':
				return `Blocked from changing ${gate}`;
			case 'held':
				return `${gate} became held`;
			case 'cleared':
				return `${gate} hold ended`;
			case 'schedule_created':
				return `Scheduled a hold on ${gate}`;
			case 'schedule_cancelled':
				return `Cancelled a scheduled hold on ${gate}`;
			case 'user_created':
				return 'Added user';
			case 'user_updated':
				return 'Changed user';
			case 'user_deleted':
				return 'Deleted user';
			case 'users_imported':
				return 'Imported users';
			default:
				return a.action;
		}
	}

	function rowClass(a: Activity): string {
		if (a.action === 'denied' || a.action === 'login_failed' || a.action.endsWith('_failed')) return 'text-red-300';
		if (a.action === 'held' || a.action === 'cleared') return 'text-gray-500';
		return 'text-gray-200';
	}
</script>

<div class="space-y-3">
	<div class="flex items-center gap-2">
		<select bind:value={gateFilter} class="rounded border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-white">
			<option value="">Everything</option>
			<option value="0">Wilson gate</option>
			<option value="1">Brigman gate</option>
		</select>
		<button
			onclick={() => load(true)}
			disabled={loading}
			class="rounded bg-gray-700 px-3 py-1 text-sm text-gray-200 hover:bg-gray-600 disabled:opacity-50"
		>
			Refresh
		</button>
	</div>

	{#if error}
		<div class="rounded bg-red-900/50 px-4 py-2 text-sm text-red-300">{error}</div>
	{/if}

	<div class="overflow-x-auto">
		<table class="w-full text-left text-sm">
			<thead class="border-b border-gray-700 text-xs text-gray-400 uppercase">
				<tr>
					<th class="px-3 py-2">When</th>
					<th class="px-3 py-2">Who</th>
					<th class="px-3 py-2">What</th>
					<th class="px-3 py-2">Detail</th>
				</tr>
			</thead>
			<tbody>
				{#each rows as a (a.id)}
					<tr class="border-b border-gray-800 {rowClass(a)}">
						<td class="px-3 py-1.5 whitespace-nowrap text-gray-400">{formatTime(a.at)}</td>
						<td class="px-3 py-1.5 whitespace-nowrap">{a.actor || (a.gate_id !== undefined ? 'Gate' : '—')}</td>
						<td class="px-3 py-1.5">{describe(a)}</td>
						<td class="px-3 py-1.5 text-xs text-gray-500">{a.detail ?? ''}</td>
					</tr>
				{:else}
					{#if !loading}
						<tr><td colspan="4" class="px-3 py-4 text-center text-gray-500">No activity yet</td></tr>
					{/if}
				{/each}
			</tbody>
		</table>
	</div>

	{#if !done && rows.length > 0}
		<button
			onclick={() => load(false)}
			disabled={loading}
			class="rounded bg-gray-700 px-3 py-1 text-sm text-gray-200 hover:bg-gray-600 disabled:opacity-50"
		>
			{loading ? 'Loading…' : 'Older'}
		</button>
	{/if}
</div>
