<script lang="ts">
	import {
		events,
		eventsLoading,
		eventsError,
		eventsHasMore,
		eventsLoadingMore,
		fetchEvents,
		loadMoreEvents
	} from '$lib/stores/events';
	import { api, type Event } from '$lib/api';
	import EventCard from './EventCard.svelte';

	let eventList = $derived($events);
	let loading = $derived($eventsLoading);
	let error = $derived($eventsError);
	let clipModal = $state<Event | null>(null);

	// Load the next page automatically when the end of the list scrolls
	// into view (with some lead), so the whole day is reachable by scrolling.
	let sentinel = $state<HTMLDivElement | undefined>();
	$effect(() => {
		if (!sentinel) return;
		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((e) => e.isIntersecting)) loadMoreEvents();
			},
			{ rootMargin: '400px' }
		);
		observer.observe(sentinel);
		return () => observer.disconnect();
	});

	async function handleDelete(id: string) {
		if (!confirm('Delete this event?')) return;
		try {
			await api.deleteEvent(id);
			events.update((list) => list.filter((e) => e.id !== id));
		} catch (err) {
			alert('Failed to delete event');
		}
	}

	function handlePlay(event: Event) {
		if (event.has_clip) {
			clipModal = event;
		}
	}

	function closeModal() {
		clipModal = null;
	}
</script>

{#if loading}
	<div class="py-8 text-center text-gray-400">Loading events...</div>
{:else if error}
	<div class="py-8 text-center">
		<p class="text-red-400">Failed to load events</p>
		<p class="mt-1 text-sm text-gray-500">{error}</p>
		<button
			onclick={() => fetchEvents()}
			class="mt-2 rounded bg-gray-700 px-3 py-1 text-sm text-gray-300 hover:bg-gray-600"
		>
			Retry
		</button>
	</div>
{:else if eventList.length === 0}
	<div class="py-8 text-center text-gray-500">No events for this day</div>
{:else}
	<div class="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
		{#each eventList as event (event.id)}
			<EventCard {event} onDelete={handleDelete} onPlay={handlePlay} />
		{/each}
	</div>
	{#if $eventsHasMore}
		<div bind:this={sentinel} class="py-4 text-center">
			<button
				onclick={() => loadMoreEvents()}
				disabled={$eventsLoadingMore}
				class="rounded bg-gray-700 px-4 py-2 text-sm text-gray-200 hover:bg-gray-600 disabled:opacity-50"
			>
				{$eventsLoadingMore ? 'Loading…' : 'Show more'}
			</button>
		</div>
	{:else if eventList.length > 0}
		<p class="py-4 text-center text-xs text-gray-500">{eventList.length} events · that's the whole day</p>
	{/if}
{/if}

{#if clipModal}
	<div
		class="fixed inset-0 z-50 flex items-center justify-center bg-black/80"
		role="dialog"
	>
		<div class="relative w-full max-w-3xl">
			<button
				onclick={closeModal}
				class="absolute -top-8 right-0 text-sm text-gray-300 hover:text-white"
			>
				Close
			</button>
			<!-- svelte-ignore a11y_media_has_caption -->
			<video
				src={api.clipUrl(clipModal.id)}
				controls
				autoplay
				class="w-full rounded"
			></video>
		</div>
	</div>
{/if}
