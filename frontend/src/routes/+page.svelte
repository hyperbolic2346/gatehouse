<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { gates } from '$lib/stores/gates';
	import { fetchEvents } from '$lib/stores/events';
	import LiveFeed from '$lib/components/LiveFeed.svelte';
	import GateControl from '$lib/components/GateControl.svelte';
	import Calendar from '$lib/components/Calendar.svelte';
	import EventList from '$lib/components/EventList.svelte';

	onMount(async () => {
		const [gateStatus] = await Promise.all([api.getGates(), fetchEvents()]);
		gates.set(gateStatus);
	});
</script>

<div class="mx-auto max-w-6xl space-y-4 p-4">
	<section>
		<LiveFeed />
	</section>

	<section>
		<h2 class="mb-2 text-sm font-medium text-gray-400">Gate Controls</h2>
		<GateControl />
	</section>

	<section>
		<div class="mb-2 flex items-center justify-between">
			<h2 class="text-sm font-medium text-gray-400">Events</h2>
			<Calendar />
		</div>
		<EventList />
	</section>
</div>
