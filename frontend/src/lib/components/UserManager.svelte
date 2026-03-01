<script lang="ts">
	import { api, type User } from '$lib/api';

	let users = $state<User[]>([]);
	let loading = $state(true);
	let error = $state('');

	let newUsername = $state('');
	let newPassword = $state('');
	let newRole = $state('user');
	let newWilson = $state(false);
	let newBrigman = $state(false);
	let creating = $state(false);

	async function loadUsers() {
		loading = true;
		try {
			users = await api.getUsers();
		} catch {
			error = 'Failed to load users';
		} finally {
			loading = false;
		}
	}

	async function createUser() {
		if (!newUsername || !newPassword) return;
		creating = true;
		error = '';
		try {
			const user = await api.createUser({
				username: newUsername,
				password: newPassword,
				role: newRole,
				wilson_gate: newWilson,
				brigman_gate: newBrigman
			});
			users = [...users, user];
			newUsername = '';
			newPassword = '';
			newRole = 'user';
			newWilson = false;
			newBrigman = false;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to create user';
		} finally {
			creating = false;
		}
	}

	async function togglePermission(user: User, field: 'wilson_gate' | 'brigman_gate') {
		try {
			const updated = await api.updateUser(user.id, { [field]: !user[field] });
			users = users.map((u) => (u.id === updated.id ? updated : u));
		} catch {
			error = 'Failed to update user';
		}
	}

	async function changeRole(user: User, role: string) {
		try {
			const updated = await api.updateUser(user.id, { role });
			users = users.map((u) => (u.id === updated.id ? updated : u));
		} catch {
			error = 'Failed to update user';
		}
	}

	async function resetPassword(user: User) {
		const pw = prompt(`New password for ${user.username}:`);
		if (!pw) return;
		try {
			await api.updateUser(user.id, { password: pw });
		} catch {
			error = 'Failed to reset password';
		}
	}

	async function deleteUser(user: User) {
		if (!confirm(`Delete user "${user.username}"?`)) return;
		try {
			await api.deleteUser(user.id);
			users = users.filter((u) => u.id !== user.id);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to delete user';
		}
	}

	$effect(() => {
		loadUsers();
	});
</script>

<div class="space-y-6">
	{#if error}
		<div class="rounded bg-red-900/50 px-4 py-2 text-sm text-red-300">{error}</div>
	{/if}

	<div class="rounded bg-gray-800 p-4">
		<h3 class="mb-3 text-sm font-medium text-gray-300">Add User</h3>
		<form onsubmit={(e) => { e.preventDefault(); createUser(); }} class="flex flex-wrap gap-2">
			<input
				bind:value={newUsername}
				placeholder="Username"
				required
				class="rounded border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-white"
			/>
			<input
				bind:value={newPassword}
				type="password"
				placeholder="Password"
				required
				class="rounded border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-white"
			/>
			<select bind:value={newRole} class="rounded border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-white">
				<option value="user">User</option>
				<option value="admin">Admin</option>
			</select>
			<label class="flex items-center gap-1 text-sm text-gray-300">
				<input type="checkbox" bind:checked={newWilson} /> Wilson
			</label>
			<label class="flex items-center gap-1 text-sm text-gray-300">
				<input type="checkbox" bind:checked={newBrigman} /> Brigman
			</label>
			<button
				type="submit"
				disabled={creating}
				class="rounded bg-blue-600 px-3 py-1 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
			>
				Add
			</button>
		</form>
	</div>

	{#if loading}
		<div class="text-center text-gray-400">Loading users...</div>
	{:else}
		<div class="overflow-x-auto">
			<table class="w-full text-sm text-left">
				<thead class="text-xs text-gray-400 uppercase border-b border-gray-700">
					<tr>
						<th class="px-3 py-2">Username</th>
						<th class="px-3 py-2">Role</th>
						<th class="px-3 py-2">Wilson</th>
						<th class="px-3 py-2">Brigman</th>
						<th class="px-3 py-2">Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each users as u (u.id)}
						<tr class="border-b border-gray-800">
							<td class="px-3 py-2 text-white">{u.username}</td>
							<td class="px-3 py-2">
								<select
									value={u.role}
									onchange={(e) => changeRole(u, (e.target as HTMLSelectElement).value)}
									class="rounded bg-gray-700 px-1 py-0.5 text-xs text-white"
								>
									<option value="user">user</option>
									<option value="admin">admin</option>
								</select>
							</td>
							<td class="px-3 py-2">
								<button
									onclick={() => togglePermission(u, 'wilson_gate')}
									class="text-xs {u.wilson_gate ? 'text-green-400' : 'text-gray-500'}"
								>
									{u.wilson_gate ? 'Yes' : 'No'}
								</button>
							</td>
							<td class="px-3 py-2">
								<button
									onclick={() => togglePermission(u, 'brigman_gate')}
									class="text-xs {u.brigman_gate ? 'text-green-400' : 'text-gray-500'}"
								>
									{u.brigman_gate ? 'Yes' : 'No'}
								</button>
							</td>
							<td class="px-3 py-2 flex gap-1">
								<button
									onclick={() => resetPassword(u)}
									class="rounded bg-gray-700 px-2 py-0.5 text-xs text-gray-300 hover:bg-gray-600"
								>
									Reset PW
								</button>
								<button
									onclick={() => deleteUser(u)}
									class="rounded bg-red-900/50 px-2 py-0.5 text-xs text-red-300 hover:bg-red-900"
								>
									Delete
								</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
