<script lang="ts">
	interface Props {
		cameras: string[];
		active: number;
		onChange: (index: number) => void;
	}

	let { cameras, active, onChange }: Props = $props();
</script>

<div class="flex border-b border-gray-700">
	{#each cameras as camera, i}
		<button
			onclick={() => onChange(i)}
			class="flex-1 px-4 py-2 text-center text-sm font-medium transition-colors {i === active
				? 'border-b-2 border-blue-500 text-white'
				: 'text-gray-400 hover:text-gray-200'}"
		>
			{camera}
		</button>
	{/each}
</div>
