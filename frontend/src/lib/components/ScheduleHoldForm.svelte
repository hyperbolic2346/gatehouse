<script lang="ts">
	import { api } from '$lib/api';
	import { refreshSchedules } from '$lib/stores/schedules';

	let { gateId, gateName, onDone }: { gateId: number; gateName: string; onDone: () => void } = $props();

	const pad = (n: number) => String(n).padStart(2, '0');
	const dateValue = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
	const timeValue = (d: Date) => `${pad(d.getHours())}:${pad(d.getMinutes())}`;

	// Default to the next whole hour, for two hours.
	const start = new Date();
	start.setMinutes(0, 0, 0);
	start.setHours(start.getHours() + 1);
	const end = new Date(start.getTime() + 2 * 60 * 60 * 1000);

	let date = $state(dateValue(start));
	let from = $state(timeValue(start));
	let until = $state(timeValue(end));
	let note = $state('');
	let saving = $state(false);
	let error = $state('');

	// "Until" earlier than "from" means the next morning.
	let span = $derived.by(() => {
		const s = new Date(`${date}T${from}`);
		const e = new Date(`${date}T${until}`);
		if (e <= s) e.setDate(e.getDate() + 1);
		return { s, e };
	});
	let overnight = $derived(span.e.getDate() !== span.s.getDate());

	async function save() {
		saving = true;
		error = '';
		try {
			await api.scheduleHold({ gate_id: gateId, start: span.s, end: span.e, note });
			await refreshSchedules();
			onDone();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Could not schedule the hold';
		} finally {
			saving = false;
		}
	}
</script>

<form
	onsubmit={(e) => {
		e.preventDefault();
		save();
	}}
	class="space-y-2 rounded bg-gray-900 p-3 text-sm"
>
	<p class="text-gray-300">Hold {gateName} open</p>
	<div class="flex flex-wrap items-center gap-2">
		<label class="flex items-center gap-1 text-gray-400">
			On
			<input type="date" bind:value={date} required class="rounded bg-gray-700 px-2 py-1 text-white" />
		</label>
		<label class="flex items-center gap-1 text-gray-400">
			from
			<input type="time" bind:value={from} required class="rounded bg-gray-700 px-2 py-1 text-white" />
		</label>
		<label class="flex items-center gap-1 text-gray-400">
			until
			<input type="time" bind:value={until} required class="rounded bg-gray-700 px-2 py-1 text-white" />
		</label>
		{#if overnight}
			<span class="text-xs text-yellow-300">(next day)</span>
		{/if}
	</div>
	<input
		bind:value={note}
		maxlength="200"
		placeholder="What for? e.g. UPS delivery"
		class="w-full rounded bg-gray-700 px-2 py-1 text-white"
	/>
	{#if error}
		<p class="text-red-300">{error}</p>
	{/if}
	<div class="flex gap-2">
		<button
			type="submit"
			disabled={saving}
			class="rounded bg-blue-600 px-3 py-1 font-medium text-white hover:bg-blue-700 disabled:opacity-50"
		>
			{saving ? 'Scheduling…' : 'Schedule'}
		</button>
		<button type="button" onclick={onDone} class="rounded bg-gray-700 px-3 py-1 text-gray-200 hover:bg-gray-600">
			Cancel
		</button>
	</div>
</form>
