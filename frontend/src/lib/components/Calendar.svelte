<script lang="ts">
	import { selectedDate, fetchEvents } from '$lib/stores/events';

	let dateValue = $derived(formatForInput($selectedDate));

	function formatForInput(yyyymmdd: string): string {
		return `${yyyymmdd.slice(0, 4)}-${yyyymmdd.slice(4, 6)}-${yyyymmdd.slice(6, 8)}`;
	}

	function parseFromInput(value: string): string {
		return value.replace(/-/g, '');
	}

	function handleChange(e: globalThis.Event) {
		const target = e.target as HTMLInputElement;
		selectedDate.set(parseFromInput(target.value));
		fetchEvents();
	}

	function goToday() {
		const d = new Date();
		const today =
			d.getFullYear().toString() +
			(d.getMonth() + 1).toString().padStart(2, '0') +
			d.getDate().toString().padStart(2, '0');
		selectedDate.set(today);
		fetchEvents();
	}
</script>

<div class="flex items-center gap-2">
	<input
		type="date"
		value={dateValue}
		onchange={handleChange}
		class="rounded border border-gray-700 bg-gray-800 px-2 py-1 text-sm text-white"
	/>
	<button
		onclick={goToday}
		class="rounded bg-gray-700 px-2 py-1 text-sm text-gray-300 hover:bg-gray-600"
	>
		Today
	</button>
</div>
