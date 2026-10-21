<script lang="ts">
	import { api, type ImportResult, type User } from '$lib/api';

	let users = $state<User[]>([]);
	let catalog = $state<string[]>([]);
	let loading = $state(true);
	let error = $state('');

	let newUsername = $state('');
	let newDisplayName = $state('');
	let newPassword = $state('');
	let newPriority = $state(0);
	let newRole = $state('user');
	let newWilson = $state(false);
	let newBrigman = $state(false);
	let newCameras = $state<string[]>(['gate']);
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

	async function loadCatalog() {
		try {
			catalog = await api.getCameras();
		} catch {
			// Frigate may be unreachable; fall back to cameras already assigned
			// so existing grants remain editable.
			catalog = [];
		}
	}

	// Cameras to offer as toggles: the Frigate catalog, or — if unavailable —
	// the union of cameras already assigned across users.
	let cameraOptions = $derived(
		catalog.length > 0
			? catalog
			: [...new Set(users.flatMap((u) => u.cameras ?? []))].sort()
	);

	function toggleNewCamera(camera: string) {
		newCameras = newCameras.includes(camera)
			? newCameras.filter((c) => c !== camera)
			: [...newCameras, camera];
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
				brigman_gate: newBrigman,
				cameras: newCameras,
				display_name: newDisplayName,
				hold_priority: newPriority
			});
			users = [...users, user];
			newUsername = '';
			newDisplayName = '';
			newPassword = '';
			newPriority = 0;
			newRole = 'user';
			newWilson = false;
			newBrigman = false;
			newCameras = ['gate'];
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

	async function toggleCamera(user: User, camera: string) {
		const set = new Set(user.cameras ?? []);
		if (set.has(camera)) {
			set.delete(camera);
		} else {
			set.add(camera);
		}
		try {
			const updated = await api.updateUser(user.id, { cameras: [...set] });
			users = users.map((u) => (u.id === updated.id ? updated : u));
		} catch {
			error = 'Failed to update cameras';
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

	async function editDisplayName(user: User) {
		const name = prompt(`Name shown for ${user.username} (blank to use the username):`, user.display_name);
		if (name === null) return;
		try {
			const updated = await api.updateUser(user.id, { display_name: name });
			users = users.map((u) => (u.id === updated.id ? updated : u));
		} catch {
			error = 'Failed to update name';
		}
	}

	async function changePriority(user: User, value: string) {
		const priority = Number.parseInt(value, 10);
		if (!Number.isFinite(priority) || priority === user.hold_priority) return;
		try {
			const updated = await api.updateUser(user.id, { hold_priority: priority });
			users = users.map((u) => (u.id === updated.id ? updated : u));
		} catch {
			error = 'Failed to update hold priority';
		}
	}

	// Import from the old gate site.
	const importCommand =
		"mysql -B -u root -p GATE_DATABASE -e 'SELECT username, password, display_name, access_level, operate_wilson_gate, operate_brigman_gate FROM users'";
	let importTSV = $state('');
	let importing = $state(false);
	let importResult = $state<ImportResult | null>(null);

	async function importUsers() {
		if (!importTSV.trim()) return;
		importing = true;
		error = '';
		try {
			importResult = await api.importUsers(importTSV);
			importTSV = '';
			await loadUsers();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Import failed';
		} finally {
			importing = false;
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
		loadCatalog();
	});
</script>

<div class="space-y-6">
	{#if error}
		<div class="rounded bg-red-900/50 px-4 py-2 text-sm text-red-300">{error}</div>
	{/if}

	<div class="rounded bg-gray-800 p-4">
		<h3 class="mb-3 text-sm font-medium text-gray-300">Add User</h3>
		<form onsubmit={(e) => { e.preventDefault(); createUser(); }} class="flex flex-wrap items-start gap-2">
			<input
				bind:value={newUsername}
				placeholder="Username"
				required
				class="rounded border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-white"
			/>
			<input
				bind:value={newDisplayName}
				placeholder="Display name (optional)"
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
			<label class="flex items-center gap-1 text-sm text-gray-300" title="Higher outranks lower for gate holds">
				Priority
				<input
					type="number"
					bind:value={newPriority}
					class="w-14 rounded border border-gray-700 bg-gray-900 px-1 py-1 text-sm text-white"
				/>
			</label>
			<button
				type="submit"
				disabled={creating}
				class="rounded bg-blue-600 px-3 py-1 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
			>
				Add
			</button>
			{#if cameraOptions.length > 0}
				<div class="flex w-full flex-wrap gap-1 pt-1">
					<span class="mr-1 text-xs text-gray-400">Cameras:</span>
					{#each cameraOptions as camera (camera)}
						<button
							type="button"
							onclick={() => toggleNewCamera(camera)}
							class="rounded px-2 py-0.5 text-xs {newCameras.includes(camera)
								? 'bg-blue-600 text-white'
								: 'bg-gray-700 text-gray-400'}"
						>
							{camera}
						</button>
					{/each}
				</div>
			{/if}
		</form>
	</div>

	{#if loading}
		<div class="text-center text-gray-400">Loading users...</div>
	{:else}
		<div class="overflow-x-auto">
			<table class="w-full text-left text-sm">
				<thead class="border-b border-gray-700 text-xs text-gray-400 uppercase">
					<tr>
						<th class="px-3 py-2">Username</th>
						<th class="px-3 py-2">Name</th>
						<th class="px-3 py-2">Role</th>
						<th class="px-3 py-2" title="Higher outranks lower for gate holds; admins outrank everyone">Priority</th>
						<th class="px-3 py-2">Wilson</th>
						<th class="px-3 py-2">Brigman</th>
						<th class="px-3 py-2">Cameras</th>
						<th class="px-3 py-2">Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each users as u (u.id)}
						<tr class="border-b border-gray-800">
							<td class="px-3 py-2 text-white">
								{u.username}
								{#if u.legacy_password}
									<span
										class="ml-1 rounded bg-yellow-900/50 px-1 text-[10px] text-yellow-300"
										title="Imported from the old gate site; upgraded when they next log in"
									>
										old pw
									</span>
								{/if}
							</td>
							<td class="px-3 py-2">
								<button onclick={() => editDisplayName(u)} class="text-xs text-gray-300 hover:text-white">
									{u.display_name || '—'}
								</button>
							</td>
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
								{#if u.role === 'admin'}
									<span class="text-xs text-gray-500" title="Admins outrank everyone">top</span>
								{:else}
									<input
										type="number"
										value={u.hold_priority}
										onchange={(e) => changePriority(u, (e.target as HTMLInputElement).value)}
										class="w-14 rounded bg-gray-700 px-1 py-0.5 text-xs text-white"
									/>
								{/if}
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
							<td class="px-3 py-2">
								<div class="flex flex-wrap gap-1">
									{#each cameraOptions as camera (camera)}
										<button
											onclick={() => toggleCamera(u, camera)}
											class="rounded px-1.5 py-0.5 text-xs {(u.cameras ?? []).includes(camera)
												? 'bg-blue-600 text-white'
												: 'bg-gray-700 text-gray-500'}"
										>
											{camera}
										</button>
									{/each}
									{#if cameraOptions.length === 0}
										<span class="text-xs text-gray-500">—</span>
									{/if}
								</div>
							</td>
							<td class="flex gap-1 px-3 py-2">
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

	<details class="rounded bg-gray-800 p-4">
		<summary class="cursor-pointer text-sm font-medium text-gray-300">Import users from the old gate site</summary>
		<div class="mt-3 space-y-2 text-sm text-gray-400">
			<p>
				Run this against the old gate MySQL database and paste the output below. Existing usernames are
				skipped. Everyone comes in as a regular user with their current password, which is upgraded the
				first time they log in.
			</p>
			<code class="block overflow-x-auto rounded bg-gray-900 p-2 text-xs whitespace-pre text-gray-300">{importCommand}</code>
			<textarea
				bind:value={importTSV}
				rows="6"
				placeholder="username&#9;password&#9;display_name&#9;..."
				class="w-full rounded border border-gray-700 bg-gray-900 p-2 font-mono text-xs text-white"
			></textarea>
			<button
				onclick={importUsers}
				disabled={importing || !importTSV.trim()}
				class="rounded bg-blue-600 px-3 py-1 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
			>
				Import
			</button>
			{#if importResult}
				<div class="text-xs">
					<p class="text-green-400">
						Imported {importResult.imported.length}:
						{importResult.imported.map((u) => u.username).join(', ') || 'none'}
					</p>
					{#each importResult.skipped as skip}
						<p class="text-yellow-300">Skipped {skip.username}: {skip.reason}</p>
					{/each}
				</div>
			{/if}
		</div>
	</details>
</div>
