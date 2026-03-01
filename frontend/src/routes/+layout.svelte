<script lang="ts">
	import '../app.css';
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { user, loading, checkAuth, logout } from '$lib/stores/auth';
	import { connect, disconnect } from '$lib/ws';

	let { children } = $props();

	let currentUser = $derived($user);
	let isLoading = $derived($loading);

	onMount(async () => {
		// On the login page, skip the auth check — just mark loading done.
		if (page.url.pathname === '/login') {
			loading.set(false);
			return;
		}

		const authed = await checkAuth();
		if (authed) {
			connect();
		}
	});

	onDestroy(() => {
		disconnect();
	});

	// Reactive redirect: whenever user becomes null (initial load or
	// mid-session token expiry) and we're not on /login, navigate there.
	$effect(() => {
		if (!isLoading && !currentUser && page.url.pathname !== '/login') {
			goto('/login');
		}
	});

	async function handleLogout() {
		await logout();
		disconnect();
		goto('/login');
	}
</script>

{#if isLoading}
	<div class="flex h-screen items-center justify-center bg-gray-900">
		<div class="text-gray-400">Loading...</div>
	</div>
{:else if currentUser || page.url.pathname === '/login'}
	{#if currentUser && page.url.pathname !== '/login'}
		<header class="flex items-center justify-between border-b border-gray-700 bg-gray-900 px-4 py-3">
			<a href="/" class="text-xl font-bold text-white">Gatehouse</a>
			<div class="flex items-center gap-4">
				{#if currentUser.role === 'admin'}
					<a href="/admin" class="text-sm text-gray-300 hover:text-white">Admin</a>
				{/if}
				<span class="text-sm text-gray-400">{currentUser.username}</span>
				<button
					onclick={handleLogout}
					class="rounded bg-gray-700 px-3 py-1 text-sm text-gray-300 hover:bg-gray-600"
				>
					Logout
				</button>
			</div>
		</header>
	{/if}
	<main class="min-h-screen bg-gray-950">
		{@render children()}
	</main>
{/if}
