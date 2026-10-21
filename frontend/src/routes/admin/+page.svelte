<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { user } from '$lib/stores/auth';
	import UserManager from '$lib/components/UserManager.svelte';
	import ActivityLog from '$lib/components/ActivityLog.svelte';

	let currentUser = $derived($user);

	onMount(() => {
		if (currentUser && currentUser.role !== 'admin') {
			goto('/');
		}
	});
</script>

{#if currentUser?.role === 'admin'}
	<div class="mx-auto max-w-4xl p-4">
		<h1 class="mb-4 text-lg font-bold text-white">User Management</h1>
		<UserManager />

		<h2 class="mt-8 mb-4 text-lg font-bold text-white">Activity</h2>
		<ActivityLog />
	</div>
{/if}
