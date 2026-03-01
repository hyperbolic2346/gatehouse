<script lang="ts">
	import { api, type Event } from '$lib/api';
	import { user } from '$lib/stores/auth';

	interface Props {
		event: Event;
		onDelete?: (id: string) => void;
		onPlay?: (event: Event) => void;
	}

	let { event, onDelete, onPlay }: Props = $props();
	let currentUser = $derived($user);

	function formatTime(ts: number): string {
		return new Date(ts * 1000).toLocaleTimeString([], {
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	const labelColors: Record<string, string> = {
		person: 'bg-blue-600',
		car: 'bg-green-600',
		cat: 'bg-purple-600',
		dog: 'bg-orange-600',
		bird: 'bg-yellow-600'
	};
</script>

<div class="group relative overflow-hidden rounded bg-gray-800">
	<button
		onclick={() => onPlay?.(event)}
		class="block w-full cursor-pointer"
		disabled={!event.has_clip}
	>
		<div class="relative aspect-video bg-gray-700">
			<img
				src={api.thumbnailUrl(event.id)}
				alt="{event.label} at {formatTime(event.start_time)}"
				class="h-full w-full object-cover"
				loading="lazy"
			/>
		</div>
	</button>
	<div class="p-2">
		<div class="flex items-center justify-between">
			<span class="text-xs text-gray-400">{formatTime(event.start_time)}</span>
			<span class="text-xs text-gray-500">{event.camera}</span>
		</div>
		<div class="mt-1 flex gap-1">
			<span
				class="inline-block rounded px-1.5 py-0.5 text-xs text-white {labelColors[event.label] ?? 'bg-gray-600'}"
			>
				{event.label}
			</span>
			{#if event.top_score}
				<span class="text-xs text-gray-500">{Math.round(event.top_score * 100)}%</span>
			{/if}
		</div>
	</div>
	{#if currentUser?.role === 'admin' && onDelete}
		<button
			onclick={() => onDelete?.(event.id)}
			class="absolute right-1 top-1 hidden rounded bg-red-600/80 px-1.5 py-0.5 text-xs text-white hover:bg-red-600 group-hover:block"
			title="Delete event"
		>
			✕
		</button>
	{/if}
</div>
