<script lang="ts">
	import { selectedDate, fetchEvents, toDateString, fromDateString } from '$lib/stores/events';

	const today = () => toDateString(new Date());

	let selected = $derived(fromDateString($selectedDate));
	let isToday = $derived($selectedDate === today());
	let label = $derived(
		isToday
			? 'Today'
			: selected.toLocaleDateString(undefined, {
					weekday: 'short',
					month: 'short',
					day: 'numeric',
					year: selected.getFullYear() === new Date().getFullYear() ? undefined : 'numeric'
				})
	);

	let open = $state(false);
	// First day of the month shown in the picker.
	let viewMonth = $state(new Date());
	let root: HTMLDivElement | undefined = $state();

	function go(date: Date) {
		const value = toDateString(date);
		if (value > today()) return;
		selectedDate.set(value);
		fetchEvents();
	}

	function shiftDay(days: number) {
		const d = new Date(selected);
		d.setDate(d.getDate() + days);
		go(d);
	}

	function togglePicker() {
		open = !open;
		if (open) viewMonth = new Date(selected.getFullYear(), selected.getMonth(), 1);
	}

	function shiftMonth(months: number) {
		viewMonth = new Date(viewMonth.getFullYear(), viewMonth.getMonth() + months, 1);
	}

	function pick(date: Date) {
		go(date);
		open = false;
	}

	let monthLabel = $derived(viewMonth.toLocaleDateString(undefined, { month: 'long', year: 'numeric' }));
	let atCurrentMonth = $derived(
		viewMonth.getFullYear() === new Date().getFullYear() && viewMonth.getMonth() === new Date().getMonth()
	);

	// Weekday initials in the viewer's locale, Sunday first.
	const weekdays = Array.from({ length: 7 }, (_, i) =>
		new Date(2026, 0, 4 + i).toLocaleDateString(undefined, { weekday: 'narrow' })
	);

	// Cells for the month grid: leading blanks, then each day.
	let cells = $derived.by(() => {
		const first = new Date(viewMonth.getFullYear(), viewMonth.getMonth(), 1);
		const days = new Date(viewMonth.getFullYear(), viewMonth.getMonth() + 1, 0).getDate();
		const out: (Date | null)[] = Array(first.getDay()).fill(null);
		for (let d = 1; d <= days; d++) out.push(new Date(first.getFullYear(), first.getMonth(), d));
		return out;
	});

	$effect(() => {
		if (!open) return;
		const onPointer = (e: PointerEvent) => {
			if (root && !root.contains(e.target as Node)) open = false;
		};
		const onKey = (e: KeyboardEvent) => {
			if (e.key === 'Escape') open = false;
		};
		document.addEventListener('pointerdown', onPointer);
		document.addEventListener('keydown', onKey);
		return () => {
			document.removeEventListener('pointerdown', onPointer);
			document.removeEventListener('keydown', onKey);
		};
	});

	const arrowClass =
		'rounded bg-gray-700 px-3 py-1 text-lg leading-none text-gray-200 hover:bg-gray-600 disabled:opacity-30';
</script>

<div class="relative flex items-center gap-1" bind:this={root}>
	<button onclick={() => shiftDay(-1)} class={arrowClass} aria-label="Previous day">‹</button>
	<button
		onclick={togglePicker}
		aria-expanded={open}
		aria-haspopup="dialog"
		class="min-w-28 rounded bg-gray-800 px-3 py-1 text-sm text-white hover:bg-gray-700"
	>
		{label} ▾
	</button>
	<button onclick={() => shiftDay(1)} disabled={isToday} class={arrowClass} aria-label="Next day">›</button>
	{#if !isToday}
		<button
			onclick={() => go(new Date())}
			class="ml-1 rounded bg-gray-700 px-2 py-1 text-sm text-gray-300 hover:bg-gray-600"
		>
			Today
		</button>
	{/if}

	{#if open}
		<div
			role="dialog"
			aria-label="Choose a day"
			class="absolute top-full right-0 z-40 mt-2 w-72 rounded-lg border border-gray-700 bg-gray-900 p-3 shadow-xl"
		>
			<div class="mb-2 flex items-center justify-between">
				<button onclick={() => shiftMonth(-1)} class={arrowClass} aria-label="Previous month">‹</button>
				<span class="text-sm font-medium text-white">{monthLabel}</span>
				<button
					onclick={() => shiftMonth(1)}
					disabled={atCurrentMonth}
					class={arrowClass}
					aria-label="Next month">›</button
				>
			</div>
			<div class="grid grid-cols-7 gap-1 text-center text-xs">
				{#each weekdays as w}
					<div class="py-1 text-gray-500">{w}</div>
				{/each}
				{#each cells as day}
					{#if day}
						{@const value = toDateString(day)}
						{@const future = value > today()}
						<button
							onclick={() => pick(day)}
							disabled={future}
							class="rounded py-1.5 text-sm {value === $selectedDate
								? 'bg-blue-600 font-semibold text-white'
								: value === today()
									? 'border border-blue-500 text-white hover:bg-gray-700'
									: 'text-gray-200 hover:bg-gray-700'} disabled:text-gray-700 disabled:hover:bg-transparent"
						>
							{day.getDate()}
						</button>
					{:else}
						<div></div>
					{/if}
				{/each}
			</div>
		</div>
	{/if}
</div>
